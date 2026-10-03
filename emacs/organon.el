;;; organon.el --- Organon engine: RPC boundary and shared helpers  -*- lexical-binding: t; -*-

;; Copyright (c) 2026 Hansaem Woo
;; SPDX-License-Identifier: MIT

;;; Commentary:

;; This file is the only way into the engine.
;;
;; - It opens a Unix socket that speaks newline-delimited JSON, one request per
;;   connection, and dispatches each request to a fixed table of methods
;;   (`organon-defmethod').  Nothing here reads or evaluates Lisp from a request.
;; - It owns the instance configuration (organon.json, calendar_tz).
;; - It provides `organon-with-entry', the single wrapper every write goes
;;   through.  Each step of that wrapper exists because of a failure observed
;;   in the 2026-10-02 spike (docs/architecture.md §6.2, Appendix A).
;;
;; Protocol (docs/architecture.md §6.1):
;;   -> {"id":"r-1","method":"task.get","params":{...}}
;;   <- {"id":"r-1","ok":true,"result":...}
;;   <- {"id":"r-1","ok":false,"error":{"code":"not_found","message":"...","data":...}}
;;
;; Error codes: invalid, not_found, conflict, unavailable, index_rebuilding,
;; prompt_blocked, internal.

;;; Code:

(require 'cl-lib)
(require 'subr-x)
(require 'json)
(require 'org)
(require 'org-id)
(require 'org-agenda)

(defconst organon-version "0.1.0-dev")

;;;; Instance paths and configuration

(defvar organon-data-dir nil "Instance data directory (contains organon.json and org/).")
(defvar organon-org-dir nil "The org/ directory inside `organon-data-dir'.")
(defvar organon-cache-dir nil "Directory for rebuildable caches (org-id locations).")
(defvar organon-run-dir nil "Directory holding the RPC socket.")

(defvar organon-calendar-tz nil "IANA zone the Org files are written in.")
(defvar organon-doing-limit 3 "Number of DOING tasks above which `start' warns.")

(defvar organon-config-error nil
  "Why the instance is not usable, or nil.
While non-nil every method fails with code \"unavailable\" and this message,
so health checks fail instead of the engine silently guessing a time zone.")

(defvar organon-zoneinfo-dir "/usr/share/zoneinfo/")

(defvar organon-configure-hook nil
  "Functions run by `organon-configure' once the instance paths are set.
Modules that keep state per instance (the org-roam index) reset it here.")

(defvar organon-startup-hook nil
  "Functions run by `organon-start' after a successful configuration, before
the socket listens: the engine reports healthy only once they are done.")

(defvar organon--idempotency)           ; defined under "Idempotent creation"

(defun organon-configure (data-dir cache-dir run-dir)
  "Point the engine at DATA-DIR, CACHE-DIR and RUN-DIR and load organon.json."
  (setq organon-data-dir (file-name-as-directory (expand-file-name data-dir))
        organon-org-dir (expand-file-name "org/" organon-data-dir)
        organon-cache-dir (file-name-as-directory (expand-file-name cache-dir))
        organon-run-dir (file-name-as-directory (expand-file-name run-dir)))
  (make-directory organon-cache-dir t)
  (setq org-id-locations-file (expand-file-name "org-id-locations" organon-cache-dir)
        org-id-locations nil
        org-id-files nil)
  ;; Keys belong to an instance.
  (clrhash organon--idempotency)
  (organon-load-config)
  (run-hooks 'organon-configure-hook))

(defconst organon--non-zones '("localtime" "posixrules" "Factory")
  "Zone files that are not a place: the host's own zone, a POSIX default
and the \"-00\" placeholder.")

(defun organon-valid-zone-p (zone)
  "Non-nil if ZONE names a time zone in the system zoneinfo database.
Other files there (leapseconds, tzdata.zi, ...) are not TZif data, and Emacs
would silently treat such a zone as UTC."
  (and (stringp zone)
       (string-match-p "\\`[A-Za-z0-9_+-]+\\(?:/[A-Za-z0-9_+-]+\\)*\\'" zone)
       (not (member zone organon--non-zones))
       (let ((file (expand-file-name zone organon-zoneinfo-dir)))
         (and (file-regular-p file)
              (with-temp-buffer
                (set-buffer-multibyte nil)
                (insert-file-contents-literally file nil 0 4)
                (equal (buffer-string) "TZif"))))))

(defun organon-load-config ()
  "Read organon.json and apply it.  On failure set `organon-config-error'."
  (setq organon-config-error nil)
  (condition-case err
      (let* ((file (expand-file-name "organon.json" organon-data-dir))
             (config (if (file-readable-p file)
                         (with-temp-buffer
                           (insert-file-contents file)
                           (json-parse-buffer :object-type 'alist :null-object nil))
                       (error "Missing %s; create it with `organon init'" file)))
             (zone (alist-get 'calendar_tz config))
             (limit (alist-get 'doing_limit config 3)))
        (unless (organon-valid-zone-p zone)
          (error "Invalid calendar_tz %S in organon.json" zone))
        (unless (and (natnump limit) (> limit 0))
          (error "Invalid doing_limit %S in organon.json" limit))
        (setq organon-calendar-tz zone
              organon-doing-limit limit)
        ;; `setenv' of TZ also calls `set-time-zone-rule', so every date Org
        ;; computes from now on is in the calendar zone.
        (setenv "TZ" zone))
    (error (setq organon-config-error (error-message-string err)))))

;;;; Errors

(define-error 'organon-error "Organon error")

(defun organon-signal (code message &optional data)
  "Fail the current request with error CODE, MESSAGE and optional DATA (alist)."
  (signal 'organon-error (list code message data)))

;;;; Prompt blocking

(defvar organon--in-request nil
  "Non-nil while a request is being handled.")

(defconst organon--prompt-functions
  '(yes-or-no-p y-or-n-p read-string read-from-minibuffer completing-read
    read-char read-char-choice read-char-exclusive read-passwd read-key
    read-multiple-choice ask-user-about-supersession-threat ask-user-about-lock)
  "Functions that wait for a human.  A headless daemon waiting here would block
every later request, so during a request they fail immediately instead.")

(defun organon--block-prompt (orig &rest args)
  (if organon--in-request
      (organon-signal "prompt_blocked"
                      (format "engine attempted an interactive prompt: %s"
                              (string-trim (format "%s" (or (car-safe args) "")))))
    (apply orig args)))

(dolist (fn organon--prompt-functions)
  (advice-add fn :around #'organon--block-prompt))

;;;; Files and buffers

(defun organon-org-files (&rest subdirs)
  "All .org files under SUBDIRS of `organon-org-dir', recursively and sorted."
  (cl-loop for sub in subdirs
           for dir = (expand-file-name sub organon-org-dir)
           when (file-directory-p dir)
           append (sort (directory-files-recursively dir "\\`[^.#].*\\.org\\'") #'string<)))

(defun organon-agenda-files ()
  "Files that feed agenda queries (architecture §5).
Org does not recurse into directories listed in `org-agenda-files', so the
list is computed for every request."
  (organon-org-files "tasks" "projects"))

(defun organon--disable-undo ()
  (when (and buffer-file-name organon-org-dir
             (string-prefix-p organon-org-dir buffer-file-name))
    (buffer-disable-undo)))

(add-hook 'find-file-hook #'organon--disable-undo)

(defun organon-fresh-buffer (file)
  "Return a buffer visiting FILE whose contents match the file on disk.
A clean buffer whose file changed is re-read, or dropped if the file is gone;
a modified one is a conflict."
  (let ((buf (get-file-buffer file)))
    (when (and buf (not (verify-visited-file-modtime buf)))
      (with-current-buffer buf
        (when (buffer-modified-p)
          (organon-signal "conflict" (format "%s changed on disk while it had unsaved edits"
                                             (organon-relative-path file))))
        (if (file-exists-p file)
            (revert-buffer t t t)
          (kill-buffer buf)
          (setq buf nil))))
    (or buf (find-file-noselect file))))

(defun organon-relative-path (file)
  "FILE relative to `organon-org-dir' (a location hint, never an identifier)."
  (file-relative-name file organon-org-dir))

(defun organon--discard-changes ()
  "Drop the current buffer's unsaved changes.
A buffer for a file that does not exist yet (a new project) is emptied
instead: reverting it would fail and hide the original error."
  (if (and buffer-file-name (file-exists-p buffer-file-name))
      (revert-buffer t t t)
    (erase-buffer)
    (set-buffer-modified-p nil)))

(defun organon--save ()
  "Save the current buffer; on failure drop the in-memory changes and fail."
  (condition-case err
      (save-buffer)
    (error
     (organon--discard-changes)
     (organon-signal "internal" (format "could not save %s: %s"
                                        (organon-relative-path buffer-file-name)
                                        (error-message-string err))))))

(defun organon--flush-log-notes ()
  "Write pending LOGBOOK entries now.
Org queues state-change notes on `post-command-hook', which never runs
outside the command loop, so without this they are silently dropped."
  (when (memq 'org-add-log-note (default-value 'post-command-hook))
    (org-add-log-note)))

(defun organon--call-in-buffer (buffer fn)
  "Call FN in BUFFER, then flush log notes and save.
If FN fails, the buffer is reverted so no half-done change stays in memory."
  (with-current-buffer buffer
    (let ((result
           (condition-case err
               (save-excursion
                 (save-restriction
                   (widen)
                   (prog1 (funcall fn)
                     (organon--flush-log-notes))))
             (error
              (remove-hook 'post-command-hook 'org-add-log-note)
              (when (buffer-modified-p) (organon--discard-changes))
              (signal (car err) (cdr err))))))
      (when (buffer-modified-p) (organon--save))
      result)))

;;;; IDs

(defvar organon--id-scan-signature nil
  "`organon--org-files-signature' as of the last full ID scan.")

(defun organon--id-files ()
  (organon-org-files "tasks" "projects" "archive" "knowledge" "journal"))

(defun organon--org-files-signature (files)
  "Names, sizes and modification times of FILES: cheap to compute, and it
changes whenever a file is added, removed, moved or written."
  (mapcar (lambda (file)
            (let ((attrs (file-attributes file)))
              (list file (file-attribute-size attrs) (file-attribute-modification-time attrs))))
          files))

(defun organon-update-id-locations ()
  "Rebuild the ID index from every file in the org directory."
  (let ((inhibit-message t)
        (files (organon--id-files)))
    (org-id-update-id-locations files t)
    (setq organon--id-scan-signature (organon--org-files-signature files))))

(defun organon--find-id-1 (id)
  (let ((file (and (hash-table-p org-id-locations) (gethash id org-id-locations))))
    (when (and file (file-exists-p (expand-file-name file)))
      (with-current-buffer (organon-fresh-buffer (expand-file-name file))
        (save-restriction
          (widen)
          (let ((pos (org-find-property "ID" id)))
            (when pos
              (save-excursion
                (goto-char pos)
                (org-back-to-heading t)
                (point-marker)))))))))

(defun organon-find-id (id)
  "Return a marker at the heading whose ID property is ID.
Looks in the ID index first; on a miss rescans once if any file changed since
the last scan (a heading may have moved), then fails with not_found.  Without
that check every unknown ID would re-read the whole org directory."
  (or (organon--find-id-1 id)
      (unless (equal (organon--org-files-signature (organon--id-files))
                     organon--id-scan-signature)
        (organon-update-id-locations)
        (organon--find-id-1 id))
      (organon-signal "not_found" (format "no entry with id %s" id))))

;;;; Idempotent creation

(defvar organon--idempotency (make-hash-table :test #'equal)
  "Recent keyed creates: idempotency key -> (FINGERPRINT ID CREATED).
Kept in memory only, so the keys survive API restarts and API timeouts but not
an engine restart.")

(defconst organon-idempotency-ttl (* 24 60 60)
  "Seconds a key is remembered, as long as the API remembers responses.")

(defconst organon-idempotency-max 10000
  "Most keys remembered at once; the oldest are forgotten first.")

(defun organon--param-hex (params key &optional required)
  "Value of KEY in PARAMS, which must be 64 lowercase hex digits."
  (let ((value (organon-param-string params key required)))
    (when (and value (not (let ((case-fold-search nil))
                             (string-match-p "\\`[0-9a-f]\\{64\\}\\'" value))))
      (organon-signal "invalid" (format "%s must be 64 lowercase hex digits" key)))
    value))

(defun organon--remember-create (key fingerprint id)
  (let ((now (current-time)) oldest)
    (maphash (lambda (k entry)
               (if (> (float-time (time-subtract now (nth 2 entry))) organon-idempotency-ttl)
                   (remhash k organon--idempotency)
                 (when (or (null oldest)
                           (time-less-p (nth 2 entry) (nth 2 (gethash oldest organon--idempotency))))
                   (setq oldest k))))
             organon--idempotency)
    (when (and oldest (>= (hash-table-count organon--idempotency) organon-idempotency-max))
      (remhash oldest organon--idempotency))
    (puthash key (list fingerprint id now) organon--idempotency)))

(defun organon-idempotent (params create replay)
  "Call CREATE at most once per idempotency key in PARAMS and return its result.
The API passes `idempotency_key' and `idempotency_fingerprint' (hashes of the
client's key and of the payload).  CREATE returns a JSON alist with an `id'.
For a key seen before with the same fingerprint, REPLAY is called with the ID
it created instead; with another fingerprint the request is invalid.  This
covers retries after the API stopped waiting for a create the engine still
completed."
  (let* ((key (organon--param-hex params 'idempotency_key))
         (fingerprint (and key (organon--param-hex params 'idempotency_fingerprint t)))
         (entry (and key (gethash key organon--idempotency))))
    (when (and entry (> (float-time (time-since (nth 2 entry))) organon-idempotency-ttl))
      (remhash key organon--idempotency)
      (setq entry nil))
    (when (and entry (not (equal (car entry) fingerprint)))
      (organon-signal "invalid" "Idempotency-Key was already used with a different request body"))
    (or (and entry
             ;; The entry may be gone; then forget the key and create anew.
             (condition-case err
                 (funcall replay (nth 1 entry))
               (organon-error
                (unless (equal (nth 1 err) "not_found")
                  (signal (car err) (cdr err)))
                (remhash key organon--idempotency)
                nil)))
        (let ((result (funcall create)))
          (when key
            (organon--remember-create key fingerprint (alist-get 'id result)))
          result))))

(defmacro organon-with-entry (id &rest body)
  "Run BODY with point on the heading whose ID is ID, then save the file.
This is the only way methods modify existing entries."
  (declare (indent 1) (debug t))
  (let ((marker (make-symbol "marker")))
    `(let ((,marker (organon-find-id ,id)))
       (organon--call-in-buffer (marker-buffer ,marker)
                                (lambda () (goto-char ,marker) ,@body)))))

(defmacro organon-with-file (file &rest body)
  "Run BODY in a fresh buffer visiting FILE, then save it."
  (declare (indent 1) (debug t))
  `(organon--call-in-buffer (organon-fresh-buffer ,file) (lambda () ,@body)))

;;;; Parameters

(defconst organon--uuid-regexp
  "\\`[0-9a-f]\\{8\\}-[0-9a-f]\\{4\\}-[0-9a-f]\\{4\\}-[0-9a-f]\\{4\\}-[0-9a-f]\\{12\\}\\'")

(defun organon-param (params key &optional required)
  "Value of KEY in PARAMS (an alist).  Signal invalid if REQUIRED and absent."
  (let ((value (alist-get key params)))
    (when (and required (null value))
      (organon-signal "invalid" (format "missing parameter: %s" key)))
    value))

(defun organon-param-string (params key &optional required)
  (let ((value (organon-param params key required)))
    (when (and value (not (stringp value)))
      (organon-signal "invalid" (format "%s must be a string" key)))
    value))

(defun organon-param-enum (params key allowed &optional default)
  (let ((value (organon-param-string params key)))
    (cond ((null value) default)
          ((member value allowed) value)
          (t (organon-signal "invalid" (format "%s must be one of %s" key
                                               (string-join allowed ", ")))))))

(defun organon-param-uuid (params key)
  (let ((value (organon-param-string params key t)))
    (unless (let ((case-fold-search nil)) (string-match-p organon--uuid-regexp value))
      (organon-signal "invalid" (format "%s must be a lowercase UUID" key)))
    value))

(defun organon-param-date (params key)
  "Validated \"YYYY-MM-DD\" under KEY, or nil when absent."
  (let ((value (organon-param-string params key)))
    (when value
      (unless (organon-valid-date-p value)
        (organon-signal "invalid" (format "%s must be a calendar date YYYY-MM-DD" key)))
      value)))

(defun organon-valid-date-p (string)
  "Non-nil if STRING is a real calendar date in YYYY-MM-DD form."
  (and (stringp string)
       (string-match "\\`\\([0-9]\\{4\\}\\)-\\([0-9]\\{2\\}\\)-\\([0-9]\\{2\\}\\)\\'" string)
       (let ((y (string-to-number (match-string 1 string)))
             (m (string-to-number (match-string 2 string)))
             (d (string-to-number (match-string 3 string))))
         (and (<= 1 m 12) (<= 1 d (calendar-last-day-of-month m y))))))

;;;; JSON helpers

(defun organon-json-date (absolute)
  "ABSOLUTE day number (as used by the Org agenda) as \"YYYY-MM-DD\"."
  (pcase-let ((`(,m ,d ,y) (calendar-gregorian-from-absolute absolute)))
    (format "%04d-%02d-%02d" y m d)))

(defun organon-json-instant (time)
  "TIME (an Emacs time value) as an RFC 3339 UTC timestamp."
  (format-time-string "%Y-%m-%dT%H:%M:%SZ" time t))

(defun organon-today ()
  "Current calendar date in the instance zone, as \"YYYY-MM-DD\"."
  (organon-json-date (org-today)))

;;;; Methods and dispatch

(defvar organon-methods (make-hash-table :test 'equal)
  "Method name -> function of one argument (the params alist).")

(defmacro organon-defmethod (name arglist docstring &rest body)
  "Define RPC method NAME.  ARGLIST is (PARAMS)."
  (declare (indent 3) (doc-string 3))
  (let ((fn (intern (format "organon-method--%s" name))))
    `(progn
       (defun ,fn ,arglist ,docstring ,@body)
       (puthash ,name #',fn organon-methods))))

(defvar organon-log-requests nil
  "When non-nil, log one line per request (method, outcome, duration) to stderr.")

(defun organon-dispatch (method params)
  "Run METHOD with PARAMS and return its result (JSON-serializable data)."
  (let ((fn (and (stringp method) (gethash method organon-methods))))
    (unless fn
      (organon-signal "invalid" (format "unknown method: %s"
                                        (truncate-string-to-width (format "%s" method) 64))))
    (when organon-config-error
      (organon-signal "unavailable" organon-config-error))
    (let ((organon--in-request t)
          (inhibit-message t))
      (funcall fn params))))

(defun organon--response (id ok payload)
  (json-serialize
   (if ok
       `((id . ,(or id :null)) (ok . t) (result . ,payload))
     `((id . ,(or id :null)) (ok . :false) (error . ,payload)))))

(defun organon--error-payload (code message data)
  `((code . ,code) (message . ,message) (data . ,(or data :null))))

(defun organon-handle-request (line)
  "Handle one request LINE (a JSON string) and return the response line."
  (let ((start (float-time)) id method outcome)
    (prog1
        (condition-case err
            (let ((request (json-parse-string line :object-type 'alist
                                              :null-object nil :false-object :false)))
              (unless (and (listp request) (consp request))
                (organon-signal "invalid" "request must be a JSON object"))
              (setq id (let ((v (alist-get 'id request))) (and (stringp v) v))
                    method (alist-get 'method request))
              (let ((params (alist-get 'params request)))
                (unless (listp params)
                  (organon-signal "invalid" "params must be a JSON object"))
                (prog1 (organon--response id t (organon-dispatch method params))
                  (setq outcome "ok"))))
          (organon-error
           (setq outcome (nth 1 err))
           (organon--response id nil (organon--error-payload (nth 1 err) (nth 2 err) (nth 3 err))))
          (json-error
           (setq outcome "invalid")
           (organon--response id nil (organon--error-payload "invalid" "malformed JSON request" nil)))
          (error
           (setq outcome "internal")
           (organon--response id nil (organon--error-payload "internal" (error-message-string err) nil))))
      (when organon-log-requests
        (message "organon: %s %s %.0fms" (if (stringp method) method "-") outcome
                 (* 1000 (- (float-time) start)))))))

;;;; Socket server

(defconst organon--max-request-bytes (* 1024 1024))

(defvar organon--server nil)

(defun organon-socket-path ()
  (expand-file-name "rpc.sock" organon-run-dir))

(defun organon--reply (proc response)
  (when (process-live-p proc)
    (process-send-string proc (concat response "\n"))
    (process-send-eof proc)))

(defun organon--serve (proc line)
  (let ((response (organon-handle-request line)))
    ;; The client may have given up (API timeout) and closed the connection;
    ;; the request itself is complete either way.
    (condition-case err
        (organon--reply proc response)
      (error (message "organon: could not send response: %s" (error-message-string err))))))

(defun organon--filter (proc chunk)
  (let ((buffered (concat (or (process-get proc :organon-buffer) "") chunk)))
    (cond
     ((string-match "\n" buffered)
      (process-put proc :organon-buffer nil)
      ;; Run outside the process filter: Org code should never execute in
      ;; filter context, and this serializes requests in the main loop.
      (run-at-time 0 nil #'organon--serve proc (substring buffered 0 (match-beginning 0))))
     ((> (string-bytes buffered) organon--max-request-bytes)
      (process-put proc :organon-buffer nil)
      (organon--reply proc (organon--response nil nil (organon--error-payload
                                                        "invalid" "request too large" nil))))
     (t (process-put proc :organon-buffer buffered)))))

(defun organon--sentinel (proc _event)
  (unless (or (process-live-p proc) (eq proc organon--server))
    (delete-process proc)))

(defun organon-start-server ()
  "Listen on the RPC socket (replacing a stale socket file)."
  (when (process-live-p organon--server) (delete-process organon--server))
  (make-directory organon-run-dir t)
  (let ((path (organon-socket-path)))
    (when (file-exists-p path) (delete-file path))
    (setq organon--server
          (with-file-modes #o700
            (make-network-process :name "organon-rpc" :server t :family 'local
                                  :service path :coding 'utf-8 :noquery t
                                  :filter #'organon--filter
                                  :sentinel #'organon--sentinel)))))

;;;; Built-in methods

(organon-defmethod "ping" (_params)
  "Health check.  Fails with \"unavailable\" while the instance is misconfigured.
Also says whether the note index is ready (organon-node.el)."
  `((status . "ok")
    ,@(and (fboundp 'organon-index-status)
           `((index . ,(alist-get 'state (organon-index-status)))))))

(organon-defmethod "meta" (_params)
  "Instance metadata."
  `((calendar_tz . ,organon-calendar-tz)
    (today . ,(organon-today))
    (doing_limit . ,organon-doing-limit)
    (engine_version . ,organon-version)
    ,@(and (fboundp 'organon-index-status)
           `((index . ,(organon-index-status))))))

;;;; Startup

(defun organon--env-dir (variable default)
  (let ((value (getenv variable)))
    (if (and value (not (string-empty-p value))) value default)))

(defun organon-start ()
  "Entry point of the engine daemon (see init.el)."
  (setq organon-log-requests t)
  (organon-configure (organon--env-dir "ORGANON_DATA_DIR" "/data")
                     (organon--env-dir "ORGANON_CACHE_DIR" "/cache")
                     (organon--env-dir "ORGANON_RUN_DIR" "/run/organon"))
  (if organon-config-error
      (message "organon: NOT CONFIGURED: %s" organon-config-error)
    (organon-update-id-locations)
    (run-hooks 'organon-startup-hook)
    ;; The first agenda build loads a lot of Org code (~3 s on a Pi 4); pay
    ;; that cost before reporting healthy rather than on the first request.
    (condition-case err
        (when (fboundp 'organon-agenda-day) (organon-agenda-day nil))
      (error (message "organon: agenda warm-up failed: %s" (error-message-string err)))))
  (organon-start-server)
  (message "organon: listening on %s (calendar_tz=%s)" (organon-socket-path)
           (or organon-calendar-tz "unset")))

(provide 'organon)

;;; organon.el ends here
