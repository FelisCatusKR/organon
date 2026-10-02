;;; organon-essentials-test.el --- Editing, reopening, listing, projects  -*- lexical-binding: t; -*-

;; Scenarios: openspec/changes/task-essentials/specs/{task-lifecycle,
;; task-listing,projects}.  Fixture: tests/fixtures/essentials.
;; Clocks are UTC: "2026-10-02 05:29:00" is 14:29 in Asia/Seoul.

;;; Code:

(require 'organon-test-helpers)
(require 'organon-task)

(defconst organon-test-essentials-clock "2026-10-02 05:29:00")

(defun organon-test-eid (n)
  (format "44444444-4444-4444-8444-%012d" n))

(defun organon-test-version (n)
  (alist-get 'version (organon-test-result "task.get" `((id . ,(organon-test-eid n))))))

(defun organon-test-update (n set &optional clear)
  "Update fixture task N with SET (alist) and CLEAR (list of names)."
  (organon-test-call "task.update" `((id . ,(organon-test-eid n))
                                     (expected_version . ,(organon-test-version n))
                                     (set . ,(or set :null))
                                     (clear . ,(vconcat clear)))))

(defun organon-test-update-result (n set &optional clear)
  (let ((response (organon-test-update n set clear)))
    (unless (alist-get 'ok response)
      (ert-fail (list "update failed" (alist-get 'error response))))
    (alist-get 'result response)))

(defun organon-test-ids (tasks)
  (mapcar (lambda (task) (alist-get 'id task)) tasks))

(defmacro organon-test-with-essentials (&rest body)
  (declare (indent 0))
  `(organon-test-with-instance "essentials" organon-test-essentials-clock ,@body))

;;;; 1.1 Edit a task

(ert-deftest organon-essentials/postpone-a-deadline ()
  "task-lifecycle: Postpone a deadline."
  (organon-test-with-essentials
    (let ((task (organon-test-update-result 2 '((deadline . ((date . "2026-10-30")))))))
      (should (equal (alist-get 'date (alist-get 'deadline task)) "2026-10-30"))
      (should (equal (alist-get 'title task) "Pay the bill"))
      (should (equal (alist-get 'state task) "NEXT"))
      (should (equal (alist-get 'tags task) ["bills"]))
      (should (string-match-p "^DEADLINE: <2026-10-30 Fri>$"
                              (organon-test-file-string "org/tasks/inbox.org"))))))

(ert-deftest organon-essentials/clear-schedule-and-priority ()
  "task-lifecycle: Clear a schedule and a priority."
  (organon-test-with-essentials
    (let ((task (organon-test-update-result 6 nil '("scheduled" "priority")))
          (text (organon-test-file-string "org/tasks/inbox.org")))
      (should (eq (alist-get 'scheduled task) nil))
      (should (eq (alist-get 'priority task) nil))
      (should (string-match-p "^\\* NEXT Scheduled with priority$" text))
      (should-not (string-match-p "2026-10-05" text))
      (should (equal (alist-get 'body task) "old body")))))

(ert-deftest organon-essentials/rename-keeps-state-tags-and-id ()
  "task-lifecycle: Rename keeps state, tags and ID."
  (organon-test-with-essentials
    (let ((task (organon-test-update-result 2 '((title . "Pay the electricity bill")))))
      (should (equal (alist-get 'id task) (organon-test-eid 2)))
      (should (equal (alist-get 'state task) "NEXT"))
      (should (equal (alist-get 'tags task) ["bills"]))
      (should (string-match-p "^\\* NEXT Pay the electricity bill :bills:$"
                              (organon-test-file-string "org/tasks/inbox.org"))))))

(ert-deftest organon-essentials/replace-tags-and-body ()
  "task-lifecycle: Replace tags and body."
  (organon-test-with-essentials
    (let* ((body "new body\n* looks like a heading")
           (task (organon-test-update-result 6 `((tags . ["home"]) (body . ,body)))))
      (should (equal (alist-get 'tags task) ["home"]))
      (should (equal (alist-get 'body task) body))
      (organon-test-check-golden "essentials/replace-tags-and-body.org" "org/tasks/inbox.org"))))

(ert-deftest organon-essentials/new-deadline-drops-old-repeater ()
  "A deadline sent without a repeater replaces one that had a repeater (D1)."
  (organon-test-with-essentials
    (let ((task (organon-test-update-result 7 '((deadline . ((date . "2026-11-01")))))))
      (should (equal (alist-get 'deadline task)
                     '((date . "2026-11-01") (time) (repeat) (warning_days)))))))

(ert-deftest organon-essentials/stale-version ()
  "task-lifecycle: Stale version."
  (organon-test-with-essentials
    (let* ((before (organon-test-file-string "org/tasks/inbox.org"))
           (response (organon-test-call "task.update" `((id . ,(organon-test-eid 2))
                                                        (expected_version . "0000000000000000")
                                                        (set . ((title . "x"))))))
           (err (alist-get 'error response)))
      (should (equal (alist-get 'code err) "conflict"))
      (should (equal (alist-get 'actual_version (alist-get 'data err)) (organon-test-version 2)))
      (should (equal (organon-test-file-string "org/tasks/inbox.org") before)))))

(ert-deftest organon-essentials/invalid-edit ()
  "task-lifecycle: Invalid edit (engine side)."
  (organon-test-with-essentials
    (let ((before (organon-test-file-string "org/tasks/inbox.org"))
          (version (organon-test-version 2)))
      (dolist (params `(((set . ((title . "pay :bills:"))))
                        ((set . ((file . "/etc/passwd"))))
                        ((set . ((title . "x"))) (clear . ["title"]))
                        ((set . ((priority . "A"))) (clear . ["priority"]))
                        ((set . ((deadline . ((date . "2026-02-30"))))))
                        ()))
        (should (equal (organon-test-error-code
                        "task.update" (append `((id . ,(organon-test-eid 2)) (expected_version . ,version))
                                              params))
                       "invalid")))
      ;; Missing expected_version.
      (should (equal (organon-test-error-code "task.update" `((id . ,(organon-test-eid 2))
                                                              (set . ((title . "x")))))
                     "invalid"))
      (should (equal (organon-test-file-string "org/tasks/inbox.org") before)))))

;;;; 1.2 Transitions back to TODO and NEXT

(defun organon-test-move (n action)
  (let ((task (organon-test-result "task.get" `((id . ,(organon-test-eid n))))))
    (alist-get 'task (organon-test-result "task.transition"
                                          `((id . ,(organon-test-eid n)) (action . ,action)
                                            (expected_state . ,(alist-get 'state task))
                                            (expected_version . ,(alist-get 'version task)))))))

(ert-deftest organon-essentials/waiting-back-to-next ()
  "task-lifecycle: Waiting task back to NEXT."
  (organon-test-with-essentials
    (should (equal (alist-get 'state (organon-test-move 3 "next")) "NEXT"))))

(ert-deftest organon-essentials/reopen-a-completed-task ()
  "task-lifecycle: Reopen a completed task."
  (organon-test-with-essentials
    (let ((task (organon-test-move 4 "todo")))
      (should (equal (alist-get 'state task) "TODO"))
      (should (eq (alist-get 'closed_at task) nil)))
    (organon-test-check-golden "essentials/reopen.org" "org/tasks/inbox.org")))

(ert-deftest organon-essentials/repeating-next-needs-version ()
  (organon-test-with-essentials
    (should (equal (organon-test-error-code "task.transition" `((id . ,(organon-test-eid 7)) (action . "todo")
                                                                (expected_state . "NEXT")))
                   "invalid"))))

;;;; 1.3 Listing

(ert-deftest organon-essentials/list-defaults-to-open-tasks ()
  "task-listing: Undated backlog is listed; Closed tasks are not listed by default."
  (organon-test-with-essentials
    (let ((ids (organon-test-ids (organon-test-result "tasks.list"))))
      (should (member (organon-test-eid 1) ids))
      (should (member (organon-test-eid 10) ids))
      (should-not (member (organon-test-eid 4) ids))
      (should-not (member (organon-test-eid 5) ids))
      (should-not (member (organon-test-eid 12) ids))
      ;; File order: tasks/ before projects/, headings in order.
      (should (< (cl-position (organon-test-eid 1) ids :test #'equal)
                 (cl-position (organon-test-eid 2) ids :test #'equal)
                 (cl-position (organon-test-eid 10) ids :test #'equal))))))

(ert-deftest organon-essentials/list-next-of-one-project ()
  "task-listing: Next actions of one project."
  (organon-test-with-essentials
    (should (equal (organon-test-ids (organon-test-result "tasks.list" `((states . ["NEXT"])
                                                                         (project . ,(organon-test-eid 100)))))
                   (list (organon-test-eid 10))))))

(ert-deftest organon-essentials/list-several-states-and-a-tag ()
  "task-listing: Several states and a tag."
  (organon-test-with-essentials
    (should (equal (organon-test-ids (organon-test-result "tasks.list" '((states . ["DONE" "CANCELLED"])
                                                                         (tag . "bills"))))
                   (list (organon-test-eid 4))))))

(ert-deftest organon-essentials/other-keywords-are-not-tasks ()
  "task-listing: Headings with other keywords are not tasks."
  (organon-test-with-essentials
    (let ((ids (organon-test-ids (organon-test-result
                                  "tasks.list" `((states . ,(vconcat organon-task-states)))))))
      (should-not (member (organon-test-eid 20) ids)))
    (let ((ideas (seq-find (lambda (p) (equal (alist-get 'id p) (organon-test-eid 200)))
                           (organon-test-result "projects.list"))))
      (should (equal (alist-get 'open_tasks ideas) 0)))))

(ert-deftest organon-essentials/list-invalid-filters ()
  "task-listing: Invalid filter (engine side)."
  (organon-test-with-essentials
    (dolist (params '(((states . ["SOMEDAY"])) ((states . [])) ((project . "../x")) ((tag . "a b"))))
      (should (equal (organon-test-error-code "tasks.list" params) "invalid")))))

;;;; 1.4 Projects

(ert-deftest organon-essentials/create-a-project ()
  "projects: New project file; Tasks can be added to the new project."
  (organon-test-with-essentials
    (let* ((project (organon-test-result "project.create" '((title . "Home renovation"))))
           (id (alist-get 'id project)))
      (should (equal (alist-get 'location_hint project) "projects/home-renovation.org"))
      (should (equal (alist-get 'open_tasks project) 0))
      (should (string-match-p (concat "^\\* Home renovation\n:PROPERTIES:\n:ID: +" (regexp-quote id))
                              (organon-test-file-string "org/projects/home-renovation.org")))
      (let ((task (organon-test-result "task.create" `((title . "Order tiles") (project_id . ,id)))))
        (should (equal (alist-get 'project task) `((id . ,id) (title . "Home renovation"))))))))

(ert-deftest organon-essentials/same-project-title-twice ()
  "projects: Same title twice."
  (organon-test-with-essentials
    (let ((a (organon-test-result "project.create" '((title . "Home renovation"))))
          (b (organon-test-result "project.create" '((title . "Home renovation")))))
      (should-not (equal (alist-get 'id a) (alist-get 'id b)))
      (should (equal (alist-get 'location_hint b) "projects/home-renovation-2.org")))))

(ert-deftest organon-essentials/hangul-project-file-name ()
  (organon-test-with-essentials
    (should (equal (alist-get 'location_hint (organon-test-result "project.create" '((title . "집 수리 / 2026"))))
                   "projects/집-수리-2026.org"))
    (should (equal (alist-get 'location_hint (organon-test-result "project.create" '((title . "!!!"))))
                   "projects/project.org"))))

(ert-deftest organon-essentials/list-projects-with-open-counts ()
  "projects: Counts of open tasks."
  (organon-test-with-essentials
    (let ((household (seq-find (lambda (p) (equal (alist-get 'id p) (organon-test-eid 100)))
                               (organon-test-result "projects.list"))))
      (should (equal (alist-get 'title household) "Household"))
      (should (equal (alist-get 'open_tasks household) 2)))))

(ert-deftest organon-essentials/invalid-project-title ()
  (organon-test-with-essentials
    (should (equal (organon-test-error-code "project.create" '((title . "COMMENT x"))) "invalid"))
    (should-not (directory-files (organon-test-file "org/projects/") nil "comment"))))

;;; organon-essentials-test.el ends here
