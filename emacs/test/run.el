;;; run.el --- ERT entry point for the engine  -*- lexical-binding: t; -*-

;; Loaded by scripts/test-elisp.sh.  Loads the engine from this checkout (not
;; the copy baked into the image), then every *-test.el file next to this one.

;;; Code:

;; ERT asks git for the Emacs repository version; the test image has no git.
(advice-add 'emacs-repository-get-version :override #'ignore)

(let ((dir (file-name-directory load-file-name)))
  (load (expand-file-name "../init.el" dir) nil t)
  (add-to-list 'load-path dir)
  (require 'organon-test-helpers)
  (dolist (file (directory-files dir t "-test\\.el\\'"))
    (load file nil t)))

(let ((selector (getenv "ORGANON_TEST_SELECTOR")))
  (ert-run-tests-batch-and-exit (if (and selector (not (string-empty-p selector))) selector t)))

;;; run.el ends here
