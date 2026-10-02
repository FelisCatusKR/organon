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
       (files (directory-files dir t "\\`organon.*\\.el\\'"))
       (native (and files (native-comp-available-p))))
  (add-to-list 'load-path dir)
  (setq byte-compile-error-on-warn nil)
  (when native
    ;; organon.el advises the prompt primitives when it is first loaded, which
    ;; happens below while byte-compiling organon-task.el (it requires
    ;; organon).  With trampolines enabled, that writes their trampolines to
    ;; `native-compile-target-directory'.  site-start.el disables them, so
    ;; enable them before anything loads organon.el.
    (setq native-compile-target-directory (expand-file-name "eln/" dir)
          native-comp-enable-subr-trampolines t))
  (dolist (file files)
    (unless (byte-compile-file file)
      (message "build: byte-compilation failed: %s" file)
      (kill-emacs 1)))
  (when native
    (dolist (file files)
      (native-compile file))
    (require 'organon)
    (unless (directory-files-recursively native-compile-target-directory "\\`subr--trampoline-")
      (message "build: no trampolines were written to %s" native-compile-target-directory)
      (kill-emacs 1))))

;;; build.el ends here
