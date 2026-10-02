;;; organon-task.el --- Organon engine: tasks and agenda queries  -*- lexical-binding: t; -*-

;; Copyright (c) 2026 Hansaem Woo
;; SPDX-License-Identifier: MIT

;;; Commentary:

;; RPC methods for tasks.  Every behavior that has a meaning in Org (state
;; changes, repeaters, warnings, agenda membership) is delegated to Org; this
;; file only validates input, positions new headings, and serializes what Org
;; stored.  Specs: openspec/specs/{task-lifecycle,
;; task-recurrence,agenda-queries}.

;;; Code:

(require 'organon)

;;;; Input validation and escaping

(defconst organon-title-max-length 500)
(defconst organon-body-max-length 100000)

(defun organon--deactivate-timestamps (string)
  "Make active timestamps and diary sexps in STRING inactive.
An active timestamp anywhere in a heading or body puts the entry on the
agenda, and a diary sexp (<%%(...)> or %%(...)) is evaluated by the agenda."
  (let ((case-fold-search nil))
    (replace-regexp-in-string
     "<%%(" "[%%("
     (replace-regexp-in-string
      "<\\([0-9]\\{4\\}-[0-9]\\{2\\}-[0-9]\\{2\\}[^<>\n]*\\)>" "[\\1]" string t)
     t t)))

(defun organon-clean-title (title)
  "Validate TITLE and return the form stored in the heading."
  (unless (and (stringp title) (not (string-blank-p title)))
    (organon-signal "invalid" "title is required"))
  (when (string-match-p "[\0-\37\177]" title)
    (organon-signal "invalid" "title must be a single line without control characters"))
  (when (> (length title) organon-title-max-length)
    (organon-signal "invalid" (format "title must be at most %d characters" organon-title-max-length)))
  (let ((title (string-trim title))
        (case-fold-search nil))
    (when (string-match-p "\\`\\[#[^]]*\\]" title)
      (organon-signal "invalid" "title must not start with a priority cookie"))
    (when (string-match-p "\\`COMMENT\\_>" title)
      (organon-signal "invalid" "title must not start with COMMENT"))
    (when (string-match-p "\\(?:\\`\\|[ \t]\\):[[:alnum:]_@#%:]+:\\'" title)
      (organon-signal "invalid" "title must not end with a tag list such as :tag:"))
    (organon--deactivate-timestamps title)))

(defconst organon--structural-line-regexp
  "^\\([ \t]*\\)\\(,*\\(?:\\*\\|#\\+\\|:[[:alnum:]_-]*:\\|%%(\\)\\)"
  "Lines Org would read as structure inside an entry: headings, #+ keywords,
drawer boundaries and diary sexps.  Org's own convention for literal text
(`org-escape-code-in-string') prefixes such lines with a comma.")

(defun organon-escape-body (body)
  "Return BODY made safe to store under a heading."
  (replace-regexp-in-string
   organon--structural-line-regexp "\\1,\\2"
   (organon--deactivate-timestamps (replace-regexp-in-string "\r\n?" "\n" body))
   t))

(defun organon-unescape-body (text)
  "Inverse of `organon-escape-body' (except for deactivated timestamps)."
  (replace-regexp-in-string
   "^\\([ \t]*\\),\\(,*\\(?:\\*\\|#\\+\\|:[[:alnum:]_-]*:\\|%%(\\)\\)" "\\1\\2" text t))

(defun organon--param-body (params)
  (let ((body (organon-param-string params 'body)))
    (when (and body (> (length body) organon-body-max-length))
      (organon-signal "invalid" (format "body must be at most %d characters" organon-body-max-length)))
    (and body (not (string-empty-p body)) body)))

(defun organon--param-tags (params)
  (let ((tags (organon-param params 'tags)))
    (cond ((null tags) nil)
          ((not (vectorp tags)) (organon-signal "invalid" "tags must be an array of strings"))
          (t (mapcar (lambda (tag)
                       (unless (and (stringp tag) (string-match-p "\\`[[:alnum:]_@#%]+\\'" tag))
                         (organon-signal "invalid" (format "invalid tag %S (letters, digits, _@#%% only)" tag)))
                       tag)
                     tags)))))

(defun organon--param-timestamp (params key &optional allow-warning)
  "Org timestamp string built from the object under KEY, or nil.
The object is {date, time?, repeat?, warning_days?}.  Only syntax is checked
here; Org parses and places the result."
  (let ((value (organon-param params key)))
    (when value
      (unless (consp value)
        (organon-signal "invalid" (format "%s must be an object" key)))
      (let ((date (alist-get 'date value))
            (time (alist-get 'time value))
            (repeat (alist-get 'repeat value))
            (warning (alist-get 'warning_days value))
            (case-fold-search nil))
        (unless (organon-valid-date-p date)
          (organon-signal "invalid" (format "%s.date must be a calendar date YYYY-MM-DD" key)))
        (when (and time (not (and (stringp time)
                                  (string-match-p "\\`\\(?:[01][0-9]\\|2[0-3]\\):[0-5][0-9]\\'" time))))
          (organon-signal "invalid" (format "%s.time must be HH:MM" key)))
        (when (and repeat (not (and (stringp repeat)
                                    (string-match-p "\\`\\(?:\\+\\|\\+\\+\\|\\.\\+\\)[1-9][0-9]\\{0,2\\}[hdwmy]\\'" repeat))))
          (organon-signal "invalid" (format "%s.repeat must look like +1m, ++1w or .+3d" key)))
        (when (and repeat (string-suffix-p "h" repeat) (not time))
          (organon-signal "invalid" (format "%s: an hourly repeat needs a time" key)))
        (when warning
          (unless allow-warning
            (organon-signal "invalid" (format "%s does not take warning_days" key)))
          (unless (and (natnump warning) (<= 1 warning 365))
            (organon-signal "invalid" (format "%s.warning_days must be 1..365" key))))
        (concat "<" date
                (if time (concat " " time) "")
                (if repeat (concat " " repeat) "")
                (if warning (format " -%dd" warning) "")
                ">")))))

;;;; Serialization

(defun organon--timestamp-json (ts)
  "Planning timestamp TS (an org-element object) as {date,time,repeat,warning_days}."
  (if (not ts)
      :null
    (let ((hour (org-element-property :hour-start ts))
          (rtype (org-element-property :repeater-type ts))
          (wunit (org-element-property :warning-unit ts))
          (wvalue (org-element-property :warning-value ts)))
      `((date . ,(format "%04d-%02d-%02d" (org-element-property :year-start ts)
                         (org-element-property :month-start ts)
                         (org-element-property :day-start ts)))
        (time . ,(if hour (format "%02d:%02d" hour (org-element-property :minute-start ts)) :null))
        (repeat . ,(if rtype
                       (format "%s%d%s"
                               (pcase rtype ('cumulate "+") ('catch-up "++") ('restart ".+"))
                               (org-element-property :repeater-value ts)
                               (pcase (org-element-property :repeater-unit ts)
                                 ('hour "h") ('day "d") ('week "w") ('month "m") ('year "y")))
                     :null))
        (warning_days . ,(pcase wunit
                           ('day wvalue)
                           ('week (* 7 wvalue))
                           (_ :null)))))))

(defun organon--in-org-subdir-p (subdir)
  (and buffer-file-name
       (string-prefix-p (expand-file-name (concat subdir "/") organon-org-dir) buffer-file-name)))

(defun organon--project-json ()
  "The project of the heading at point: its level-1 ancestor in projects/."
  (or (and (organon--in-org-subdir-p "projects")
           (save-excursion
             (org-back-to-heading t)
             (when (> (org-current-level) 1)
               (while (org-up-heading-safe))
               (let ((id (org-entry-get nil "ID")))
                 (when id
                   `((id . ,id) (title . ,(org-get-heading t t t t))))))))
      :null))

(defun organon--body-at-point ()
  "Text of the entry at point after its metadata and drawers, unescaped."
  (save-excursion
    (org-back-to-heading t)
    (let ((end (save-excursion (outline-next-heading) (point))))
      (org-end-of-meta-data t)
      (if (>= (point) end)
          ""
        (organon-unescape-body
         (string-trim-right (buffer-substring-no-properties (point) end)))))))

(defun organon--entry-version ()
  "Opaque version of the entry at point: a hash of its text (heading, planning,
drawers and body, not children).  Any change to the entry changes it."
  (save-excursion
    (org-back-to-heading t)
    (let ((start (point))
          (end (save-excursion (outline-next-heading) (point))))
      (substring (secure-hash 'sha256 (buffer-substring-no-properties start end)) 0 16))))

(defun organon--repeating-p ()
  "Non-nil if the entry at point has a repeater on SCHEDULED or DEADLINE."
  (let ((headline (save-excursion (org-back-to-heading t) (org-element-at-point))))
    (cl-some (lambda (key)
               (let ((ts (org-element-property key headline)))
                 (and ts (org-element-property :repeater-type ts))))
             '(:scheduled :deadline))))

(defun organon-task-json ()
  "The task heading at point as a JSON-ready alist."
  (save-excursion
    (org-back-to-heading t)
    (let* ((headline (org-element-at-point))
           (priority (org-element-property :priority headline))
           (closed (org-element-property :closed headline)))
      `((id . ,(or (org-entry-get nil "ID") :null))
        (title . ,(org-get-heading t t t t))
        (state . ,(or (org-get-todo-state) :null))
        (priority . ,(if priority (char-to-string priority) :null))
        (tags . ,(vconcat (org-get-tags nil t)))
        (scheduled . ,(organon--timestamp-json (org-element-property :scheduled headline)))
        (deadline . ,(organon--timestamp-json (org-element-property :deadline headline)))
        (repeat_to_state . ,(or (org-entry-get nil "REPEAT_TO_STATE") :null))
        (closed_at . ,(if closed (organon-json-instant (org-timestamp-to-time closed)) :null))
        (project . ,(organon--project-json))
        (body . ,(organon--body-at-point))
        (version . ,(organon--entry-version))
        (location_hint . ,(organon-relative-path buffer-file-name))))))

(defun organon--task-json-at (marker)
  (with-current-buffer (marker-buffer marker)
    (save-excursion
      (save-restriction
        (widen)
        (goto-char marker)
        (organon-task-json)))))

;;;; task.get

(organon-defmethod "task.get" (params)
  "Return the task with the given id."
  (let ((marker (organon-find-id (organon-param-uuid params 'id))))
    (with-current-buffer (marker-buffer marker)
      (save-excursion
        (goto-char marker)
        (unless (org-get-todo-state)
          (organon-signal "not_found" "entry is not a task"))))
    (organon--task-json-at marker)))

;;;; task.create

(defun organon--project-marker (id)
  "Marker at the project heading ID, which must be level 1 in projects/."
  (let ((marker (condition-case nil
                    (organon-find-id id)
                  (organon-error nil))))
    (unless (and marker
                 (with-current-buffer (marker-buffer marker)
                   (save-excursion
                     (goto-char marker)
                     (and (organon--in-org-subdir-p "projects")
                          (= (org-current-level) 1)))))
      (organon-signal "invalid" (format "project_id %s is not a project heading" id)))
    marker))

(organon-defmethod "task.create" (params)
  "Create a task in the inbox, or as the last child of a project."
  (let* ((title (organon-clean-title (organon-param params 'title t)))
         (state (organon-param-enum params 'state '("TODO" "NEXT") "TODO"))
         (priority (organon-param-enum params 'priority '("A" "B" "C")))
         (tags (organon--param-tags params))
         (body (organon--param-body params))
         (scheduled (organon--param-timestamp params 'scheduled))
         (deadline (organon--param-timestamp params 'deadline t))
         (repeat-to (organon-param-enum params 'repeat_to_state '("TODO" "NEXT")))
         (project-id (and (organon-param params 'project_id) (organon-param-uuid params 'project_id)))
         (project (and project-id (organon--project-marker project-id)))
         (file (if project
                   (buffer-file-name (marker-buffer project))
                 (expand-file-name "tasks/inbox.org" organon-org-dir))))
    (make-directory (file-name-directory file) t)
    (organon-with-file file
      (goto-char (if project
                     (save-excursion (goto-char project) (org-end-of-subtree t t) (point))
                   (point-max)))
      (unless (bolp) (insert "\n"))
      (insert (make-string (if project 2 1) ?*) " " state " " title "\n")
      (forward-line -1)
      (let ((heading (point-marker)))
        (org-id-get-create)
        (when priority (org-priority (string-to-char priority)))
        (when tags (org-set-tags tags))
        (when deadline (org-deadline nil deadline))
        (when scheduled (org-schedule nil scheduled))
        (when repeat-to (org-entry-put nil "REPEAT_TO_STATE" repeat-to))
        (when body
          (goto-char heading)
          (org-end-of-meta-data t)
          (unless (bolp) (insert "\n"))
          (insert (organon-escape-body body) "\n"))
        (goto-char heading)
        (organon-task-json)))))

;;;; Transitions

(defun organon--strip-repeaters ()
  "Remove the repeaters from the SCHEDULED and DEADLINE stamps at point.
Rewrites each stamp from Org's own parse with the repeater unset."
  (save-excursion
    (org-back-to-heading t)
    (let* ((headline (org-element-at-point))
           (stamps (delq nil (list (org-element-property :scheduled headline)
                                   (org-element-property :deadline headline)))))
      ;; Rewrite from the end so earlier positions stay valid.
      (dolist (ts (sort stamps (lambda (a b) (> (org-element-property :begin a)
                                                (org-element-property :begin b)))))
        (when (org-element-property :repeater-type ts)
          (let ((new (org-element-copy ts))
                (begin (org-element-property :begin ts))
                (end (- (org-element-property :end ts) (or (org-element-property :post-blank ts) 0))))
            (dolist (prop '(:repeater-type :repeater-value :repeater-unit :raw-value))
              (org-element-put-property new prop nil))
            (goto-char begin)
            (delete-region begin end)
            (insert (org-element-interpret-data new))))))))

(defun organon--count-state (state)
  (let ((org-agenda-files (organon-agenda-files)))
    (length (org-map-entries #'ignore (format "TODO=%S" state) 'agenda))))

(organon-defmethod "task.transition" (params)
  "Move a task to another state.  ACTION is start, wait, complete, skip or cancel.
expected_state must match the stored state, and expected_version the stored
version (required for repeating tasks, which return to an open state, so a
retried request would otherwise move their dates twice)."
  (let* ((id (organon-param-uuid params 'id))
         (action (organon-param-enum params 'action '("start" "wait" "complete" "skip" "cancel")))
         (expected (organon-param-string params 'expected_state t))
         (expected-version (organon-param-string params 'expected_version))
         (task
          (organon-with-entry id
            (let ((current (org-get-todo-state))
                  (version (organon--entry-version)))
              (unless current
                (organon-signal "not_found" "entry is not a task"))
              (when (and (not expected-version) (organon--repeating-p))
                (organon-signal "invalid" "expected_version is required for repeating tasks"))
              (unless (and (equal current expected)
                           (or (null expected-version) (equal version expected-version)))
                (organon-signal "conflict"
                                (if (equal current expected)
                                    "the task changed since expected_version was read"
                                  (format "expected state %s but the task is %s" expected current))
                                `((actual_state . ,current) (actual_version . ,version))))
              (pcase action
                ("start" (org-todo "DOING"))
                ("wait" (org-todo "WAITING"))
                ("complete" (org-todo "DONE"))
                ;; On a repeating task Org treats CANCELLED like DONE: the
                ;; occurrence is logged and the dates move on.
                ("skip" (org-todo "CANCELLED"))
                ("cancel" (organon--strip-repeaters) (org-todo "CANCELLED")))
              (organon-task-json))))
         (warnings (when (and (equal action "start")
                              (> (organon--count-state "DOING") organon-doing-limit))
                     '("doing_limit_exceeded"))))
    `((task . ,task) (warnings . ,(vconcat warnings)))))

;;;; Agenda queries
;;
;; Agenda membership, warning windows and overdue status are Org's: the engine
;; builds a one-day agenda and reads the text properties Org attaches to each
;; line (`org-hd-marker', `type', `ts-date').  The rendered text is never parsed.

(defconst organon--agenda-buffer "*organon-agenda*")

(defun organon--agenda-kind (type)
  (pcase type
    ((or "scheduled" "past-scheduled" "deadline" "upcoming-deadline") type)
    ((or "timestamp" "sexp" "block") "event")))

(defun organon--query-day (date)
  "Absolute day number for DATE (\"YYYY-MM-DD\") or for today."
  (if date (org-time-string-to-absolute date) (org-today)))

(defun organon--build-agenda (date log-mode collect)
  "Build a one-day agenda for DATE and call COLLECT on each line that has a
heading marker.  LOG-MODE non-nil shows only completion log entries.
Org releases its markers when the agenda buffer is killed, so COLLECT must
copy any marker it keeps."
  (let ((files (organon-agenda-files)))
    (when files
      (mapc #'organon-fresh-buffer files)
      (let ((org-agenda-files files)
            (org-agenda-buffer-name organon--agenda-buffer)
            (org-agenda-span 1)
            (org-agenda-start-with-log-mode (and log-mode '(closed state)))
            (org-agenda-log-mode-items '(closed state))
            (org-agenda-show-log (and log-mode 'only))
            (inhibit-message t))
        ;; Org shows warnings, carried-over schedules and overdue deadlines only
        ;; on the agenda of the current day.  A query for DATE means "the agenda
        ;; as Org would show it if DATE were today", so let Org believe that
        ;; while it builds the agenda; every rule is still Org's.
        (let ((day (organon--query-day date)))
          (cl-letf (((symbol-function 'org-today) (lambda () day)))
            (save-window-excursion (org-agenda-list nil date 1))))
        (with-current-buffer org-agenda-buffer-name
          (unwind-protect
              (progn
                (goto-char (point-min))
                (while (not (eobp))
                  (when (get-text-property (point) 'org-hd-marker)
                    (funcall collect))
                  (forward-line 1)))
            (kill-buffer)))))))

(defun organon-agenda-day (date)
  "Entries Org Agenda shows for DATE, as a list of (KIND MARKER TS-DATE)."
  (let (entries)
    (organon--build-agenda
     date nil
     (lambda ()
       (let ((kind (organon--agenda-kind (get-text-property (point) 'type))))
         (when kind
           (push (list kind (copy-marker (get-text-property (point) 'org-hd-marker))
                       (get-text-property (point) 'ts-date))
                 entries)))))
    (nreverse entries)))

(defun organon--agenda-tasks (date)
  "Open tasks on the agenda for DATE: (TASK-JSON KIND TS-DATE), first entry per task."
  (let (seen result)
    (pcase-dolist (`(,kind ,marker ,ts-date) (organon-agenda-day date))
      (let ((task (with-current-buffer (marker-buffer marker)
                    (save-excursion
                      (goto-char marker)
                      (let ((state (org-get-todo-state)))
                        (and state (not (member state org-done-keywords))
                             (organon-task-json)))))))
        (when (and task (not (member (alist-get 'id task) seen)))
          (push (alist-get 'id task) seen)
          (push (list task kind ts-date) result))))
    (nreverse result)))

(defun organon--with-agenda-info (task kind ts-date)
  (append task `((agenda . ((kind . ,kind) (date . ,(organon-json-date ts-date)))))))

(organon-defmethod "agenda.day" (params)
  "Every entry the Org agenda shows for the date: tasks and events."
  (let ((date (organon-param-date params 'date)))
    (vconcat
     (mapcar (pcase-lambda (`(,kind ,marker ,ts-date))
               (with-current-buffer (marker-buffer marker)
                 (save-excursion
                   (goto-char marker)
                   (let ((state (org-get-todo-state)))
                     `((kind . ,kind)
                       (date . ,(organon-json-date ts-date))
                       (id . ,(or (org-entry-get nil "ID") :null))
                       (title . ,(org-get-heading t t t t))
                       (task . ,(if state (organon-task-json) :null)))))))
             (organon-agenda-day date)))))

(organon-defmethod "tasks.today" (params)
  "Open tasks on the agenda for the date."
  (vconcat (mapcar (lambda (entry) (apply #'organon--with-agenda-info entry))
                   (organon--agenda-tasks (organon-param-date params 'date)))))

(organon-defmethod "tasks.overdue" (params)
  "Tasks on the agenda for the date that are past-scheduled or past their deadline."
  (let* ((date (organon-param-date params 'date))
         (day (organon--query-day date)))
    (vconcat
     (cl-loop for (task kind ts-date) in (organon--agenda-tasks date)
              when (or (equal kind "past-scheduled")
                       (and (equal kind "deadline") (< ts-date day)))
              collect (organon--with-agenda-info task kind ts-date)))))

(organon-defmethod "tasks.waiting" (_params)
  "Every WAITING task in the agenda sources, regardless of dates."
  (let ((files (organon-agenda-files)))
    (mapc #'organon-fresh-buffer files)
    (let ((org-agenda-files files))
      (vconcat (and files (org-map-entries #'organon-task-json "TODO=\"WAITING\"" 'agenda))))))

(defun organon--log-line-done-p (marker)
  "Non-nil if the LOGBOOK line at MARKER records a change into DONE."
  (with-current-buffer (marker-buffer marker)
    (save-excursion
      (goto-char marker)
      (beginning-of-line)
      ;; The line format is Org's `org-log-note-headings' entry for `state'.
      (looking-at-p "[ \t]*- State \"DONE\""))))

(organon-defmethod "tasks.completed" (params)
  "Tasks completed (moved to DONE) on the date, including repeating tasks.
Uses the agenda's log mode, i.e. the CLOSED stamps and LOGBOOK lines Org wrote."
  (let ((date (organon-param-date params 'date))
        markers)
    (organon--build-agenda
     date t
     (lambda ()
       (let ((type (get-text-property (point) 'type))
             (heading (get-text-property (point) 'org-hd-marker))
             (log-line (get-text-property (point) 'org-marker)))
         (when (pcase type
                 ("state" (and log-line (organon--log-line-done-p log-line)))
                 ;; A CLOSED stamp is also written for CANCELLED; count it only
                 ;; while the task is DONE.
                 ("closed" (with-current-buffer (marker-buffer heading)
                             (save-excursion (goto-char heading)
                                             (equal (org-get-todo-state) "DONE")))))
           (push (copy-marker heading) markers)))))
    (let (seen result)
      (dolist (marker (nreverse markers))
        (let ((task (organon--task-json-at marker)))
          (unless (member (alist-get 'id task) seen)
            (push (alist-get 'id task) seen)
            (push task result))))
      (vconcat (nreverse result)))))

(provide 'organon-task)

;;; organon-task.el ends here
