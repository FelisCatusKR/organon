;;; organon-node-test.el --- Knowledge nodes and the org-roam index  -*- lexical-binding: t; -*-

;; Scenarios: openspec/specs/{knowledge-nodes,knowledge-index}.
;; Fixture: tests/fixtures/knowledge.  Clocks are UTC: "2026-10-02 05:29:00"
;; is 14:29 in Asia/Seoul, so new node files are named 20261002142900-*.org.

;;; Code:

(require 'organon-test-helpers)
(require 'organon-node)

(defconst organon-test-node-clock "2026-10-02 05:29:00")

(defconst organon-test-emacs-note "aaaaaaaa-0000-4000-8000-000000000001")
(defconst organon-test-keys-note "aaaaaaaa-0000-4000-8000-000000000002")
(defconst organon-test-reading-list "aaaaaaaa-0000-4000-8000-000000000003")
(defconst organon-test-garden-idea "aaaaaaaa-0000-4000-8000-000000000004")
(defconst organon-test-archived-note "bbbbbbbb-0000-4000-8000-000000000001")
(defconst organon-test-linking-task "cccccccc-0000-4000-8000-000000000001")

(defmacro organon-test-with-knowledge (&rest body)
  (declare (indent 0))
  `(organon-test-with-instance "knowledge" organon-test-node-clock ,@body))

(defun organon-test-node-ids (items)
  (mapcar (lambda (item) (alist-get 'id item)) items))

(defun organon-test-refs (method id)
  "Result of METHOD (node.backlinks or node.links) for ID as (KIND ID) pairs."
  (mapcar (lambda (ref) (list (alist-get 'kind ref) (alist-get 'id ref)))
          (organon-test-result method `((id . ,id)))))

(defun organon-test-search (&rest params)
  (organon-test-node-ids (organon-test-result "nodes.search" params)))

(defun organon-test-write-outside (relative text)
  "Write TEXT to RELATIVE as another process would: no Emacs save, and a
modification time that visibly differs from the previous one."
  (let ((file (organon-test-file relative))
        (file-precious-flag nil))
    (make-directory (file-name-directory file) t)
    (with-temp-file file (insert text))
    (set-file-times file (time-add (current-time) 5))))

(defun organon-test-index-snapshot ()
  "Every query result the index answers, for comparing before and after a rebuild."
  (let ((nodes (organon-test-result "nodes.search")))
    (list nodes
          (mapcar (lambda (node)
                    (let ((id (alist-get 'id node)))
                      (list id (organon-test-refs "node.backlinks" id) (organon-test-refs "node.links" id))))
                  nodes)
          (organon-test-refs "node.backlinks" organon-test-archived-note))))

;;;; 1.1 Configuration, writes are indexed

(ert-deftest organon-node/index-stays-out-of-the-data-directory ()
  "knowledge-index: Index stays out of the data directory."
  (organon-test-with-knowledge
    (organon-test-result "node.create" '((title . "Indexed")))
    (organon-test-result "nodes.search")
    (should (file-exists-p (expand-file-name "org-roam.db" organon-cache-dir)))
    (should-not (directory-files-recursively organon-test-instance "\\.db\\'"))))

(ert-deftest organon-node/task-that-links-to-a-note ()
  "knowledge-index: Task that links to a note."
  (organon-test-with-knowledge
    (organon-test-result "nodes.search")    ; index built before the write
    (let ((task (organon-test-result "task.create"
                                     `((title . "Re-read the notes")
                                       (body . ,(format "See [[id:%s][the note]]." organon-test-emacs-note))))))
      (should (member (list "task" (alist-get 'id task))
                      (organon-test-refs "node.backlinks" organon-test-emacs-note))))))

;;;; 1.2 Startup and rebuild

(ert-deftest organon-node/rebuild-after-the-index-is-deleted ()
  "knowledge-index: Rebuild after the index is deleted."
  (organon-test-with-knowledge
    (organon-test-result "node.create"
                         `((title . "Links back") (body . ,(format "[[id:%s][x]]" organon-test-keys-note))))
    (let ((before (organon-test-index-snapshot)))
      (organon--index-reset)
      (should-not (file-exists-p org-roam-db-location))
      (organon-index-startup)
      (should (eq organon--index-state 'rebuilding))
      (organon-test-wait-for-index)
      (should (eq organon--index-state 'ready))
      (should (file-exists-p org-roam-db-location))
      (should-not (file-exists-p (concat org-roam-db-location ".rebuild")))
      (should (equal (organon-test-index-snapshot) before)))))

(ert-deftest organon-node/corrupt-index ()
  "knowledge-index: Corrupt index."
  (organon-test-with-knowledge
    (let ((before (organon-test-index-snapshot)))
      (org-roam-db--close-all)
      (let ((file-precious-flag nil))
        (with-temp-file org-roam-db-location (insert "this is not a database\n")))
      (clrhash organon--index-stats)
      (organon-index-startup)
      (organon-test-wait-for-index)
      (should (equal (organon-test-index-snapshot) before)))))

;; A fresh instance: org/ exists but holds no .org file.
(ert-deftest organon-node/corrupt-index-without-any-file ()
  "knowledge-index: Corrupt index without any file."
  (organon-test-with-knowledge
    (dolist (file (directory-files-recursively (organon-test-file "org/") "\\.org\\'"))
      (delete-file file))
    (make-directory organon-cache-dir t)
    (let ((file-precious-flag nil))
      (with-temp-file org-roam-db-location (insert "this is not a database\n")))
    (organon-index-startup)
    (organon-test-wait-for-index)
    (should (equal (organon-test-result "nodes.search") []))))

(ert-deftest organon-node/node-request-during-a-rebuild ()
  "knowledge-index: Node request during a rebuild; Progress; Ready after the rebuild.
Tasks are served meanwhile (Tasks are served during a rebuild)."
  (organon-test-with-knowledge
    (organon-test-result "nodes.search")
    (organon--index-reset)
    (organon-index-startup)
    (should (equal (alist-get 'state (alist-get 'index (organon-test-result "meta"))) "rebuilding"))
    (should (equal (alist-get 'index (organon-test-result "ping")) "rebuilding"))
    (let ((before (directory-files (organon-test-file "org/knowledge/"))))
      (dolist (call `(("nodes.search" . nil)
                      ("node.get" . ((id . ,organon-test-emacs-note)))
                      ("node.backlinks" . ((id . ,organon-test-emacs-note)))
                      ("node.create" . ((title . "Too early")))))
        (should (equal (organon-test-error-code (car call) (cdr call)) "index_rebuilding")))
      (should (equal (directory-files (organon-test-file "org/knowledge/")) before)))
    ;; Progress arrives as the child indexes files: record what meta shows.
    (let (seen)
      (while organon--index-process
        (accept-process-output organon--index-process 0.05)
        (let ((index (alist-get 'index (organon-test-result "meta"))))
          (when (and organon--index-process (alist-get 'files_done index))
            (push (cons (alist-get 'files_done index) (alist-get 'files_total index)) seen))))
      (should seen)
      (dolist (progress seen)
        (should (<= (car progress) (cdr progress)))))
    (should (organon-test-result "tasks.list"))
    (let ((index (alist-get 'index (organon-test-result "meta"))))
      (should (equal (alist-get 'state index) "ready"))
      (should (null (alist-get 'files_done index))))
    (should (equal (alist-get 'index (organon-test-result "ping")) "ready"))
    (should (= (length (organon-test-result "nodes.search")) 4))))

(ert-deftest organon-node/task-written-during-a-rebuild ()
  "knowledge-index: Task written during a rebuild."
  (organon-test-with-knowledge
    (organon--index-reset)
    (organon-index-startup)
    (let ((task (organon-test-result "task.create"
                                     `((title . "Written meanwhile")
                                       (body . ,(format "[[id:%s][x]]" organon-test-reading-list))))))
      (organon-test-wait-for-index)
      (should (equal (organon-test-refs "node.backlinks" organon-test-reading-list)
                     `(("task" ,(alist-get 'id task))))))))

(ert-deftest organon-node/failed-rebuild ()
  "knowledge-index: Index state is reported (a failed rebuild leaves node requests internal)."
  (organon-test-with-knowledge
    (organon--index-reset)
    (let ((organon-init-file "/nonexistent/init.el"))
      (organon-index-startup))
    (organon-test-wait-for-index)
    (should (eq organon--index-state 'failed))
    (should (equal (alist-get 'state (alist-get 'index (organon-test-result "meta"))) "failed"))
    (should (equal (organon-test-error-code "nodes.search") "internal"))
    (should (organon-test-result "tasks.list"))
    (should-not (file-exists-p (concat org-roam-db-location ".rebuild")))))

;;;; 1.3 Changes outside the engine

(ert-deftest organon-node/note-written-by-another-process ()
  "knowledge-index: Note written by another process."
  (organon-test-with-knowledge
    (organon-test-result "nodes.search")
    (organon-test-write-outside
     "org/knowledge/outside.org"
     (format ":PROPERTIES:\n:ID:       eeeeeeee-0000-4000-8000-000000000001\n:END:\n#+title: Written outside\n\n[[id:%s][link]]\n"
             organon-test-emacs-note))
    (should (equal (organon-test-search '(q . "outside")) '("eeeeeeee-0000-4000-8000-000000000001")))
    (should (member '("node" "eeeeeeee-0000-4000-8000-000000000001")
                    (organon-test-refs "node.backlinks" organon-test-emacs-note)))))

(ert-deftest organon-node/link-removed-by-another-process ()
  "knowledge-index: Link removed by another process.
The file is open in the engine (read by node.get), so this also checks that a
stale buffer is re-read rather than indexed."
  (organon-test-with-knowledge
    (organon-test-result "node.get" `((id . ,organon-test-keys-note)))
    (should (member (list "node" organon-test-keys-note)
                    (organon-test-refs "node.backlinks" organon-test-emacs-note)))
    (organon-test-write-outside
     "org/knowledge/keys.org"
     (format ":PROPERTIES:\n:ID:       %s\n:END:\n#+title: Key bindings\n\nNo links any more.\n"
             organon-test-keys-note))
    (should-not (member (list "node" organon-test-keys-note)
                        (organon-test-refs "node.backlinks" organon-test-emacs-note)))
    (should (equal (alist-get 'body (organon-test-result "node.get" `((id . ,organon-test-keys-note))))
                   "No links any more."))))

(ert-deftest organon-node/file-deleted-while-the-engine-was-stopped ()
  "knowledge-index: File deleted while the engine was stopped."
  (organon-test-with-knowledge
    (organon-test-result "nodes.search")
    (org-roam-db--close-all)              ; "stop"
    (delete-file (organon-test-file "org/knowledge/emacs.org"))
    (clrhash organon--index-stats)        ; a new process remembers nothing
    (organon-index-startup)
    (should (equal (organon-test-error-code "node.get" `((id . ,organon-test-emacs-note))) "not_found"))
    (should-not (member organon-test-emacs-note (organon-test-search)))))

(ert-deftest organon-node/unchanged-files-are-not-synced ()
  "Node queries sync only after a file changed (design D3)."
  (organon-test-with-knowledge
    (organon-test-result "nodes.search")
    (let ((syncs 0))
      (cl-letf* ((sync (symbol-function 'org-roam-db-sync))
                 ((symbol-function 'org-roam-db-sync)
                  (lambda (&rest args) (cl-incf syncs) (apply sync args))))
        (organon-test-result "nodes.search")
        (organon-test-result "node.create" '((title . "Saved by the engine")))
        (organon-test-result "node.get" `((id . ,organon-test-emacs-note)))
        (should (= syncs 0))
        (organon-test-write-outside "org/knowledge/new.org" "#+title: no id\n")
        (organon-test-result "nodes.search")
        (should (= syncs 1))))))

;;;; 1.4 node.create

(ert-deftest organon-node/node-is-persisted-as-an-org-file ()
  "knowledge-nodes: Node is persisted as an Org file."
  (organon-test-with-knowledge
    (let* ((node (organon-test-result "node.create" '((title . "Emacs 설정 노트"))))
           (file "org/knowledge/20261002142900-emacs_설정_노트.org"))
      (should (equal (alist-get 'id node) "00000000-0000-4000-8000-000000000001"))
      (should (equal (alist-get 'title node) "Emacs 설정 노트"))
      (should (equal (alist-get 'location_hint node) "knowledge/20261002142900-emacs_설정_노트.org"))
      (should (equal (alist-get 'aliases node) []))
      (should (equal (alist-get 'body node) ""))
      (organon-test-check-golden "node/create-basic.org" file))))

(ert-deftest organon-node/tags-and-aliases ()
  "knowledge-nodes: Tags and aliases."
  (organon-test-with-knowledge
    (let ((node (organon-test-result "node.create" '((title . "Init file")
                                                    (tags . ["emacs" "tools"])
                                                    (aliases . ["init.el" "닷 이맥스" "say \"hi\""])))))
      (should (equal (alist-get 'tags node) ["emacs" "tools"]))
      (should (equal (alist-get 'aliases node) ["init.el" "닷 이맥스" "say \"hi\""]))
      (organon-test-check-golden "node/create-tags-aliases.org"
                                 "org/knowledge/20261002142900-init_file.org"))))

(ert-deftest organon-node/same-title-twice ()
  "knowledge-nodes: Same title twice."
  (organon-test-with-knowledge
    (let ((a (organon-test-result "node.create" '((title . "Twice"))))
          (b (organon-test-result "node.create" '((title . "Twice")))))
      (should-not (equal (alist-get 'id a) (alist-get 'id b)))
      (should (equal (alist-get 'location_hint a) "knowledge/20261002142900-twice.org"))
      (should (equal (alist-get 'location_hint b) "knowledge/20261002142900-twice-2.org")))))

(ert-deftest organon-node/title-with-a-newline ()
  "knowledge-nodes: Title with a newline."
  (organon-test-with-knowledge
    (let ((before (directory-files (organon-test-file "org/knowledge/"))))
      (should (equal (organon-test-error-code "node.create" '((title . "two\nlines"))) "invalid"))
      (should (equal (organon-test-error-code "node.create" '((title . "ok") (aliases . ["two\nlines"])))
                     "invalid"))
      (should (equal (organon-test-error-code "node.create" '((title . "   "))) "invalid"))
      (should (equal (organon-test-error-code "node.create" '((title . "ok") (aliases . "a"))) "invalid"))
      (should (equal (organon-test-error-code "node.create" '((title . "ok") (tags . ["a b"]))) "invalid"))
      (should (equal (directory-files (organon-test-file "org/knowledge/")) before)))))

(ert-deftest organon-node/body-that-looks-like-a-heading ()
  "knowledge-nodes: Body that looks like a heading."
  (organon-test-with-knowledge
    (let* ((body "First line\n* TODO injected\n#+title: other\n:PROPERTIES:\nSee <2026-10-02 Fri>.")
           (node (organon-test-result "node.create" `((title . "Escaped") (body . ,body))))
           (file "org/knowledge/20261002142900-escaped.org"))
      (should (equal (alist-get 'title node) "Escaped"))
      (should (equal (alist-get 'body node)
                     "First line\n* TODO injected\n#+title: other\n:PROPERTIES:\nSee [2026-10-02 Fri]."))
      (should-not (string-match-p "^\\*" (organon-test-file-string file)))
      (organon-test-check-golden "node/create-escaped-body.org" file))))

(ert-deftest organon-node/link-in-a-body ()
  "knowledge-nodes: Link in a body."
  (organon-test-with-knowledge
    (let* ((link (format "[[id:%s][the other note]]" organon-test-emacs-note))
           (node (organon-test-result "node.create" `((title . "Linking") (body . ,(concat "See " link "."))))))
      (should (equal (alist-get 'body node) (concat "See " link ".")))
      (should (string-match-p (regexp-quote link)
                              (organon-test-file-string "org/knowledge/20261002142900-linking.org"))))))

(ert-deftest organon-node/new-node-is-searchable ()
  "knowledge-index: New node is searchable."
  (organon-test-with-knowledge
    (organon-test-result "nodes.search")
    (let ((node (organon-test-result "node.create" '((title . "Fresh thought")))))
      (should (equal (organon-test-search '(q . "fresh")) (list (alist-get 'id node)))))))

(ert-deftest organon-node/new-node-that-cannot-be-indexed ()
  "knowledge-index: New node that cannot be indexed."
  (organon-test-with-knowledge
    (organon-test-result "nodes.search")
    (cl-letf (((symbol-function 'org-roam-db-update-file)
               (lambda (&rest _) (error "Simulated parse failure")))
              ;; org-roam reports per-file failures with `lwarn'.
              ((symbol-function 'display-warning) #'ignore))
      (should (equal (organon-test-error-code "node.create" '((title . "Unindexable"))) "internal")))
    (should (file-exists-p (organon-test-file "org/knowledge/20261002142900-unindexable.org")))))

(ert-deftest organon-node/link-in-a-title ()
  "knowledge-nodes: Link in a title."
  (organon-test-with-knowledge
    (let ((before (directory-files (organon-test-file "org/knowledge/")))
          (link (format "[[id:%s][the config]]" organon-test-emacs-note)))
      (should (equal (organon-test-error-code "node.create" `((title . ,(concat "See " link)))) "invalid"))
      (should (equal (organon-test-error-code "node.create" `((title . "ok") (aliases . [,link]))) "invalid"))
      (should (equal (directory-files (organon-test-file "org/knowledge/")) before)))))

(ert-deftest organon-node/retried-creation ()
  "knowledge-nodes: Retried creation (engine side: a retry after an API timeout)."
  (organon-test-with-knowledge
    (let* ((params `((title . "Once") (idempotency_key . ,(make-string 64 ?a))
                     (idempotency_fingerprint . ,(make-string 64 ?b))))
           (first (organon-test-result "node.create" params))
           (second (organon-test-result "node.create" params)))
      (should (equal first second))
      (should (equal (directory-files (organon-test-file "org/knowledge/") nil "once")
                     '("20261002142900-once.org"))))))

;;;; 1.5 node.get

(ert-deftest organon-node/read-a-created-node ()
  "knowledge-nodes: Read a created node."
  (organon-test-with-knowledge
    (let* ((created (organon-test-result "node.create" '((title . "Round trip") (body . "  indented\n\nlast  ")
                                                        (tags . ["t"]) (aliases . ["rt"]))))
           (read (organon-test-result "node.get" `((id . ,(alist-get 'id created))))))
      (should (equal read created))
      (should (equal (alist-get 'body read) "  indented\n\nlast")))))

(ert-deftest organon-node/hand-written-heading-node ()
  "knowledge-nodes: Hand-written heading node."
  (organon-test-with-knowledge
    (let ((node (organon-test-result "node.get" `((id . ,organon-test-reading-list)))))
      (should (equal (alist-get 'title node) "Reading list"))
      (should (equal (alist-get 'body node) "Books to read this year."))
      (should (equal (alist-get 'location_hint node) "knowledge/reading.org")))
    (let ((node (organon-test-result "node.get" `((id . ,organon-test-emacs-note)))))
      (should (equal (alist-get 'title node) "Emacs 설정 노트"))
      (should (equal (alist-get 'aliases node) ["init.el" "닷 이맥스"]))
      (should (equal (alist-get 'tags node) ["emacs" "tools"]))
      (should (equal (alist-get 'body node) "Notes about my Emacs configuration.")))))

(ert-deftest organon-node/hand-written-file-node ()
  "knowledge-nodes: Hand-written file node."
  (organon-test-with-knowledge
    (organon-test-write-outside
     "org/knowledge/commented.org"
     (concat "# -*- mode: org -*-\n"
             ":PROPERTIES:\n:ID:       eeeeeeee-0000-4000-8000-000000000002\n:END:\n"
             "#+title: Commented\n#+filetags: :pics:\n\n"
             "#+caption: A picture\n[[file:pond.png]]\n\nMore text.\n"))
    (let ((node (organon-test-result "node.get" '((id . "eeeeeeee-0000-4000-8000-000000000002")))))
      (should (equal (alist-get 'title node) "Commented"))
      (should (equal (alist-get 'tags node) ["pics"]))
      (should (equal (alist-get 'body node) "#+caption: A picture\n[[file:pond.png]]\n\nMore text.")))))

(ert-deftest organon-node/unknown-node ()
  "knowledge-nodes: Unknown node."
  (organon-test-with-knowledge
    (should (equal (organon-test-error-code "node.get" '((id . "dddddddd-0000-4000-8000-000000000404")))
                   "not_found"))
    (should (equal (organon-test-error-code "node.get" '((id . "../../etc/passwd"))) "invalid"))))

(ert-deftest organon-node/task-id-under-nodes ()
  "knowledge-nodes: Task ID under /nodes."
  (organon-test-with-knowledge
    (dolist (method '("node.get" "node.backlinks" "node.links"))
      (should (equal (organon-test-error-code method `((id . ,organon-test-linking-task))) "not_found")))
    (should-not (organon-test-search '(q . "Read the Emacs notes")))))

;;;; 1.6 nodes.search

(ert-deftest organon-node/match-on-an-alias-ignoring-case ()
  "knowledge-nodes: Match on an alias, ignoring case."
  (organon-test-with-knowledge
    (should (equal (organon-test-search '(q . "INIT.EL")) (list organon-test-emacs-note)))
    (should (equal (organon-test-search '(q . "설정")) (list organon-test-emacs-note)))
    (should (equal (organon-test-search '(q . "닷")) (list organon-test-emacs-note)))))

(ert-deftest organon-node/filter-by-tag ()
  "knowledge-nodes: Filter by tag."
  (organon-test-with-knowledge
    (should (equal (organon-test-search '(tag . "emacs"))
                   (list organon-test-emacs-note organon-test-keys-note)))
    (should (equal (organon-test-search '(tag . "tools") '(q . "init")) (list organon-test-emacs-note)))
    (should (equal (organon-test-search '(tag . "tools") '(q . "keys")) nil))))

(ert-deftest organon-node/hangul-tag ()
  "knowledge-nodes: Hangul tag."
  (organon-test-with-knowledge
    (let ((node (organon-test-result "node.create" '((title . "한글 태그") (tags . ["이맥스"])))))
      (should (equal (organon-test-search '(tag . "이맥스")) (list (alist-get 'id node)))))))

(ert-deftest organon-node/archived-notes-are-not-searched ()
  "knowledge-nodes: Archived notes are not searched."
  (organon-test-with-knowledge
    (should-not (member organon-test-archived-note (organon-test-search '(q . "emacs"))))
    (should (equal (alist-get 'title (organon-test-result "node.get" `((id . ,organon-test-archived-note))))
                   "Old Emacs config"))))

(ert-deftest organon-node/heading-with-another-keyword ()
  "knowledge-nodes: Heading with another keyword."
  (organon-test-with-knowledge
    (should (equal (organon-test-search '(q . "Garden")) (list organon-test-garden-idea)))))

(ert-deftest organon-node/search-without-filters ()
  "knowledge-nodes: Search nodes (every node outside archive/, ordered by title)."
  (organon-test-with-knowledge
    (let ((items (organon-test-result "nodes.search")))
      (should (equal (mapcar (lambda (n) (alist-get 'title n)) items)
                     '("Emacs 설정 노트" "Garden pond" "Key bindings" "Reading list")))
      (should (equal (alist-get 'location_hint (aref items 0)) "knowledge/emacs.org")))))

(ert-deftest organon-node/invalid-search ()
  "knowledge-nodes: Invalid search (engine side)."
  (organon-test-with-knowledge
    (should (equal (organon-test-error-code "nodes.search" `((q . ,(make-string 201 ?x)))) "invalid"))
    (should (equal (organon-test-error-code "nodes.search" '((q . "a\tb"))) "invalid"))
    (should (equal (organon-test-error-code "nodes.search" '((tag . "a b"))) "invalid"))))

;;;; 1.7 Backlinks and links

(ert-deftest organon-node/backlinks-from-a-note-and-a-task ()
  "knowledge-nodes: Backlinks from a note and a task; Backlink from an archived file."
  (organon-test-with-knowledge
    (should (equal (organon-test-refs "node.backlinks" organon-test-emacs-note)
                   `(("node" ,organon-test-keys-note)
                     ("node" ,organon-test-archived-note)
                     ("task" ,organon-test-linking-task))))
    (should (equal (mapcar (lambda (r) (alist-get 'title r))
                           (organon-test-result "node.backlinks" `((id . ,organon-test-emacs-note))))
                   '("Key bindings" "Old Emacs config" "Read the Emacs notes")))))

(ert-deftest organon-node/new-link-appears-at-once ()
  "knowledge-nodes: New link appears at once."
  (organon-test-with-knowledge
    (organon-test-result "node.backlinks" `((id . ,organon-test-reading-list)))
    (let ((node (organon-test-result "node.create"
                                     `((title . "Points at the list")
                                       (body . ,(format "[[id:%s]]" organon-test-reading-list))))))
      (should (equal (organon-test-refs "node.backlinks" organon-test-reading-list)
                     `(("node" ,(alist-get 'id node))))))))

(ert-deftest organon-node/links-of-a-note ()
  "knowledge-nodes: Links of a note; Dangling link."
  (organon-test-with-knowledge
    (should (equal (organon-test-refs "node.links" organon-test-keys-note)
                   `(("node" ,organon-test-emacs-note) ("task" ,organon-test-linking-task))))
    (should (equal (organon-test-refs "node.links" organon-test-reading-list) nil))
    (should (equal (organon-test-error-code "node.backlinks" '((id . "dddddddd-0000-4000-8000-000000000404")))
                   "not_found"))))

(provide 'organon-node-test)

;;; organon-node-test.el ends here
