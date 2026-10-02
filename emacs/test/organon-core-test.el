;;; organon-core-test.el --- RPC boundary, wrapper and time model  -*- lexical-binding: t; -*-

;;; Code:

(require 'organon-test-helpers)
(require 'organon)

(defmacro organon-test--with-temp-method (name fn &rest body)
  "Register FN as RPC method NAME for the duration of BODY."
  (declare (indent 2))
  `(unwind-protect
       (progn (puthash ,name ,fn organon-methods) ,@body)
     (remhash ,name organon-methods)))

;;;; 3.1 Dispatch boundary

(ert-deftest organon-core/ping-and-meta ()
  (organon-test-with-instance "basic" "2026-10-02 05:29:00"
    (should (equal (alist-get 'status (organon-test-result "ping")) "ok"))
    (let ((meta (organon-test-result "meta")))
      (should (equal (alist-get 'calendar_tz meta) "Asia/Seoul"))
      (should (equal (alist-get 'today meta) "2026-10-02")))))

(ert-deftest organon-core/unknown-method-is-invalid ()
  (organon-test-with-instance "basic" "2026-10-02 05:29:00"
    (should (equal (organon-test-error-code "nope") "invalid"))))

(ert-deftest organon-core/lisp-as-method-name-is-not-evaluated ()
  (organon-test-with-instance "basic" "2026-10-02 05:29:00"
    (let ((marker (make-temp-name "/tmp/organon-pwned-")))
      (should (equal (organon-test-error-code (format "(write-region \"x\" nil %S)" marker)) "invalid"))
      (should (equal (organon-test-error-code "(kill-emacs)") "invalid"))
      (should-not (file-exists-p marker)))))

(ert-deftest organon-core/malformed-requests ()
  (organon-test-with-instance "basic" "2026-10-02 05:29:00"
    (dolist (line '("not json" "[1,2]" "{\"method\":\"ping\",\"params\":[1]}" "{\"method\":42}"))
      (let ((response (json-parse-string (organon-handle-request line) :object-type 'alist
                                         :false-object nil :null-object nil)))
        (should-not (alist-get 'ok response))
        (should (equal (alist-get 'code (alist-get 'error response)) "invalid"))))))

(ert-deftest organon-core/socket-round-trip ()
  (organon-test-with-instance "basic" "2026-10-02 05:29:00"
    (organon-start-server)
    (let* ((output "")
           (client (make-network-process
                    :name "test-client" :family 'local :service (organon-socket-path)
                    :coding 'utf-8 :filter (lambda (_p s) (setq output (concat output s))))))
      (process-send-string client "{\"id\":\"r-1\",\"method\":\"meta\",\"params\":{}}\n")
      (with-timeout (5 (ert-fail "no response from socket"))
        (while (not (string-suffix-p "\n" output))
          (accept-process-output client 0.05)))
      (delete-process client)
      (let ((response (json-parse-string output :object-type 'alist)))
        (should (equal (alist-get 'id response) "r-1"))
        (should (eq (alist-get 'ok response) t))
        (should (equal (alist-get 'calendar_tz (alist-get 'result response)) "Asia/Seoul"))))))

;;;; 3.2 Prompts, external edits, failed writes

(ert-deftest organon-core/prompts-fail-instead-of-blocking ()
  (organon-test-with-instance "basic" "2026-10-02 05:29:00"
    (organon-test--with-temp-method "test.ask" (lambda (_) (y-or-n-p "Really? "))
      (should (equal (organon-test-error-code "test.ask") "prompt_blocked")))
    (organon-test--with-temp-method "test.read" (lambda (_) (read-string "Name: "))
      (should (equal (organon-test-error-code "test.read") "prompt_blocked")))))

(ert-deftest organon-core/external-edit-of-clean-buffer-is-picked-up ()
  "data-integrity: External edits are picked up (engine level)."
  (organon-test-with-instance "basic" "2026-10-02 05:29:00"
    (let ((file (organon-test-file "org/tasks/inbox.org")))
      (organon-fresh-buffer file)
      ;; Make sure the modification time visibly changes.
      (set-file-times file (time-subtract (current-time) 10))
      (with-current-buffer (get-file-buffer file) (set-visited-file-modtime))
      (let ((file-precious-flag nil))
        (with-temp-file file (insert "#+title: Inbox\n\n* TODO added outside\n")))
      (with-current-buffer (organon-fresh-buffer file)
        (should (string-match-p "added outside" (buffer-string)))))))

;; A file the engine has open may be moved away or deleted by the user.
(ert-deftest organon-core/deleted-file-is-recreated ()
  "data-integrity: External edits are picked up (file removed)."
  (organon-test-with-instance "basic" "2026-10-02 05:29:00"
    (let ((file (organon-test-file "org/tasks/inbox.org")))
      (organon-test-result "task.create" '((title . "Before")))
      (delete-file file)
      (let ((task (organon-test-result "task.create" '((title . "After")))))
        (should (equal (alist-get 'title task) "After")))
      (should (file-exists-p file))
      (should-not (string-match-p "Before" (organon-test-file-string "org/tasks/inbox.org")))
      (should (= (organon-test-heading-count "org/tasks/inbox.org") 1)))))

(ert-deftest organon-core/external-edit-of-modified-buffer-is-conflict ()
  (organon-test-with-instance "basic" "2026-10-02 05:29:00"
    (let ((file (organon-test-file "org/tasks/inbox.org")))
      (with-current-buffer (organon-fresh-buffer file)
        (goto-char (point-max))
        (insert "* TODO unsaved\n"))
      (set-file-times file (time-subtract (current-time) 10))
      (with-current-buffer (get-file-buffer file) (set-visited-file-modtime))
      (let ((file-precious-flag nil))
        (with-temp-file file (insert "#+title: Inbox\n\n* TODO added outside\n")))
      (organon-test--with-temp-method "test.touch"
          (lambda (_) (organon-with-file file (insert "x")))
        (should (equal (organon-test-error-code "test.touch") "conflict")))
      (should (string-match-p "added outside" (organon-test-file-string "org/tasks/inbox.org"))))))

(ert-deftest organon-core/failed-write-leaves-file-and-buffer-untouched ()
  (organon-test-with-instance "basic" "2026-10-02 05:29:00"
    (let* ((file (organon-test-file "org/tasks/inbox.org"))
           (before (organon-test-file-string "org/tasks/inbox.org")))
      (organon-test--with-temp-method "test.half-write"
          (lambda (_) (organon-with-file file
                        (goto-char (point-max))
                        (insert "* TODO half written\n")
                        (error "Boom")))
        (should (equal (organon-test-error-code "test.half-write") "internal")))
      (should (equal (organon-test-file-string "org/tasks/inbox.org") before))
      (with-current-buffer (get-file-buffer file)
        (should-not (buffer-modified-p))
        (should-not (string-match-p "half written" (buffer-string)))))))

;;;; 3.3 Time model

(ert-deftest organon-core/missing-config-makes-engine-unavailable ()
  "time-model: Missing declaration."
  (organon-test-with-instance "basic" "2026-10-02 05:29:00"
    (delete-file (organon-test-file "organon.json"))
    (organon-load-config)
    (let ((err (alist-get 'error (organon-test-call "ping"))))
      (should (equal (alist-get 'code err) "unavailable"))
      (should (string-match-p "organon\\.json" (alist-get 'message err))))
    (should (equal (organon-test-error-code "meta") "unavailable"))))

(ert-deftest organon-core/invalid-zone-is-named ()
  "time-model: Invalid zone."
  (organon-test-with-instance "basic" "2026-10-02 05:29:00"
    (dolist (zone '("Mars/Olympus" "../../etc/passwd" "Asia/Seoul;rm"))
      (let ((file-precious-flag nil))
        (with-temp-file (organon-test-file "organon.json")
          (insert (json-serialize `((calendar_tz . ,zone))))))
      (organon-load-config)
      (let ((err (alist-get 'error (organon-test-call "ping"))))
        (should (equal (alist-get 'code err) "unavailable"))
        (should (string-match-p (regexp-quote zone) (alist-get 'message err)))))))

(ert-deftest organon-core/day-boundary-follows-calendar-zone ()
  "time-model: Day boundary in Asia/Seoul (container TZ is UTC)."
  (organon-test-with-instance "basic" "2026-10-02 23:30:00"
    (should (equal (organon-json-instant (current-time)) "2026-10-02T23:30:00Z"))
    (should (equal (alist-get 'today (organon-test-result "meta")) "2026-10-03"))))

;;; organon-core-test.el ends here
