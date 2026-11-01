// Package web is the browser interface: server-rendered pages for signing in
// and working with tasks (spec: web-ui). It holds no task logic. Everything it
// shows or changes goes through the HTTP API's own handlers in process (see
// httpapi.Server.InProcess), under the token the session was created from.
//
// The pages use no JavaScript. Every change is a form POST answered with a
// redirect, so reloading a page cannot repeat it.
package web

import (
	"bytes"
	"context"
	"crypto/rand"
	"embed"
	"encoding/base64"
	"errors"
	"html/template"
	"io/fs"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/FelisCatusKR/organon/api/internal/auth"
	"github.com/FelisCatusKR/organon/api/internal/client"
	"github.com/FelisCatusKR/organon/api/internal/model"
)

//go:embed templates/*.html
var templateFiles embed.FS

//go:embed static/*
var staticFiles embed.FS

const (
	sessionCookie = "organon_session"
	langCookie    = "organon_lang"
	maxFormBytes  = 64 << 10

	contentSecurityPolicy = "default-src 'none'; style-src 'self'; img-src 'self'; " +
		"form-action 'self'; base-uri 'none'; frame-ancestors 'none'"
)

// Config is what the interface needs from the rest of the process.
type Config struct {
	Tokens       *auth.Set
	Limiter      *auth.FailureLimiter
	ClientAddr   func(*http.Request) string          // the key failed sign-ins count against
	Transport    func(*auth.Token) http.RoundTripper // calls the API in process as a token
	SecureCookie bool                                // false only for plain HTTP
	Logger       *slog.Logger
	Now          func() time.Time // nil: time.Now (tests set it)
}

// Handler serves the interface.
type Handler struct {
	cfg      Config
	sessions *sessions
	pages    map[string]*template.Template
	handler  http.Handler
}

// New parses the templates and builds the routes.
func New(cfg Config) (*Handler, error) {
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	pages, err := parsePages()
	if err != nil {
		return nil, err
	}
	h := &Handler{cfg: cfg, sessions: newSessions(cfg.Now), pages: pages}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /static/style.css", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFileFS(w, r, staticFiles, "static/style.css")
	})
	mux.HandleFunc("GET /login", h.loginPage)
	mux.HandleFunc("POST /login", h.login)
	mux.HandleFunc("POST /logout", h.logout)
	mux.HandleFunc("POST /language", h.setLanguage)
	mux.Handle("GET /{$}", h.signedIn(h.listPage("/")))
	mux.Handle("GET /tasks", h.signedIn(h.listPage("/tasks")))
	mux.Handle("GET /waiting", h.signedIn(h.listPage("/waiting")))
	mux.Handle("POST /tasks", h.signedIn(h.addTask))
	mux.Handle("POST /tasks/{id}/{action}", h.signedIn(h.transition))
	mux.Handle("GET /tasks/{id}/confirm", h.signedIn(h.confirmPage))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		h.errorPage(w, r, http.StatusNotFound, "error.notfound")
	})

	// Refuses state changes from another origin, using Sec-Fetch-Site and
	// Origin; requests that have neither header do not come from a browser form.
	csrf := http.NewCrossOriginProtection()
	h.handler = securityHeaders(csrf.Handler(mux))
	return h, nil
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) { h.handler.ServeHTTP(w, r) }

// securityHeaders adds what browsers enforce on every UI response.
// Cache-Control and nosniff come from the API's own middleware around this one.
func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Security-Policy", contentSecurityPolicy)
		w.Header().Set("Referrer-Policy", "no-referrer")
		next.ServeHTTP(w, r)
	})
}

// ---- templates -------------------------------------------------------------

// parsePages builds one template per page: the layout plus that page's content.
func parsePages() (map[string]*template.Template, error) {
	funcs := template.FuncMap{"t": translate}
	names, err := fs.Glob(templateFiles, "templates/*.html")
	if err != nil {
		return nil, err
	}
	pages := map[string]*template.Template{}
	for _, file := range names {
		name := strings.TrimSuffix(strings.TrimPrefix(file, "templates/"), ".html")
		if name == "layout" {
			continue
		}
		t, err := template.New("layout.html").Funcs(funcs).ParseFS(templateFiles, "templates/layout.html", file)
		if err != nil {
			return nil, err
		}
		pages[name] = t
	}
	return pages, nil
}

// pageData is what every page has; Content differs per page.
type pageData struct {
	Lang     string
	Langs    []string
	Path     string // this page, used to come back after a language switch
	SignedIn bool
	Tab      string
	Content  any
}

// render writes a page through a buffer, so that a template error becomes a
// clean 500 instead of half a page.
func (h *Handler) render(w http.ResponseWriter, r *http.Request, status int, page, path string, signedIn bool, content any) {
	data := pageData{
		Lang: requestLang(r), Langs: []string{"en", "ko"},
		Path: path, SignedIn: signedIn, Tab: path, Content: content,
	}
	var buf bytes.Buffer
	if err := h.pages[page].Execute(&buf, data); err != nil {
		h.logError(r, "render "+page, err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	w.Write(buf.Bytes())
}

type errorContent struct{ Message string }

func (h *Handler) errorPage(w http.ResponseWriter, r *http.Request, status int, message string) {
	signedIn := h.token(r) != nil
	h.render(w, r, status, "error", "/", signedIn, errorContent{message})
}

func (h *Handler) logError(r *http.Request, what string, err error) {
	if h.cfg.Logger != nil {
		h.cfg.Logger.Error("web", "what", what, "path", r.URL.Path, "error", err)
	}
}

// ---- sessions and sign-in ------------------------------------------------------

func (h *Handler) token(r *http.Request) *auth.Token {
	c, err := r.Cookie(sessionCookie)
	if err != nil {
		return nil
	}
	return h.sessions.get(c.Value)
}

type tokenKey struct{}

// signedIn sends requests without a valid session to the sign-in page.
func (h *Handler) signedIn(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tok := h.token(r)
		if tok == nil {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}
		next(w, r.WithContext(context.WithValue(r.Context(), tokenKey{}, tok)))
	}
}

// api returns a client for the API, acting as the session's token. The token
// value stays empty: the transport passes the identity in process.
func (h *Handler) api(r *http.Request) (*client.Client, *auth.Token) {
	tok := r.Context().Value(tokenKey{}).(*auth.Token)
	return &client.Client{BaseURL: "http://in-process", HTTP: &http.Client{Transport: h.cfg.Transport(tok)}}, tok
}

type loginContent struct{ Error string }

func (h *Handler) loginPage(w http.ResponseWriter, r *http.Request) {
	if h.token(r) != nil {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	h.render(w, r, http.StatusOK, "login", "/login", false, loginContent{})
}

// login exchanges a token for a session. PostFormValue reads only the request
// body, so a token in the URL (and so in logs and history) is never accepted.
func (h *Handler) login(w http.ResponseWriter, r *http.Request) {
	addr := h.cfg.ClientAddr(r)
	fail := func(status int, message string) {
		h.render(w, r, status, "login", "/login", false, loginContent{message})
	}
	if h.cfg.Limiter.Blocked(addr) {
		fail(http.StatusTooManyRequests, "login.blocked")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxFormBytes)
	tok := h.cfg.Tokens.Authenticate("Bearer " + r.PostFormValue("token"))
	if tok == nil {
		h.cfg.Limiter.Fail(addr)
		fail(http.StatusUnauthorized, "login.invalid")
		return
	}
	if !tok.Has(auth.ScopeRead) {
		fail(http.StatusForbidden, "login.noread")
		return
	}
	id, expires, err := h.sessions.create(tok)
	if err != nil {
		h.logError(r, "create session", err)
		h.errorPage(w, r, http.StatusInternalServerError, "error.internal")
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookie, Value: id, Path: "/", Expires: expires,
		HttpOnly: true, Secure: h.cfg.SecureCookie, SameSite: http.SameSiteStrictMode,
	})
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (h *Handler) logout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(sessionCookie); err == nil {
		h.sessions.remove(c.Value)
	}
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookie, Path: "/", MaxAge: -1,
		HttpOnly: true, Secure: h.cfg.SecureCookie, SameSite: http.SameSiteStrictMode,
	})
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

// setLanguage remembers the choice for a year. The cookie is Lax so that the
// language also holds when arriving from a link; it is not a secret.
func (h *Handler) setLanguage(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxFormBytes)
	if lang := r.PostFormValue("lang"); catalog[lang] != nil {
		http.SetCookie(w, &http.Cookie{
			Name: langCookie, Value: lang, Path: "/", MaxAge: 365 * 24 * 60 * 60,
			HttpOnly: true, Secure: h.cfg.SecureCookie, SameSite: http.SameSiteLaxMode,
		})
	}
	target := r.PostFormValue("back")
	if target != "/login" {
		target = backPath(target)
	}
	http.Redirect(w, r, target, http.StatusSeeOther)
}

// ---- task pages ----------------------------------------------------------------

// backPath returns a list to go back to. Only these fixed paths are possible,
// so a form field can never redirect anywhere else.
func backPath(v string) string {
	switch v {
	case "/tasks", "/waiting":
		return v
	}
	return "/"
}

// notices are the messages a redirect may ask a list to show. The query only
// selects one of these keys, so no request text ends up on the page.
var notices = map[string]bool{
	"conflict": true, "gone": true, "invalid": true, "forbidden": true, "unknown": true, "error": true,
}

type taskRow struct {
	model.Task
	AgendaKind string
	Repeats    bool
	Actions    []string
	Back       string
}

type listContent struct {
	Tasks    []taskRow
	CanWrite bool
	Notice   string
	AddKey   string
	Back     string
}

// actionsFor lists what a state offers; a closed task offers nothing.
func actionsFor(state model.State) []string {
	switch state {
	case "TODO", "NEXT", "WAITING":
		return []string{"start", "complete", "skip"}
	case "DOING":
		return []string{"complete", "skip"}
	}
	return nil
}

func repeats(t model.Task) bool {
	return (t.Scheduled != nil && t.Scheduled.Repeat != nil) || (t.Deadline != nil && t.Deadline.Repeat != nil)
}

func (h *Handler) listPage(path string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		api, tok := h.api(r)
		var list model.TaskList
		var err error
		switch path {
		case "/tasks":
			list, _, err = api.ListTasks(r.Context(), nil, "", "")
		case "/waiting":
			list, _, err = api.TaskView(r.Context(), "waiting", "")
		default:
			list, _, err = api.TaskView(r.Context(), "today", "")
		}
		if err != nil {
			h.apiErrorPage(w, r, err)
			return
		}
		key, err := randomKey()
		if err != nil {
			h.logError(r, "form key", err)
			h.errorPage(w, r, http.StatusInternalServerError, "error.internal")
			return
		}
		content := listContent{CanWrite: tok.Has(auth.ScopeTasksWrite), AddKey: key, Back: path}
		if n := r.URL.Query().Get("notice"); notices[n] {
			content.Notice = "notice." + n
		}
		for _, t := range list.Items {
			row := taskRow{Task: t, Repeats: repeats(t), Back: path}
			if content.CanWrite {
				row.Actions = actionsFor(t.State)
			}
			if t.Agenda != nil {
				row.AgendaKind = "kind." + string(t.Agenda.Kind)
			}
			content.Tasks = append(content.Tasks, row)
		}
		h.render(w, r, http.StatusOK, "list", path, true, content)
	}
}

func randomKey() (string, error) {
	raw := make([]byte, 18)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

// apiErrorPage shows a failed read. Details stay in the log: the API already
// logged engine failures and does not send their messages.
func (h *Handler) apiErrorPage(w http.ResponseWriter, r *http.Request, err error) {
	var apiErr *client.APIError
	switch {
	case errors.As(err, &apiErr) && apiErr.Status == http.StatusForbidden:
		h.errorPage(w, r, http.StatusForbidden, "error.forbidden")
	case errors.As(err, &apiErr) && apiErr.Status == http.StatusNotFound:
		h.errorPage(w, r, http.StatusNotFound, "error.notfound")
	case errors.As(err, &apiErr) && apiErr.Status == http.StatusServiceUnavailable:
		h.errorPage(w, r, http.StatusServiceUnavailable, "error.unavailable")
	default:
		h.logError(r, "api", err)
		h.errorPage(w, r, http.StatusInternalServerError, "error.internal")
	}
}

// noticeFor maps a failed change to the notice its list shows.
func noticeFor(err error) string {
	if client.OutcomeUnknown(err) {
		return "unknown"
	}
	var apiErr *client.APIError
	if errors.As(err, &apiErr) {
		switch apiErr.Status {
		case http.StatusConflict:
			return "conflict"
		case http.StatusNotFound:
			return "gone"
		case http.StatusUnprocessableEntity:
			return "invalid"
		case http.StatusForbidden:
			return "forbidden"
		}
	}
	return "error"
}

func redirectBack(w http.ResponseWriter, r *http.Request, back, notice string) {
	target := back
	if notice != "" {
		target += "?notice=" + notice
	}
	http.Redirect(w, r, target, http.StatusSeeOther)
}

// addTask is quick add. The form's key makes a double tap create one task.
func (h *Handler) addTask(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxFormBytes)
	back := backPath(r.PostFormValue("back"))
	title := strings.TrimSpace(r.PostFormValue("title"))
	key := r.PostFormValue("key")
	if title == "" || key == "" {
		redirectBack(w, r, back, "invalid")
		return
	}
	state := model.CreateTaskStateTODO
	req := model.CreateTask{Title: title, State: &state}
	if date := r.PostFormValue("deadline"); date != "" {
		req.Deadline = &model.TimestampInput{Date: date}
	}
	api, _ := h.api(r)
	if _, _, err := api.CreateTaskOnce(r.Context(), req, key); err != nil {
		redirectBack(w, r, back, addNotice(err))
		return
	}
	redirectBack(w, r, back, "")
}

// addNotice is noticeFor for quick add. A conflict here means the same form is
// still being processed (a double tap): the task is being created, so there is
// nothing to report.
func addNotice(err error) string {
	var apiErr *client.APIError
	if errors.As(err, &apiErr) && apiErr.Status == http.StatusConflict {
		return ""
	}
	return noticeFor(err)
}

var (
	idRE        = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
	webActions  = map[string]bool{"start": true, "complete": true, "skip": true}
	askBeforeOn = map[string]bool{"complete": true, "skip": true}
)

// transition applies an action with the state and version the page showed, so
// a change in between comes back as a conflict from the engine. Completing or
// skipping a repeating task moves its dates, which the API cannot undo, so an
// unconfirmed request goes to the confirmation page first.
func (h *Handler) transition(w http.ResponseWriter, r *http.Request) {
	id, action := r.PathValue("id"), r.PathValue("action")
	if !idRE.MatchString(id) || !webActions[action] {
		h.errorPage(w, r, http.StatusNotFound, "error.notfound")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxFormBytes)
	back := backPath(r.PostFormValue("back"))
	state := r.PostFormValue("expected_state")
	version := r.PostFormValue("expected_version")
	api, _ := h.api(r)

	if askBeforeOn[action] && r.PostFormValue("confirmed") == "" {
		task, _, err := api.GetTask(r.Context(), id)
		if err != nil {
			redirectBack(w, r, back, noticeFor(err))
			return
		}
		if repeats(task) {
			q := url.Values{
				"action": {action}, "expected_state": {state}, "expected_version": {version}, "back": {back},
			}
			http.Redirect(w, r, "/tasks/"+id+"/confirm?"+q.Encode(), http.StatusSeeOther)
			return
		}
	}
	if _, _, err := api.Transition(r.Context(), id, action, state, version); err != nil {
		redirectBack(w, r, back, noticeFor(err))
		return
	}
	redirectBack(w, r, back, "")
}

type confirmContent struct {
	ID, Title, Action, Message, State, Version, Back string
}

// confirmPage keeps the state and version of the original click rather than
// reading fresh ones: confirming must not hide a change made in between.
func (h *Handler) confirmPage(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	q := r.URL.Query()
	action := q.Get("action")
	if !idRE.MatchString(id) || !askBeforeOn[action] {
		h.errorPage(w, r, http.StatusNotFound, "error.notfound")
		return
	}
	api, _ := h.api(r)
	task, _, err := api.GetTask(r.Context(), id)
	if err != nil {
		h.apiErrorPage(w, r, err)
		return
	}
	h.render(w, r, http.StatusOK, "confirm", backPath(q.Get("back")), true, confirmContent{
		ID: id, Title: task.Title, Action: action, Message: "confirm." + action,
		State: q.Get("expected_state"), Version: q.Get("expected_version"), Back: backPath(q.Get("back")),
	})
}
