;;; organon-agenda-test.el --- Agenda queries  -*- lexical-binding: t; -*-

;; Scenarios: openspec/specs/agenda-queries (and the
;; agenda parts of task-lifecycle / task-recurrence).

;;; Code:

(require 'organon-test-helpers)
(require 'organon-task)

(defconst organon-test-agenda-clock "2026-10-02 05:29:00") ; 14:29 KST

(defun organon-test-agenda-kinds (date)
  "Alist of title -> kind for agenda.day on DATE."
  (mapcar (lambda (e) (cons (alist-get 'title e) (alist-get 'kind e)))
          (organon-test-result "agenda.day" `((date . ,date)))))

(defun organon-test-titles (method &optional params)
  (mapcar (lambda (task) (alist-get 'title task)) (organon-test-result method params)))

;;;; 5.1

(ert-deftest organon-agenda/kinds-on-a-fixed-day ()
  "agenda-queries: Kinds on a fixed day."
  (organon-test-with-instance "agenda" organon-test-agenda-clock
    (let ((kinds (organon-test-agenda-kinds "2026-10-02")))
      (should (equal (cdr (assoc "Scheduled today at 15:00" kinds)) "scheduled"))
      (should (equal (cdr (assoc "Scheduled in the past" kinds)) "past-scheduled"))
      (should (equal (cdr (assoc "Overdue deadline" kinds)) "deadline"))
      (should (equal (cdr (assoc "Deadline in five days" kinds)) "upcoming-deadline"))
      (should (equal (cdr (assoc "Dinner with family" kinds)) "event")))))

(ert-deftest organon-agenda/date-defaults-to-calendar-today ()
  (organon-test-with-instance "agenda" organon-test-agenda-clock
    (should (equal (organon-test-agenda-kinds "2026-10-02")
                   (mapcar (lambda (e) (cons (alist-get 'title e) (alist-get 'kind e)))
                           (organon-test-result "agenda.day"))))))

(ert-deftest organon-agenda/explicit-date-and-own-warning ()
  "agenda-queries: Explicit date."
  (organon-test-with-instance "agenda" organon-test-agenda-clock
    (should (equal (cdr (assoc "Spotify 가족 요금제 납부" (organon-test-agenda-kinds "2026-10-22")))
                   "upcoming-deadline"))
    (should-not (assoc "Spotify 가족 요금제 납부" (organon-test-agenda-kinds "2026-10-21")))))

(ert-deftest organon-agenda/default-warning-window ()
  "agenda-queries: Default window."
  (organon-test-with-instance "agenda" organon-test-agenda-clock
    (let ((kinds (organon-test-agenda-kinds "2026-10-02")))
      (should (assoc "Deadline in five days" kinds))
      (should-not (assoc "Deadline in ten days" kinds)))))

(ert-deftest organon-agenda/nested-project-file ()
  "agenda-queries: Nested project file."
  (organon-test-with-instance "agenda" organon-test-agenda-clock
    (should (equal (cdr (assoc "Order tiles" (organon-test-agenda-kinds "2026-10-02"))) "scheduled"))
    (let ((task (seq-find (lambda (task) (equal (alist-get 'title task) "Order tiles"))
                          (organon-test-result "tasks.today" '((date . "2026-10-02"))))))
      (should (equal (alist-get 'title (alist-get 'project task)) "Renovation")))))

(ert-deftest organon-agenda/excluded-directories ()
  "agenda-queries: Excluded directories."
  (organon-test-with-instance "agenda" organon-test-agenda-clock
    (let ((titles (mapcar #'car (organon-test-agenda-kinds "2026-10-02"))))
      (dolist (title '("Wrote about [2026-10-02 Fri]" "Wrote about <2026-10-02 Fri>" "Journal todo"
                       "Note mentioning <2026-10-02 Fri>" "Archived task"))
        (should-not (member title titles))))))

(ert-deftest organon-agenda/invalid-date ()
  "agenda-queries: Invalid date (engine side)."
  (organon-test-with-instance "agenda" organon-test-agenda-clock
    (dolist (date '("2026-02-30" "../../etc" "2026-1-2"))
      (should (equal (organon-test-error-code "agenda.day" `((date . ,date))) "invalid")))))

;;;; 5.2

(ert-deftest organon-agenda/today-excludes-done-and-events ()
  "agenda-queries: Done and event entries excluded."
  (organon-test-with-instance "agenda" organon-test-agenda-clock
    (let ((titles (organon-test-titles "tasks.today" '((date . "2026-10-02")))))
      (should (member "Scheduled today at 15:00" titles))
      (should-not (member "Done today" titles))
      (should-not (member "Dinner with family" titles))
      (should (equal titles (seq-uniq titles))))
    (let ((first (aref (organon-test-result "tasks.today" '((date . "2026-10-02"))) 0)))
      (should (alist-get 'agenda first))
      (should (alist-get 'version first)))))

(ert-deftest organon-agenda/overdue-subset ()
  "agenda-queries: Overdue subset."
  (organon-test-with-instance "agenda" organon-test-agenda-clock
    (let ((titles (organon-test-titles "tasks.overdue" '((date . "2026-10-02")))))
      (should (member "Scheduled in the past" titles))
      (should (member "Overdue deadline" titles))
      (should-not (member "Scheduled today at 15:00" titles))
      (should-not (member "Deadline in five days" titles)))))

(ert-deftest organon-agenda/waiting-includes-undated ()
  "agenda-queries: Undated waiting task."
  (organon-test-with-instance "agenda" organon-test-agenda-clock
    (should (equal (sort (organon-test-titles "tasks.waiting") #'string<)
                   '("Reply from ISP" "Waiting with a date")))))

;;;; 5.3

(ert-deftest organon-agenda/completed-on-date ()
  "agenda-queries: Mixed completions; task-recurrence: Repeating completion appears in history."
  (organon-test-with-instance "tasks" "2026-10-02 05:29:00"
    (organon-test-transition 1 "complete" "NEXT")
    (organon-test-transition 2 "complete" "NEXT")
    ;; Cancelled tasks are not completions.
    (organon-test-transition 11 "cancel" "TODO")
    (should (equal (sort (organon-test-titles "tasks.completed" '((date . "2026-10-02"))) #'string<)
                   '("Monthly cumulative" "Plain task")))
    (should (equal (organon-test-result "tasks.completed" '((date . "2026-10-01"))) []))))

(ert-deftest organon-agenda/text-that-looks-like-a-completion ()
  "agenda-queries: Text that looks like a completion."
  (organon-test-with-instance "basic" "2026-10-02 05:29:00"
    (organon-test-result "task.create"
                         '((title . "Forged log line")
                           (body . "- State \"DONE\"       from \"NEXT\"       [2026-10-02 Fri 09:00]")))
    (let ((done (organon-test-result "task.create"
                                     '((title . "Really done CLOSED: [2026-09-01 Tue 09:00]") (state . "NEXT")))))
      (organon-test-result "task.transition" `((id . ,(alist-get 'id done)) (action . "complete")
                                               (expected_state . "NEXT"))))
    (should (equal (organon-test-titles "tasks.completed" '((date . "2026-10-02")))
                   '("Really done CLOSED: [2026-09-01 Tue 09:00]")))
    (should (equal (organon-test-result "tasks.completed" '((date . "2026-09-01"))) []))))

;;;; Agenda parts of other capabilities

(ert-deftest organon-agenda/cancelled-series-leaves-agenda ()
  "task-recurrence: Cancel a subscription (no longer in any agenda query)."
  (organon-test-with-instance "tasks" organon-test-agenda-clock
    (organon-test-transition 8 "cancel" "NEXT")
    (dolist (date '("2026-10-25" "2026-11-25"))
      (should-not (member "Streaming subscription" (organon-test-titles "tasks.today" `((date . ,date)))))
      (should-not (assoc "Streaming subscription" (organon-test-agenda-kinds date))))
    (should-not (member "Streaming subscription" (organon-test-titles "tasks.overdue" '((date . "2026-11-30")))))))

;;; organon-agenda-test.el ends here
