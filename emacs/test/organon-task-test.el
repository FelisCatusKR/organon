;;; organon-task-test.el --- Task create/get/transition and recurrence  -*- lexical-binding: t; -*-

;; Scenario names in docstrings refer to
;; openspec/specs/{task-lifecycle,task-recurrence,time-model}.
;; Clocks are UTC: "2026-10-02 05:29:00" is 14:29 in Asia/Seoul.

;;; Code:

(require 'organon-test-helpers)
(require 'organon-task)

(defconst organon-test-clock "2026-10-02 05:29:00")

(defun organon-test-id (n)
  (format "11111111-1111-4111-8111-%012d" n))

(defconst organon-test-project-id "22222222-2222-4222-8222-000000000001")

(defun organon-test-transition (n action expected)
  "Run ACTION on fixture task N, passing its current version like a client would."
  (let ((version (alist-get 'version (organon-test-result "task.get" `((id . ,(organon-test-id n)))))))
    (organon-test-result "task.transition" `((id . ,(organon-test-id n)) (action . ,action)
                                             (expected_state . ,expected)
                                             (expected_version . ,version)))))

(defun organon-test-heading-count (relative)
  (with-temp-buffer
    (insert-file-contents (organon-test-file relative))
    (how-many "^\\*+ " (point-min) (point-max))))

;;;; 4.1 Create

(ert-deftest organon-task/create-persists-org-heading ()
  "task-lifecycle: Task is persisted as an Org heading."
  (organon-test-with-instance "basic" organon-test-clock
    (let ((task (organon-test-result "task.create" '((title . "Spotify 가족 요금제 납부")))))
      (should (equal (alist-get 'state task) "TODO"))
      (should (equal (alist-get 'id task) "00000000-0000-4000-8000-000000000001"))
      (should (equal (alist-get 'location_hint task) "tasks/inbox.org"))
      (organon-test-check-golden "task/create-basic.org" "org/tasks/inbox.org"))))

(ert-deftest organon-task/create-with-deadline-warning-and-tags ()
  "task-lifecycle: Task with deadline, warning and tags."
  (organon-test-with-instance "basic" organon-test-clock
    (let ((task (organon-test-result
                 "task.create"
                 '((title . "Spotify 가족 요금제 납부") (state . "NEXT") (priority . "A")
                   (tags . ["bills" "home"])
                   (deadline . ((date . "2026-10-25") (repeat . "+1m") (warning_days . 3)))))))
      (should (equal (alist-get 'state task) "NEXT"))
      (should (equal (alist-get 'priority task) "A"))
      (should (equal (alist-get 'tags task) ["bills" "home"]))
      (should (equal (alist-get 'deadline task)
                     '((date . "2026-10-25") (time) (repeat . "+1m") (warning_days . 3))))
      (should (string-match-p "^DEADLINE: <2026-10-25 Sun \\+1m -3d>$"
                              (organon-test-file-string "org/tasks/inbox.org")))
      (organon-test-check-golden "task/create-full.org" "org/tasks/inbox.org"))))

(ert-deftest organon-task/create-under-project ()
  "task-lifecycle: Task created under a project."
  (organon-test-with-instance "tasks" organon-test-clock
    (let ((task (organon-test-result "task.create" `((title . "Fix the sink")
                                                     (project_id . ,organon-test-project-id)))))
      (should (equal (alist-get 'project task)
                     `((id . ,organon-test-project-id) (title . "Household"))))
      (should (equal (alist-get 'location_hint task) "projects/household.org"))
      (organon-test-check-golden "task/create-in-project.org" "org/projects/household.org"))))

(ert-deftest organon-task/create-with-unknown-project ()
  "task-lifecycle: Unknown project."
  (organon-test-with-instance "tasks" organon-test-clock
    (let ((before-inbox (organon-test-file-string "org/tasks/inbox.org"))
          (before-project (organon-test-file-string "org/projects/household.org")))
      ;; A random id, and an id that exists but is not a level-1 project heading.
      (dolist (id (list "99999999-9999-4999-8999-999999999999" (organon-test-id 1)
                        "22222222-2222-4222-8222-000000000002"))
        (should (equal (organon-test-error-code "task.create" `((title . "x") (project_id . ,id)))
                       "invalid")))
      (should (equal (organon-test-file-string "org/tasks/inbox.org") before-inbox))
      (should (equal (organon-test-file-string "org/projects/household.org") before-project)))))

(ert-deftest organon-task/create-validates-parameters ()
  (organon-test-with-instance "basic" organon-test-clock
    (dolist (params '(((title . ""))
                      ((title . "x") (state . "DONE"))
                      ((title . "x") (priority . "D"))
                      ((title . "x") (tags . ["bad tag"]))
                      ((title . "x") (deadline . ((date . "2026-02-30"))))
                      ((title . "x") (deadline . ((date . "2026-10-25") (repeat . "+1x"))))
                      ((title . "x") (deadline . ((date . "2026-10-25") (time . "25:00"))))
                      ((title . "x") (scheduled . ((date . "2026-10-25") (warning_days . 3))))
                      ((title . "x") (repeat_to_state . "DOING"))))
      (should (equal (organon-test-error-code "task.create" params) "invalid")))
    (should (equal (organon-test-file-string "org/tasks/inbox.org") "#+title: Inbox\n"))))

;;;; 4.2 Text handling

(ert-deftest organon-task/title-with-newline-is-invalid ()
  "task-lifecycle: Title with a newline."
  (organon-test-with-instance "basic" organon-test-clock
    (should (equal (organon-test-error-code "task.create" '((title . "two\nlines"))) "invalid"))))

(ert-deftest organon-task/structural-titles-are-invalid ()
  "task-lifecycle: Title that Org would parse as structure."
  (organon-test-with-instance "basic" organon-test-clock
    (dolist (title '("[#A] pay rent" "COMMENT pay rent" "pay rent :bills:" ":bills:"))
      (should (equal (organon-test-error-code "task.create" `((title . ,title))) "invalid")))
    (should (equal (organon-test-file-string "org/tasks/inbox.org") "#+title: Inbox\n"))))

(ert-deftest organon-task/body-that-looks-like-structure ()
  "task-lifecycle: Body that looks like a heading."
  (organon-test-with-instance "basic" organon-test-clock
    (let* ((body "first line\n* NEXT injected\n#+begin_src sh\n:PROPERTIES:\n,* already comma\n  * indented")
           (task (organon-test-result "task.create" `((title . "With body") (body . ,body)))))
      (should (= (organon-test-heading-count "org/tasks/inbox.org") 1))
      (should (equal (alist-get 'body task) body))
      (should (equal (alist-get 'body (organon-test-result "task.get" `((id . ,(alist-get 'id task)))))
                     body))
      (organon-test-check-golden "task/body-escaped.org" "org/tasks/inbox.org"))))

(ert-deftest organon-task/active-timestamps-in-text-are-deactivated ()
  "task-lifecycle: Active timestamps in title and body."
  (organon-test-with-instance "basic" organon-test-clock
    (let* ((marker (make-temp-name "/tmp/organon-sexp-"))
           (task (organon-test-result
                  "task.create"
                  `((title . "Meet <2026-10-02 Fri>")
                    (body . ,(format "at <2026-10-02 Fri 19:00>\n<%%%%(diary-float t 4 2)>\n%%%%(progn (write-region \"x\" nil %S) t)\n&%%%%(progn (write-region \"x\" nil %S) t)"
                                     marker marker))))))
      (should (equal (alist-get 'title task) "Meet [2026-10-02 Fri]"))
      (should (equal (alist-get 'body task)
                     (format "at [2026-10-02 Fri 19:00]\n[%%%%(diary-float t 4 2)>\n%%%%(progn (write-region \"x\" nil %S) t)\n&%%%%(progn (write-region \"x\" nil %S) t)"
                             marker marker)))
      ;; Nothing from the text shows up on the agenda, and the sexp never ran.
      (should (equal (organon-test-result "agenda.day" '((date . "2026-10-02"))) []))
      (should-not (file-exists-p marker)))))

(ert-deftest organon-task/lisp-in-text-stays-text ()
  "api-access: Lisp in text fields stays text."
  (organon-test-with-instance "basic" organon-test-clock
    (let* ((marker (make-temp-name "/tmp/organon-pwned-"))
           (title (format "(write-region \"x\" nil %S)" marker))
           (body (format "%%(write-region \"x\" nil %S)\n%%(shell-command \"id\")" marker))
           (task (organon-test-result "task.create" `((title . ,title) (body . ,body)))))
      (should (equal (alist-get 'title task) title))
      (should (equal (alist-get 'body task) body))
      (should-not (file-exists-p marker)))))

;;;; 4.3 Get and serialization

(ert-deftest organon-task/get-after-heading-moved-files ()
  "task-lifecycle: Read after the heading moved files."
  (organon-test-with-instance "tasks" organon-test-clock
    (organon-test-result "task.get" `((id . ,(organon-test-id 1))))
    ;; Move the heading to another file while the "engine is stopped".
    (organon-test--kill-buffers-under organon-test-instance)
    (let* ((inbox (organon-test-file "org/tasks/inbox.org"))
           (text (organon-test-file-string "org/tasks/inbox.org"))
           (start (string-match "^\\* NEXT Plain task" text))
           (end (string-match "^\\* " text (1+ start)))
           (file-precious-flag nil))
      (with-temp-file (organon-test-file "org/tasks/personal.org")
        (insert "#+title: Personal\n\n" (substring text start end)))
      (with-temp-file inbox
        (insert (substring text 0 start) (substring text end))))
    (let ((task (organon-test-result "task.get" `((id . ,(organon-test-id 1))))))
      (should (equal (alist-get 'title task) "Plain task"))
      (should (equal (alist-get 'location_hint task) "tasks/personal.org")))))

(ert-deftest organon-task/get-unknown-id ()
  "task-lifecycle: Unknown ID."
  (organon-test-with-instance "tasks" organon-test-clock
    (should (equal (organon-test-error-code "task.get" '((id . "99999999-9999-4999-8999-999999999999")))
                   "not_found"))
    (should (equal (organon-test-error-code "task.get" '((id . "../../etc/passwd"))) "invalid"))
    ;; The project heading has an ID but is not a task.
    (should (equal (organon-test-error-code "task.get" `((id . ,organon-test-project-id))) "not_found"))))

(ert-deftest organon-task/unknown-ids-rescan-only-after-changes ()
  "Repeated lookups of unknown IDs do not re-read the org directory each time."
  (organon-test-with-instance "tasks" organon-test-clock
    (let ((scans 0)
          (unknown '((id . "99999999-9999-4999-8999-999999999999"))))
      (cl-letf* ((update (symbol-function 'org-id-update-id-locations))
                 ((symbol-function 'org-id-update-id-locations)
                  (lambda (&rest args) (cl-incf scans) (apply update args))))
        (dotimes (_ 3)
          (should (equal (organon-test-error-code "task.get" unknown) "not_found")))
        (should (= scans 1))
        ;; A change on disk allows one more scan.
        (organon-test-result "task.create" '((title . "New")))
        (dotimes (_ 2)
          (should (equal (organon-test-error-code "task.get" unknown) "not_found")))
        (should (= scans 2))))))

(ert-deftest organon-task/closed-at-is-utc ()
  "time-model: closed_at in UTC (08:30 KST on 2026-10-03 = 23:30Z on 2026-10-02)."
  (organon-test-with-instance "tasks" "2026-10-02 23:30:00"
    (let ((task (alist-get 'task (organon-test-transition 1 "complete" "NEXT"))))
      (should (equal (alist-get 'closed_at task) "2026-10-02T23:30:00Z"))
      (should (string-match-p "CLOSED: \\[2026-10-03 Sat 08:30\\]"
                              (organon-test-file-string "org/tasks/inbox.org"))))))

;;;; 4.4 Transitions

(ert-deftest organon-task/complete-non-repeating ()
  "task-lifecycle: Complete a non-repeating task."
  (organon-test-with-instance "tasks" organon-test-clock
    (let ((result (organon-test-transition 1 "complete" "NEXT")))
      (should (equal (alist-get 'state (alist-get 'task result)) "DONE"))
      (should (equal (alist-get 'closed_at (alist-get 'task result)) "2026-10-02T05:29:00Z"))
      (should (equal (alist-get 'warnings result) []))
      (let ((text (organon-test-file-string "org/tasks/inbox.org")))
        (should (string-match-p "CLOSED: \\[2026-10-02 Fri 14:29\\]" text))
        (should (string-match-p "- State \"DONE\" +from \"NEXT\" +\\[2026-10-02 Fri 14:29\\]" text))))))

(ert-deftest organon-task/start-and-wait-are-logged ()
  "task-lifecycle: Start and wait are logged."
  (organon-test-with-instance "tasks" organon-test-clock
    (organon-test-transition 11 "start" "TODO")
    (organon-test-set-clock "2026-10-02 06:00:00")
    (organon-test-transition 11 "wait" "DOING")
    (organon-test-check-golden "task/start-then-wait.org" "org/tasks/inbox.org")))

(ert-deftest organon-task/retried-completion-non-repeating ()
  "task-lifecycle: Retried completion of a non-repeating task."
  (organon-test-with-instance "tasks" organon-test-clock
    (organon-test-transition 1 "complete" "NEXT")
    (let ((after (organon-test-file-string "org/tasks/inbox.org"))
          (err (alist-get 'error (organon-test-call "task.transition" `((id . ,(organon-test-id 1)) (action . "complete")
                                                                         (expected_state . "NEXT"))))))
      (should (equal (alist-get 'code err) "conflict"))
      (should (equal (alist-get 'actual_state (alist-get 'data err)) "DONE"))
      (should (equal (organon-test-file-string "org/tasks/inbox.org") after)))))

(ert-deftest organon-task/retried-completion-repeating ()
  "task-lifecycle: Retried completion of a repeating task."
  (organon-test-with-instance "tasks" organon-test-clock
    (let* ((version (alist-get 'version (organon-test-result "task.get" `((id . ,(organon-test-id 2))))))
           (request `((id . ,(organon-test-id 2)) (action . "complete")
                      (expected_state . "NEXT") (expected_version . ,version))))
      (organon-test-result "task.transition" request)
      (let ((after (organon-test-file-string "org/tasks/inbox.org")))
        (should (equal (organon-test-error-code "task.transition" request) "conflict"))
        (should (equal (organon-test-file-string "org/tasks/inbox.org") after))
        (should (string-match-p "<2026-09-25 Fri \\+1m>" after))))))

(ert-deftest organon-task/repeating-requires-expected-version ()
  "task-lifecycle: Repeating task without expected_version."
  (organon-test-with-instance "tasks" organon-test-clock
    (let ((before (organon-test-file-string "org/tasks/inbox.org")))
      (should (equal (organon-test-error-code "task.transition" `((id . ,(organon-test-id 2)) (action . "complete")
                                                                  (expected_state . "NEXT")))
                     "invalid"))
      (should (equal (organon-test-file-string "org/tasks/inbox.org") before)))))

(ert-deftest organon-task/missing-expected-state ()
  "task-lifecycle: Missing expected_state (engine side)."
  (organon-test-with-instance "tasks" organon-test-clock
    (should (equal (organon-test-error-code "task.transition" `((id . ,(organon-test-id 1)) (action . "complete")))
                   "invalid"))))

(ert-deftest organon-task/doing-limit-warning ()
  "task-lifecycle: Fourth DOING task (fixture has one DOING; limit is 3)."
  (organon-test-with-instance "tasks" organon-test-clock
    (should (equal (alist-get 'warnings (organon-test-transition 1 "start" "NEXT")) []))
    (should (equal (alist-get 'warnings (organon-test-transition 11 "start" "TODO")) []))
    (let ((result (organon-test-transition 12 "start" "TODO")))
      (should (equal (alist-get 'state (alist-get 'task result)) "DOING"))
      (should (equal (alist-get 'warnings result) ["doing_limit_exceeded"])))))

;;;; 4.5 Recurrence

(defun organon-test-deadline-after-complete (n expected-state)
  (alist-get 'deadline (alist-get 'task (organon-test-transition n "complete" expected-state))))

(ert-deftest organon-task/repeater-cumulative ()
  "task-recurrence: Cumulative repeater (+)."
  (organon-test-with-instance "tasks" organon-test-clock
    (let ((task (alist-get 'task (organon-test-transition 2 "complete" "NEXT"))))
      (should (equal (alist-get 'id task) (organon-test-id 2)))
      (should (equal (alist-get 'state task) "NEXT"))
      (should (equal (alist-get 'date (alist-get 'deadline task)) "2026-09-25")))))

(ert-deftest organon-task/repeater-catch-up ()
  "task-recurrence: Catch-up repeater (++)."
  (organon-test-with-instance "tasks" organon-test-clock
    (should (equal (alist-get 'date (organon-test-deadline-after-complete 3 "NEXT")) "2026-10-25"))))

(ert-deftest organon-task/repeater-restart ()
  "task-recurrence: Restart repeater (.+)."
  (organon-test-with-instance "tasks" organon-test-clock
    (should (equal (alist-get 'date (organon-test-deadline-after-complete 4 "NEXT")) "2026-11-02"))))

(ert-deftest organon-task/repeater-keeps-warning ()
  "task-recurrence: Warning period is preserved."
  (organon-test-with-instance "tasks" organon-test-clock
    (should (equal (organon-test-deadline-after-complete 5 "NEXT")
                   '((date . "2026-11-25") (time) (repeat . "+1m") (warning_days . 3))))
    (should (string-match-p "DEADLINE: <2026-11-25 Wed \\+1m -3d>"
                            (organon-test-file-string "org/tasks/inbox.org")))))

(ert-deftest organon-task/repeat-default-return-state ()
  "task-recurrence: Default return state (DOING -> NEXT)."
  (organon-test-with-instance "tasks" organon-test-clock
    (should (equal (alist-get 'state (alist-get 'task (organon-test-transition 6 "complete" "DOING")))
                   "NEXT"))))

(ert-deftest organon-task/repeat-per-task-return-state ()
  "task-recurrence: Per-task return state."
  (organon-test-with-instance "tasks" organon-test-clock
    (should (equal (alist-get 'state (alist-get 'task (organon-test-transition 7 "complete" "NEXT")))
                   "TODO"))
    (let ((created (organon-test-result "task.create" '((title . "Created with TODO return")
                                                        (deadline . ((date . "2026-10-25") (repeat . "+1w")))
                                                        (repeat_to_state . "TODO")))))
      (should (equal (alist-get 'repeat_to_state created) "TODO")))))

(ert-deftest organon-task/repeat-invalid-return-state ()
  "task-recurrence: Invalid return state."
  (organon-test-with-instance "tasks" organon-test-clock
    (should (equal (organon-test-error-code "task.create" '((title . "x") (repeat_to_state . "DOING")))
                   "invalid"))))

(ert-deftest organon-task/repeat-logs-completion ()
  "task-recurrence: Repeating completion appears in history (LOGBOOK part)."
  (organon-test-with-instance "tasks" organon-test-clock
    (organon-test-transition 2 "complete" "NEXT")
    (organon-test-check-golden "task/repeat-complete.org" "org/tasks/inbox.org")))

(ert-deftest organon-task/restart-repeater-uses-calendar-date ()
  "time-model: Restart repeater uses the calendar date (23:30Z = 08:30 KST next day)."
  (organon-test-with-instance "tasks" "2026-10-02 23:30:00"
    (should (equal (alist-get 'date (organon-test-deadline-after-complete 10 "NEXT")) "2026-11-03"))))

;;;; 4.6 Skip and cancel

(ert-deftest organon-task/skip-one-occurrence ()
  "task-recurrence: Skip a monthly payment."
  (organon-test-with-instance "tasks" organon-test-clock
    (let ((task (alist-get 'task (organon-test-transition 9 "skip" "NEXT"))))
      (should (equal (alist-get 'state task) "NEXT"))
      (should (equal (alist-get 'date (alist-get 'deadline task)) "2026-11-25"))
      (should (string-match-p "- State \"CANCELLED\" +from \"NEXT\""
                              (organon-test-file-string "org/tasks/inbox.org"))))))

(ert-deftest organon-task/skip-non-repeating-is-cancel ()
  (organon-test-with-instance "tasks" organon-test-clock
    (should (equal (alist-get 'state (alist-get 'task (organon-test-transition 1 "skip" "NEXT")))
                   "CANCELLED"))))

(ert-deftest organon-task/cancel-series ()
  "task-recurrence: Cancel a subscription."
  (organon-test-with-instance "tasks" organon-test-clock
    (let ((task (alist-get 'task (organon-test-transition 8 "cancel" "NEXT"))))
      (should (equal (alist-get 'state task) "CANCELLED"))
      (should (equal (alist-get 'deadline task)
                     '((date . "2026-10-25") (time) (repeat) (warning_days)))))
    (organon-test-check-golden "task/cancel-series.org" "org/tasks/inbox.org")))

;;; organon-task-test.el ends here
