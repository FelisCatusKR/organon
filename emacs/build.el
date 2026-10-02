;;; build.el --- Ahead-of-time compile the engine  -*- lexical-binding: t; -*-

;; Run once during the image build:
;;   emacs -q --batch -l /opt/organon/emacs/build.el
;;
;; Byte-compiles every organon*.el next to this file and, when native
;; compilation is available, writes .eln files to ./eln/.  init.el adds that
;; directory to `native-comp-eln-load-path' and disables JIT compilation, so the
;; engine never compiles anything at startup.  The trampolines for the advised
;; prompt primitives go to ./eln/ as well.

;;; Code:

(let* ((dir (file-name-directory load-file-name))
       (files (directory-files dir t "\\`organon.*\\.el\\'")))
  (add-to-list 'load-path dir)
  (setq byte-compile-error-on-warn nil)
  (dolist (file files)
    (unless (byte-compile-file file)
      (message "build: byte-compilation failed: %s" file)
      (kill-emacs 1)))
  (when (and files (native-comp-available-p))
    (setq native-compile-target-directory (expand-file-name "eln/" dir))
    (dolist (file files)
      (native-compile file))
    ;; Loading the engine advises the prompt primitives, which compiles their
    ;; trampolines into `native-compile-target-directory' (see init.el).
    (load (expand-file-name "init.el" dir) nil t)))

;;; build.el ends here
