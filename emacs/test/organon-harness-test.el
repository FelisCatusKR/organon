;;; organon-harness-test.el --- Tests for the test harness itself  -*- lexical-binding: t; -*-

;;; Code:

(require 'organon-test-helpers)

(ert-deftest organon-harness/clock-is-frozen ()
  (organon-test-with-instance "harness" "2026-10-02 14:29:00"
    (should (equal (format-time-string "%F %T") "2026-10-02 14:29:00"))
    (organon-test-set-clock "2026-12-31 23:59:00")
    (should (equal (format-time-string "%F %T") "2026-12-31 23:59:00"))))

(ert-deftest organon-harness/golden-match ()
  (organon-test-with-instance "harness" "2026-10-02 14:29:00"
    (organon-test-check-golden "harness/inbox.org" "org/tasks/inbox.org")))

;;; organon-harness-test.el ends here
