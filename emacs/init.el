;;; init.el --- Organon engine configuration  -*- lexical-binding: t; -*-

;; The engine is started with
;;   emacs -q --fg-daemon -l /opt/organon/emacs/init.el -f organon-start
;; and tests load this file in batch mode.  Nothing here reads user input.
;;
;; Settings are grouped by the reason they exist; see docs/architecture.md §6.3
;; (security), §6.2 (no side files, no prompts) and §7.1 (task workflow).

;;; Code:

;;;; Load path and compilation

(defvar organon-init-file (or load-file-name buffer-file-name)
  "This file: the background index rebuild starts a batch Emacs with it.")

(let ((dir (file-name-directory organon-init-file)))
  (add-to-list 'load-path dir)
  (when (boundp 'native-comp-eln-load-path)
    (add-to-list 'native-comp-eln-load-path (expand-file-name "eln/" dir))))

;; Everything we load is compiled ahead of time (build.el) or ships compiled
;; with Debian.  Never compile at runtime: it costs minutes of CPU on a Pi and
;; would write into the cache directory on every start.
(setq native-comp-jit-compilation nil)

;;;; Files: the data directory receives only .org files

(setq make-backup-files nil
      backup-inhibited t
      create-lockfiles nil
      auto-save-default nil
      auto-save-list-file-prefix nil
      ;; Write to a temp file and rename: a reader (or a backup) never sees a
      ;; half-written file.
      file-precious-flag t
      ;; ...and make the temp file durable before the rename, so that a power
      ;; cut (a Pi on an SD card) leaves either the old or the new file, never
      ;; an empty one.  The rename itself may be lost (no directory fsync).
      write-region-inhibit-fsync nil
      ;; Files changed on disk by another process are re-read without asking
      ;; (find-file-noselect would otherwise prompt and block the daemon).
      revert-without-query '(".")
      save-silently t)

(set-language-environment "UTF-8")
(prefer-coding-system 'utf-8-unix)

;;;; Security: data files never execute code

(setq enable-local-variables nil
      enable-local-eval nil
      enable-dir-local-variables nil)

;;;; Org

(require 'org)
(require 'org-agenda)
(require 'org-id)
;; Loaded lazily by Org when a repeater fires; load it at startup instead so
;; that its one-time initialisation never happens inside a request.
(require 'org-clock)

;; Org's default modules add link types for Gnus, IRC, EWW, Rmail...  Loading
;; them pulls in Gnus and D-Bus on first use, which the engine never needs
;; (and D-Bus initialisation can stall a headless process).
(setq org-modules nil)

(setq org-todo-keywords
      ;; "!" records a timestamp.  "@" (a note) is never used: it opens an
      ;; interactive buffer, which a headless engine cannot answer.
      '((sequence "TODO(t)" "NEXT(n)" "DOING(s!)" "WAITING(w!)"
                  "|" "DONE(d!)" "CANCELLED(c!)"))
      org-log-done 'time
      org-log-repeat 'time
      org-log-into-drawer t
      org-log-reschedule nil
      org-log-redeadline nil
      ;; Repeating tasks come back as NEXT unless REPEAT_TO_STATE says otherwise.
      org-todo-repeat-to-state "NEXT"
      org-treat-insert-todo-heading-as-state-change nil
      org-enforce-todo-dependencies nil
      org-priority-highest ?A
      org-priority-lowest ?C
      org-priority-default ?B
      ;; One space between title and tags keeps diffs and backups readable.
      org-tags-column 0
      org-adapt-indentation nil
      org-id-method 'uuid
      org-id-track-globally t
      ;; Org's on-disk element cache is a second source of state we don't need.
      org-element-cache-persistent nil
      org-confirm-babel-evaluate t)

;;;; Agenda

(setq org-deadline-warning-days 7
      org-agenda-skip-scheduled-if-done t
      org-agenda-skip-deadline-if-done t
      org-agenda-skip-timestamp-if-done t
      org-agenda-inhibit-startup t
      org-agenda-dim-blocked-tasks nil
      org-agenda-use-tag-inheritance nil
      org-agenda-sticky nil
      org-agenda-window-setup 'current-window
      ;; Computed per request from the data directory (organon-agenda-files).
      org-agenda-files nil)

;;;; org-roam (knowledge nodes)

;; Loading org-roam installs only Lisp advice (on org-id); the database and
;; directory are set per instance by organon-node.el.  We don't enable
;; `org-roam-db-autosync-mode': organon-node.el indexes saved files itself
;; (its commentary says why).
(require 'org-roam)

(setq org-roam-list-files-commands nil   ; list files in Lisp, no find/rg subprocess
      ;; Decrypting a .org.gpg/.org.age file would prompt for a passphrase.
      org-roam-file-exclude-regexp '("\\.\\(?:gpg\\|age\\)\\'"))

;;;; Engine

;; organon.el advises the prompt primitives (read-string, yes-or-no-p, ...).
;; Natively compiled callers, such as Debian's files.el, call primitives
;; directly and only see the advice through a trampoline.  site-start.el
;; disables trampolines so that nothing else compiles one at runtime; enable
;; them while organon.el installs its advice.  build.el compiles these
;; trampolines ahead of time into eln/, where they are found here.
(defvar native-comp-enable-subr-trampolines)
(let ((native-comp-enable-subr-trampolines t))
  (require 'organon))
(require 'organon-task)
(require 'organon-node)

;;; init.el ends here
