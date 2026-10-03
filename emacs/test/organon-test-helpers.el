;;; organon-test-helpers.el --- Shared ERT helpers  -*- lexical-binding: t; -*-

;; Every test runs against a fresh copy of a fixture data directory
;; (tests/fixtures/<name>/) under a frozen clock (libfaketime), and may compare
;; resulting .org files with golden copies in tests/golden/.

;;; Code:

(require 'ert)
(require 'cl-lib)

(defconst organon-test-root
  (expand-file-name "../../" (file-name-directory (or load-file-name buffer-file-name)))
  "Repository root.")

(defvar organon-test-instance nil
  "Data directory of the instance the current test runs against.")

;;;; Clock

(defun organon-test-set-clock (time-string)
  "Freeze the process clock at TIME-STRING, \"YYYY-MM-DD hh:mm:ss\" in UTC.
File timestamps stay real
(NO_FAKE_STAT=1) so that Emacs can still detect changes on disk."
  (let ((file (getenv "FAKETIME_TIMESTAMP_FILE")))
    (unless file
      (error "Tests must run under libfaketime; use scripts/test-elisp.sh"))
    ;; libfaketime parses the string on the first clock read after the file
    ;; changes, in the zone current at that moment, and keeps that value.
    ;; Force that first read to happen in UTC, whatever TZ the instance set.
    (let ((saved-tz (getenv "TZ")))
      (setenv "TZ" "UTC")
      (unwind-protect
          (progn
            (let ((file-precious-flag nil))
              (with-temp-file file (insert time-string "\n")))
            (current-time))
        (setenv "TZ" saved-tz)))))

;;;; Instances

(defvar organon-test--uuid-counter 0)

(defun organon-test--uuid ()
  "Deterministic replacement for `org-id-uuid' so golden files are stable."
  (format "00000000-0000-4000-8000-%012d" (cl-incf organon-test--uuid-counter)))

(defun organon-test--kill-buffers-under (dir)
  (dolist (buf (buffer-list))
    (let ((file (buffer-file-name buf)))
      (when (and file (string-prefix-p (file-truename dir) (file-truename file)))
        (with-current-buffer buf (set-buffer-modified-p nil))
        (kill-buffer buf)))))

(defun organon-test--call-with-instance (fixture clock fn)
  (let* ((tmp (file-name-as-directory (make-temp-file "organon-test-" t)))
         (data (expand-file-name "data/" tmp))
         (organon-test--uuid-counter 0)
         (saved-tz (getenv "TZ")))
    (copy-directory (expand-file-name (concat "tests/fixtures/" fixture "/") organon-test-root)
                    data nil t t)
    (organon-test-set-clock clock)
    (unwind-protect
        (cl-letf (((symbol-function 'org-id-uuid) #'organon-test--uuid))
          (let ((organon-test-instance data))
            (when (fboundp 'organon-configure)
              (organon-configure data (expand-file-name "cache/" tmp) (expand-file-name "run/" tmp)))
            (funcall fn)))
      (organon-test--kill-buffers-under tmp)
      (when (bound-and-true-p organon--server) (delete-process organon--server))
      ;; The org-roam database lives in the instance's cache directory.
      (when (fboundp 'org-roam-db--close-all) (org-roam-db--close-all))
      ;; Configuring an instance sets TZ process-wide; don't leak it.
      (setenv "TZ" saved-tz)
      (delete-directory tmp t))))

(defmacro organon-test-with-instance (fixture clock &rest body)
  "Run BODY against a fresh copy of FIXTURE with the clock frozen at CLOCK."
  (declare (indent 2))
  `(organon-test--call-with-instance ,fixture ,clock (lambda () ,@body)))

(defun organon-test-file (relative)
  "Absolute path of RELATIVE inside the current test instance."
  (expand-file-name relative organon-test-instance))

(defun organon-test-file-string (relative)
  (with-temp-buffer
    (insert-file-contents (organon-test-file relative))
    (buffer-string)))

;;;; Calling the engine

(defun organon-test-call (method &optional params)
  "Call METHOD through the full request path and return the decoded response.
PARAMS is an alist; the response is an alist with symbols as keys."
  (json-parse-string
   (organon-handle-request
    (json-serialize `((id . "t") (method . ,method) (params . ,(or params :null)))))
   :object-type 'alist :null-object nil :false-object nil))

(defun organon-test-result (method &optional params)
  "Call METHOD and return its result, failing the test on an error response."
  (let ((response (organon-test-call method params)))
    (unless (alist-get 'ok response)
      (ert-fail (list "unexpected error response" method (alist-get 'error response))))
    (alist-get 'result response)))

(defun organon-test-error-code (method &optional params)
  "Call METHOD expecting an error; return its code."
  (let ((response (organon-test-call method params)))
    (when (alist-get 'ok response)
      (ert-fail (list "expected an error response" method (alist-get 'result response))))
    (alist-get 'code (alist-get 'error response))))

;;;; Background index rebuild

(defun organon-test-wait-for-index (&optional seconds)
  "Wait until a background index rebuild has finished (default 120 s)."
  (let ((deadline (+ (float-time) (or seconds 120))))
    (while (and (bound-and-true-p organon--index-process) (< (float-time) deadline))
      (accept-process-output organon--index-process 0.1))
    (when (bound-and-true-p organon--index-process)
      (ert-fail "the index rebuild did not finish in time"))))

;;;; Golden files

(defun organon-test-check-golden (golden relative)
  "Assert that instance file RELATIVE equals tests/golden/GOLDEN.
With ORGANON_UPDATE_GOLDEN set, write the golden file instead."
  (let ((golden-file (expand-file-name (concat "tests/golden/" golden) organon-test-root))
        (actual (organon-test-file-string relative)))
    (if (getenv "ORGANON_UPDATE_GOLDEN")
        (progn
          (make-directory (file-name-directory golden-file) t)
          (let ((file-precious-flag nil)) (with-temp-file golden-file (insert actual))))
      (unless (file-exists-p golden-file)
        (ert-fail (format "missing golden file %s (run with ORGANON_UPDATE_GOLDEN=1)" golden)))
      (let ((expected (with-temp-buffer (insert-file-contents golden-file) (buffer-string))))
        (unless (equal actual expected)
          ;; Print the diff raw so it is readable in batch output.
          (message "\n%s" (organon-test--diff expected actual))
          (ert-fail (format "%s differs from golden %s (diff above)" relative golden)))))))

(defun organon-test--diff (expected actual)
  (let ((a (make-temp-file "expected-")) (b (make-temp-file "actual-")))
    (unwind-protect
        (progn
          (let ((file-precious-flag nil))
            (with-temp-file a (insert expected))
            (with-temp-file b (insert actual)))
          (with-temp-buffer
            (call-process "diff" nil t nil "-u" a b)
            (buffer-string)))
      (delete-file a) (delete-file b))))

(provide 'organon-test-helpers)

;;; organon-test-helpers.el ends here
