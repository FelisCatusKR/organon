;;; site-start.el --- Organon: disable runtime native compilation  -*- lexical-binding: t; -*-

;; Installed as /etc/emacs/site-start.d/00aaa-organon.el so that it runs
;; before Debian's own site-start files (which may load packages and would
;; otherwise queue JIT compilations into the cache directory).
;;
;; Everything Organon loads is compiled ahead of time (see build.el) or ships
;; compiled with Debian; compiling at runtime costs minutes of CPU on a Pi.
;; Trampolines are what native-comp generates when a C primitive is advised.
;; They are disabled here and enabled only while organon.el advises the prompt
;; primitives (init.el), whose trampolines build.el compiles ahead of time.

;;; Code:

(setq native-comp-jit-compilation nil
      native-comp-enable-subr-trampolines nil)

;;; site-start.el ends here
