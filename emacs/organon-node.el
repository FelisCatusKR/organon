;;; organon-node.el --- Organon engine: knowledge nodes and the org-roam index  -*- lexical-binding: t; -*-

;; Copyright (c) 2026 Hansaem Woo
;; SPDX-License-Identifier: MIT

;;; Commentary:

;; RPC methods for knowledge nodes.  org-roam owns everything that has a
;; meaning there: the file format of a node, IDs, slugs, which links count and
;; which node they belong to, and backlinks.  This file validates input, writes
;; new node files through `organon-with-file', keeps the org-roam database in
;; step with the files, and serializes what org-roam stored.
;; Specs: openspec/specs/{knowledge-nodes,knowledge-index}.
;;
;; The index (org-roam's SQLite database) is a cache in the cache directory:
;;
;; - At startup it is brought up to date with the files; when it is missing it
;;   is rebuilt, and when it cannot be read it is deleted and rebuilt.
;; - Every file the engine saves is indexed from `after-save-hook'.  We don't
;;   enable `org-roam-db-autosync-mode': it also advises the primitives
;;   `rename-file' and `delete-file' (the engine disables runtime trampolines),
;;   and an error in its hook would escape from `save-buffer', so that a save
;;   that succeeded would look failed to the write wrapper.
;; - Before every node query the size and modification time of every file are
;;   compared with those recorded when it was last indexed.  On a difference
;;   (another process changed a file) org-roam syncs, re-parsing only files
;;   whose content hash changed.
;;
;; A node is any org-roam node that is not a task (`organon-task-state'): tasks
;; stay in the index so that the links they carry show up as backlinks, and are
;; left out of node queries.

;;; Code:

(require 'organon)
(require 'organon-task)                 ; text rules shared with tasks
(require 'org-roam)

;;;; Index

(defconst organon-index-file-name "org-roam.db"
  "File name of the org-roam database inside the cache directory.")

(defvar organon--index-stats (make-hash-table :test 'equal)
  "File -> (SIZE . MODIFICATION-TIME) as of when the file was last indexed.")

(defvar organon--index-state 'ready
  "ready, rebuilding or failed.  Ready by default: tests that never start the
engine sync in the daemon on the first node query.")

(defvar organon--index-progress nil
  "(DONE . TOTAL) files of the running rebuild, or nil.")

(defvar organon--index-process nil "The rebuild child process, or nil.")

(defun organon--index-configure ()
  "Point org-roam at the current instance (see `organon-configure-hook')."
  ;; Tests configure many instances in one process: drop the old connection
  ;; and any rebuild of the old instance.
  (org-roam-db--close-all)
  (clrhash org-roam-db--connection)
  (clrhash organon--index-stats)
  (when (process-live-p organon--index-process)
    (set-process-sentinel organon--index-process #'ignore)
    (delete-process organon--index-process))
  (setq organon--index-process nil
        organon--index-progress nil
        organon--index-state 'ready)
  (setq org-roam-directory organon-org-dir
        org-roam-db-location (expand-file-name organon-index-file-name organon-cache-dir)))

(add-hook 'organon-configure-hook #'organon--index-configure)

(defun organon--file-stat (file)
  (let ((attrs (file-attributes file)))
    (and attrs (cons (file-attribute-size attrs) (file-attribute-modification-time attrs)))))

(defun organon--index-after-save ()
  "Index the file the current buffer just saved.
A failure is logged rather than raised: the file itself is saved.  The file
is then not recorded as indexed, so the next node query syncs it.  During a
background rebuild nothing is indexed here: the swap syncs these files."
  (when (and organon-org-dir buffer-file-name (eq organon--index-state 'ready))
    (let ((file (expand-file-name buffer-file-name)))
      (when (org-roam-file-p file)
        (condition-case err
            (progn
              (org-roam-db-update-file file)
              (puthash file (organon--file-stat file) organon--index-stats))
          (error
           (remhash file organon--index-stats)
           (message "organon: could not index %s: %s"
                    (organon-relative-path file) (error-message-string err))))))))

(add-hook 'after-save-hook #'organon--index-after-save)

(defun organon--refresh-changed-buffers ()
  "Re-read buffers under the org directory whose files changed on disk.
org-roam parses a buffer that already visits a file, but hashes the file on
disk, so a stale buffer would be indexed with the new hash and never again."
  (dolist (buf (buffer-list))
    (let ((file (buffer-file-name buf)))
      (when (and file
                 (string-prefix-p organon-org-dir (expand-file-name file))
                 (not (verify-visited-file-modtime buf)))
        (organon-fresh-buffer file)))))

(defun organon-index-ensure-current ()
  "Bring the index in step with the files on disk, if any file changed."
  (let ((stats (mapcar (lambda (file) (cons file (organon--file-stat file)))
                       (org-roam-list-files))))
    (unless (and (= (length stats) (hash-table-count organon--index-stats))
                 (cl-every (lambda (entry)
                             (equal (cdr entry) (gethash (car entry) organon--index-stats)))
                           stats))
      (organon--refresh-changed-buffers)
      (let ((inhibit-message t))
        (org-roam-db-sync))
      ;; Stats taken before the sync: a file that changed during it differs
      ;; from its record and is synced again by the next query.
      (clrhash organon--index-stats)
      (pcase-dolist (`(,file . ,stat) stats)
        (when stat (puthash file stat organon--index-stats))))))

;;;; Rebuilding in the background
;;
;; A full rebuild takes about 25 s per 1,000 notes on a Pi 4 and would block
;; every request, so it runs in a child batch Emacs that writes a new database
;; next to the old one; the daemon swaps it in when the child exits.  Until
;; then node methods answer "index_rebuilding" and tasks work as usual.

(defvar organon-init-file nil)          ; set by init.el

(defconst organon--progress-regexp "\\`organon-index-progress \\([0-9]+\\) \\([0-9]+\\)\\'"
  "The only line the daemon accepts from the child (besides logging it).")

(defun organon-index-status ()
  "The index state as reported by ping and meta."
  `((state . ,(symbol-name organon--index-state))
    (files_done . ,(if organon--index-progress (car organon--index-progress) :null))
    (files_total . ,(if organon--index-progress (cdr organon--index-progress) :null))))

(defun organon--index-require-ready ()
  "Fail the current node request unless the index can answer it."
  (pcase organon--index-state
    ('rebuilding (organon-signal "index_rebuilding" "the note index is being rebuilt; try again shortly"))
    ('failed (organon-signal "internal" "the note index could not be rebuilt; restart the engine to retry"))))

(defun organon--index-rebuild-file ()
  (concat org-roam-db-location ".rebuild"))

(defun organon--index-reset ()
  "Delete the database, so that the next sync rebuilds it from the files."
  (org-roam-db--close-all)
  (clrhash organon--index-stats)
  (when (file-exists-p org-roam-db-location)
    (delete-file org-roam-db-location)))

(defun organon--index-filter (proc chunk)
  "Read progress lines from the rebuild child; log anything else."
  (let ((pending (concat (or (process-get proc :organon-pending) "") chunk)))
    (while (string-match "\n" pending)
      (let ((line (substring pending 0 (match-beginning 0))))
        (setq pending (substring pending (match-end 0)))
        (if (string-match organon--progress-regexp line)
            (setq organon--index-progress (cons (string-to-number (match-string 1 line))
                                                (string-to-number (match-string 2 line))))
          (unless (string-empty-p line)
            (message "organon: index rebuild: %s" line)))))
    (process-put proc :organon-pending pending)))

(defun organon--index-sentinel (proc _event)
  "Swap in the database the child built, or record that it failed."
  (unless (process-live-p proc)
    (let ((output (process-get proc :organon-output))
          (organon--in-request t))
      (setq organon--index-process nil
            organon--index-progress nil)
      (if (and (eq (process-status proc) 'exit) (= (process-exit-status proc) 0)
               (file-exists-p output))
          (condition-case err
              (progn
                (org-roam-db--close-all)
                (rename-file output org-roam-db-location t)
                (clrhash organon--index-stats)
                (setq organon--index-state 'ready)
                ;; Files the engine wrote during the rebuild were not indexed.
                (organon-index-ensure-current)
                (message "organon: index rebuilt, %d entries (%.1fs)"
                         (caar (org-roam-db-query [:select (funcall count *) :from nodes]))
                         (- (float-time) (process-get proc :organon-start))))
            (error
             (setq organon--index-state 'failed)
             (message "organon: could not use the rebuilt index: %s" (error-message-string err))))
        (setq organon--index-state 'failed)
        (when (file-exists-p output) (delete-file output))
        (message "organon: index rebuild failed (%s %s)"
                 (process-status proc) (process-exit-status proc))))))

(defun organon-index-rebuild-in-background ()
  "Start a child batch Emacs that builds a new database from the files."
  (let ((output (organon--index-rebuild-file)))
    (organon--index-reset)
    (when (file-exists-p output) (delete-file output))
    (setq organon--index-state 'rebuilding
          organon--index-progress nil
          organon--index-process
          (make-process
           :name "organon-index"
           :command (list (expand-file-name invocation-name invocation-directory)
                          "-q" "--batch" "-l" organon-init-file
                          "-f" "organon-index-build"
                          organon-data-dir organon-cache-dir output)
           :connection-type 'pipe
           :noquery t
           :coding 'utf-8
           :filter #'organon--index-filter
           :sentinel #'organon--index-sentinel))
    (process-put organon--index-process :organon-output output)
    (process-put organon--index-process :organon-start (float-time))
    (message "organon: rebuilding the index in the background")))

(defun organon-index-build ()
  "Entry point of the rebuild child: DATA-DIR CACHE-DIR OUTPUT on the command
line.  Builds the database for DATA-DIR into OUTPUT and exits."
  (pcase-let ((`(,data-dir ,cache-dir ,output) command-line-args-left))
    (setq command-line-args-left nil)
    ;; The daemon owns the org-id cache; this process must not rewrite it.
    (setq org-id-track-globally nil)
    (organon-configure data-dir cache-dir (make-temp-file "organon-index-run-" t))
    (when organon-config-error
      (message "%s" organon-config-error)
      (kill-emacs 1))
    (setq org-id-locations-file (make-temp-file "organon-index-ids-")
          org-roam-db-location output)
    (let ((total (length (org-roam-list-files)))
          (done 0)
          (organon--in-request t))
      (advice-add 'org-roam-db-update-file :before
                  (lambda (&rest _)
                    ;; stderr: unbuffered, unlike stdout into a pipe, and
                    ;; merged into the same pipe by `make-process'.
                    (princ (format "organon-index-progress %d %d\n" (cl-incf done) (max done total))
                           #'external-debugging-output)))
      (condition-case err
          (let ((inhibit-message t))
            (org-roam-db-sync)
            (org-roam-db--close-all)
            (kill-emacs 0))
        (error
         (message "%s" (error-message-string err))
         (kill-emacs 1))))))

(defun organon-index-startup ()
  "Make the index usable without delaying the engine (`organon-startup-hook').
With a database, sync it incrementally here (fast); without one, or with one
that cannot be read, rebuild it in a child process."
  (let ((start (float-time))
        ;; Not a request, but a prompt here would hang the startup.
        (organon--in-request t))
    (if (not (file-exists-p org-roam-db-location))
        (organon-index-rebuild-in-background)
      (condition-case err
          (progn
            (organon-index-ensure-current)
            ;; The count also opens the database when no file changed (an
            ;; empty org/), so a broken one is found here too.
            (message "organon: index ready, %d entries (%.1fs)"
                     (caar (org-roam-db-query [:select (funcall count *) :from nodes]))
                     (- (float-time) start)))
        (error
         (message "organon: index unusable (%s)" (error-message-string err))
         (organon-index-rebuild-in-background))))))

(add-hook 'organon-startup-hook #'organon-index-startup)

;;;; Input

(defconst organon-node-query-max-length 200)
(defconst organon-node-max-aliases 50)

(defun organon--clean-node-line (value what)
  "Validate VALUE, a node title or alias, and return the form to store.
Unlike task titles these are not headings, so only the line rules apply, and
links are refused: org-roam would store a title with a link as the link's
description and count the link as one of the node's links."
  (unless (and (stringp value) (not (string-blank-p value)))
    (organon-signal "invalid" (format "%s must be a non-empty string" what)))
  (when (string-match-p "[\0-\37\177]" value)
    (organon-signal "invalid" (format "%s must be a single line without control characters" what)))
  (when (> (length value) organon-title-max-length)
    (organon-signal "invalid" (format "%s must be at most %d characters" what organon-title-max-length)))
  (when (string-search "[[" value)
    (organon-signal "invalid" (format "%s must not contain an Org link ([[...]])" what)))
  (organon--deactivate-timestamps (string-trim value)))

(defun organon--param-aliases (params)
  (let ((aliases (organon-param params 'aliases)))
    (cond ((null aliases) nil)
          ((not (vectorp aliases)) (organon-signal "invalid" "aliases must be an array of strings"))
          ((> (length aliases) organon-node-max-aliases)
           (organon-signal "invalid" (format "at most %d aliases" organon-node-max-aliases)))
          (t (delete-dups (mapcar (lambda (alias) (organon--clean-node-line alias "alias"))
                                  aliases))))))

(defun organon--param-query (params)
  "The search string, or nil for none."
  (let ((q (organon-param-string params 'q)))
    (when (and q (not (string-empty-p q)))
      (when (> (length q) organon-node-query-max-length)
        (organon-signal "invalid" (format "q must be at most %d characters" organon-node-query-max-length)))
      (when (string-match-p "[\0-\37\177]" q)
        (organon-signal "invalid" "q must not contain control characters"))
      q)))

(defun organon--param-tag (params)
  (let ((tag (organon-param-string params 'tag)))
    (when (and tag (not (string-match-p "\\`[[:alnum:]_@#%]+\\'" tag)))
      (organon-signal "invalid" "tag must be letters, digits or _@#%"))
    tag))

;;;; Serialization

(defun organon--task-todo-p (todo)
  "Non-nil if TODO, the keyword org-roam stored for a node, makes it a task.
Every org-roam node has an ID, so the keyword alone decides."
  (and todo (member todo organon-task-states)))

(defun organon--archived-p (file)
  (string-prefix-p (expand-file-name "archive/" organon-org-dir) (expand-file-name file)))

(defun organon--file-keyword-line-p ()
  "Non-nil if the line at point is a keyword of the file (#+title:, ...).
Affiliated keywords (#+caption:, #+name:, #+attr_html:, ...) are not: they
belong to the element below them, which is part of the body."
  (let ((case-fold-search t))
    (and (looking-at "[ \t]*#\\+\\([[:alnum:]_-]+\\):")
         (let ((key (upcase (match-string 1))))
           (not (or (member key org-element-affiliated-keywords)
                    (string-prefix-p "ATTR_" key)))))))

(defun organon--file-node-body ()
  "Text of the file node in the current buffer: after its property drawer
(which only comments and blank lines may precede), the file's keywords and
blank lines, up to the first heading; unescaped."
  (save-excursion
    (goto-char (point-min))
    (let ((end (save-excursion
                 (if (re-search-forward org-outline-regexp-bol nil t) (match-beginning 0) (point-max)))))
      ;; Blank and comment lines (e.g. "# -*- mode: org -*-")
      (while (and (< (point) end) (looking-at-p "[ \t]*\\(?:#\\(?:[ \t].*\\)?\\)?$"))
        (forward-line 1))
      (when (looking-at org-property-drawer-re)
        (goto-char (match-end 0))
        (forward-line 1))
      (while (and (< (point) end)
                  (or (looking-at-p "[ \t]*$") (organon--file-keyword-line-p)))
        (forward-line 1))
      (if (>= (point) end)
          ""
        (organon-unescape-body
         (string-trim-right (buffer-substring-no-properties (point) end)))))))

(defun organon--node-body (node)
  (with-current-buffer (organon-fresh-buffer (org-roam-node-file node))
    (save-excursion
      (save-restriction
        (widen)
        (goto-char (org-roam-node-point node))
        (if (= (org-roam-node-level node) 0)
            (organon--file-node-body)
          (organon--body-at-point))))))

(defun organon--node-json (node &optional with-body)
  "NODE (an `org-roam-node') as a JSON-ready alist, with its body if WITH-BODY."
  `((id . ,(org-roam-node-id node))
    (title . ,(org-roam-node-title node))
    (aliases . ,(vconcat (org-roam-node-aliases node)))
    (tags . ,(vconcat (org-roam-node-tags node)))
    ,@(and with-body `((body . ,(organon--node-body node))))
    (location_hint . ,(organon-relative-path (org-roam-node-file node)))))

(defun organon--sort-by-title (items)
  "ITEMS (alists with title and id) ordered by title ignoring case, then id."
  (sort items (lambda (a b)
                (let ((ta (downcase (alist-get 'title a)))
                      (tb (downcase (alist-get 'title b))))
                  (if (string= ta tb)
                      (string< (alist-get 'id a) (alist-get 'id b))
                    (string< ta tb))))))

(defun organon--refs (ids)
  "The entries with IDS that exist in the index, as {id, title, kind}, sorted."
  (when ids
    (organon--sort-by-title
     (mapcar (pcase-lambda (`(,id ,title ,todo))
               `((id . ,id) (title . ,title) (kind . ,(if (organon--task-todo-p todo) "task" "node"))))
             (org-roam-db-query [:select [id title todo] :from nodes :where (in id $v1)]
                                (vconcat (delete-dups (copy-sequence ids))))))))

(defun organon--node (id)
  "The node ID from an up-to-date index; not_found if there is none."
  (organon--index-require-ready)
  (organon-index-ensure-current)
  (let ((node (org-roam-node-from-id id)))
    (unless node
      (organon-signal "not_found" (format "no node with id %s" id)))
    (when (organon--task-todo-p (org-roam-node-todo node))
      (organon-signal "not_found" "entry is a task, not a node"))
    node))

;;;; node.create

(defconst organon-node-slug-max-length 60
  "Characters of the slug kept in a file name; 60 Hangul syllables are 180
bytes, well below the usual 255-byte limit.")

(defun organon--node-file-name (title)
  "A new file under knowledge/ named like org-roam's default capture template,
<YYYYMMDDHHMMSS>-<slug>.org; never an existing one."
  (let* ((slug (org-roam-node-slug (org-roam-node-create :title title)))
         (slug (string-trim-right (substring slug 0 (min (length slug) organon-node-slug-max-length))
                                  "_+"))
         (slug (if (string-empty-p slug) "node" slug))
         (stem (concat (format-time-string "%Y%m%d%H%M%S") "-" slug))
         (dir (expand-file-name "knowledge/" organon-org-dir))
         (candidate (expand-file-name (concat stem ".org") dir))
         (n 1))
    (while (or (file-exists-p candidate) (get-file-buffer candidate))
      (setq n (1+ n)
            candidate (expand-file-name (format "%s-%d.org" stem n) dir)))
    candidate))

(defun organon--node-create (params)
  "Create a knowledge node: a new file under knowledge/ with a file-level ID."
  (organon--index-require-ready)        ; before anything is written
  (let* ((title (organon--clean-node-line (organon-param params 'title t) "title"))
         (body (organon--param-body params))
         (tags (organon--param-tags params))
         (aliases (organon--param-aliases params))
         (file (organon--node-file-name title)))
    (make-directory (file-name-directory file) t)
    (let ((id (organon-with-file file
                (erase-buffer)
                (insert "#+title: " title "\n")
                (when body
                  (insert "\n" (organon-escape-body body) "\n"))
                (goto-char (point-min))
                (prog1 (org-id-get-create)
                  ;; Each call puts its alias first.
                  (dolist (alias (reverse aliases))
                    (org-roam-property-add "ROAM_ALIASES" alias))
                  (when tags (org-roam-tag-add tags))))))
      ;; The file is saved.  If org-roam could not index it, say so rather
      ;; than answering "not found" for a node that was just created.
      (organon-index-ensure-current)
      (let ((node (org-roam-node-from-id id)))
        (unless node
          (organon-signal "internal" (format "saved %s, but org-roam could not index it"
                                             (organon-relative-path file))))
        (organon--node-json node t)))))

(organon-defmethod "node.create" (params)
  "Create a node; a repeated idempotency key returns the node it created."
  (organon-idempotent params
                      (lambda () (organon--node-create params))
                      (lambda (id) (organon--node-json (organon--node id) t))))

;;;; Queries

(organon-defmethod "node.get" (params)
  "Return the node with the given id."
  (organon--node-json (organon--node (organon-param-uuid params 'id)) t))

(organon-defmethod "nodes.search" (params)
  "Nodes outside archive/ whose title or an alias contains q (ignoring case)
and that carry tag; both optional.  Filtering happens here, in Lisp, so
request values never become SQL."
  (let ((q (let ((q (organon--param-query params))) (and q (downcase q))))
        (tag (organon--param-tag params))
        (aliases (make-hash-table :test 'equal))
        (tags (make-hash-table :test 'equal))
        result)
    (organon--index-require-ready)
    (organon-index-ensure-current)
    ;; Rows come back in insertion order, which is file order; push reverses.
    (pcase-dolist (`(,id ,alias) (org-roam-db-query [:select [node-id alias] :from aliases]))
      (push alias (gethash id aliases)))
    (pcase-dolist (`(,id ,tag) (org-roam-db-query [:select [node-id tag] :from tags]))
      (push tag (gethash id tags)))
    (pcase-dolist (`(,id ,title ,file ,todo) (org-roam-db-query [:select [id title file todo] :from nodes]))
      (let ((node-aliases (reverse (gethash id aliases)))
            (node-tags (reverse (gethash id tags))))
        (when (and (not (organon--task-todo-p todo))
                   (not (organon--archived-p file))
                   (or (null q)
                       (cl-some (lambda (s) (string-search q (downcase s)))
                                (cons title node-aliases)))
                   (or (null tag) (member tag node-tags)))
          (push `((id . ,id) (title . ,title) (aliases . ,(vconcat node-aliases))
                  (tags . ,(vconcat node-tags)) (location_hint . ,(organon-relative-path file)))
                result))))
    (vconcat (organon--sort-by-title result))))

(organon-defmethod "node.backlinks" (params)
  "Nodes and tasks that link to the node with an id: link, once per source."
  (let ((node (organon--node (organon-param-uuid params 'id))))
    (vconcat (organon--refs (mapcar (lambda (backlink)
                                      (org-roam-node-id (org-roam-backlink-source-node backlink)))
                                    (org-roam-backlinks-get node :unique t))))))

(organon-defmethod "node.links" (params)
  "Nodes and tasks the node links to with id: links; dangling links left out."
  (let ((node (organon--node (organon-param-uuid params 'id))))
    (vconcat (organon--refs (mapcar #'car (org-roam-db-query
                                           [:select :distinct [dest] :from links
                                            :where (and (= source $s1) (= type "id"))]
                                           (org-roam-node-id node)))))))

(provide 'organon-node)

;;; organon-node.el ends here
