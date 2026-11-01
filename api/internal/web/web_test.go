package web

// Tests run the whole stack from a browser's point of view: the interface on
// top of the real API handlers, with only the engine faked. Comments name the
// scenarios of the web-ui and api-access specs.

import (
	"context"
	"encoding/json"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/FelisCatusKR/organon/api/internal/auth"
	"github.com/FelisCatusKR/organon/api/internal/client"
	"github.com/FelisCatusKR/organon/api/internal/httpapi"
	"github.com/FelisCatusKR/organon/api/internal/idem"
	"github.com/FelisCatusKR/organon/api/internal/rpc"
)

const (
	writeTok = "write-token"
	readTok  = "read-token"
	noteTok  = "no-read-token"
	plainID  = "11111111-1111-4111-8111-000000000001"
	repeatID = "11111111-1111-4111-8111-000000000002"
	addr     = "203.0.113.9:1234"
)

type fakeTask = map[string]any

// fakeEngine answers the methods the pages use from a small task table.
type fakeEngine struct {
	tasks       map[string]fakeTask
	today       []string
	down        bool
	created     []map[string]any
	transitions []map[string]any
}

func task(id, title, state string, repeat any) fakeTask {
	deadline := map[string]any{"date": "2026-10-25", "repeat": repeat, "time": nil, "warning_days": nil}
	return fakeTask{
		"id": id, "title": title, "state": state, "priority": nil, "tags": []string{"bills"},
		"scheduled": nil, "deadline": deadline, "repeat_to_state": nil, "closed_at": nil, "project": nil,
		"body": "", "version": "v1", "location_hint": "org/inbox.org",
	}
}

func (e *fakeEngine) Call(_ context.Context, method string, params, out any) error {
	if e.down {
		return &rpc.Error{Code: rpc.CodeUnavailable, Message: "down"}
	}
	var in map[string]any
	raw, _ := json.Marshal(params)
	json.Unmarshal(raw, &in)
	var result any
	switch method {
	case "tasks.today":
		items := []fakeTask{}
		for _, id := range e.today {
			items = append(items, e.tasks[id])
		}
		result = items
	case "tasks.list":
		items := []fakeTask{}
		for _, t := range e.tasks {
			items = append(items, t)
		}
		result = items
	case "tasks.waiting":
		result = []fakeTask{}
	case "task.get":
		t, ok := e.tasks[in["id"].(string)]
		if !ok {
			return &rpc.Error{Code: rpc.CodeNotFound, Message: "no such task"}
		}
		result = t
	case "task.create":
		e.created = append(e.created, in)
		result = task("11111111-1111-4111-8111-000000000099", in["title"].(string), "TODO", nil)
	case "task.transition":
		t := e.tasks[in["id"].(string)]
		if in["expected_version"] != t["version"] {
			return &rpc.Error{Code: rpc.CodeConflict, Message: "changed"}
		}
		e.transitions = append(e.transitions, in)
		result = map[string]any{"task": t, "warnings": []any{}}
	default:
		return &rpc.Error{Code: rpc.CodeInternal, Message: "unexpected " + method}
	}
	b, _ := json.Marshal(result)
	return json.Unmarshal(b, out)
}

type stack struct {
	h      http.Handler
	engine *fakeEngine
	now    time.Time
}

func newStack(t *testing.T, secure bool) *stack {
	t.Helper()
	line := func(name, scopes, tok string) string { return name + " " + scopes + " " + auth.Hash(tok) + "\n" }
	tokens, err := auth.Parse(strings.NewReader(
		line("writer", "read,tasks:write", writeTok) + line("reader", "read", readTok) + line("notes", "nodes:write", noteTok)))
	if err != nil {
		t.Fatal(err)
	}
	engine := &fakeEngine{
		tasks: map[string]fakeTask{
			plainID:  task(plainID, "Plain task", "NEXT", nil),
			repeatID: task(repeatID, "Monthly bill", "NEXT", "+1m"),
		},
		today: []string{repeatID},
	}
	s := &stack{engine: engine, now: time.Date(2026, 10, 2, 0, 0, 0, 0, time.UTC)}
	api := &httpapi.Server{
		Engine: engine, Tokens: tokens, Limiter: auth.NewFailureLimiter(3, time.Minute),
		Idempotency: idem.New(100, time.Hour), Version: "test",
	}
	ui, err := New(Config{
		Tokens: tokens, Limiter: api.Limiter, ClientAddr: api.ClientAddr, Transport: api.InProcess,
		SecureCookie: secure, Now: func() time.Time { return s.now },
	})
	if err != nil {
		t.Fatal(err)
	}
	api.UI = ui
	s.h = api.Handler()
	return s
}

type result struct {
	*httptest.ResponseRecorder
}

func (r result) text() string { return r.Body.String() }

func (r result) cookie(name string) *http.Cookie {
	for _, c := range r.Result().Cookies() {
		if c.Name == name {
			return c
		}
	}
	return nil
}

// send makes one request. headers come as name, value pairs.
func (s *stack) send(method, target string, form url.Values, cookies []*http.Cookie, headers ...string) result {
	var r *http.Request
	if form != nil {
		r = httptest.NewRequest(method, target, strings.NewReader(form.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	} else {
		r = httptest.NewRequest(method, target, nil)
	}
	r.RemoteAddr = addr
	for _, c := range cookies {
		r.AddCookie(c)
	}
	for i := 0; i+1 < len(headers); i += 2 {
		r.Header.Set(headers[i], headers[i+1])
	}
	w := httptest.NewRecorder()
	s.h.ServeHTTP(w, r)
	return result{w}
}

func (s *stack) signIn(t *testing.T, tok string) []*http.Cookie {
	t.Helper()
	res := s.send("POST", "/login", url.Values{"token": {tok}}, nil)
	c := res.cookie(sessionCookie)
	if res.Code != http.StatusSeeOther || c == nil {
		t.Fatalf("sign-in: %d, cookie %v", res.Code, c)
	}
	return []*http.Cookie{c}
}

func wantStatus(t *testing.T, r result, status int) {
	t.Helper()
	if r.Code != status {
		t.Fatalf("status %d, want %d; body: %s", r.Code, status, r.text())
	}
}

func wantRedirect(t *testing.T, r result, location string) {
	t.Helper()
	wantStatus(t, r, http.StatusSeeOther)
	if got := r.Header().Get("Location"); got != location {
		t.Fatalf("redirect to %q, want %q", got, location)
	}
}

// web-ui: Off by default.
func TestOffByDefault(t *testing.T) {
	tokens, _ := auth.Parse(strings.NewReader("a read " + auth.Hash("x") + "\n"))
	api := &httpapi.Server{Engine: &fakeEngine{}, Tokens: tokens, Limiter: auth.NewFailureLimiter(3, time.Minute)}
	w := httptest.NewRecorder()
	api.Handler().ServeHTTP(w, httptest.NewRequest("GET", "/login", nil))
	if w.Code != http.StatusNotFound || !strings.HasPrefix(w.Header().Get("Content-Type"), "application/problem+json") {
		t.Fatalf("got %d %s", w.Code, w.Header().Get("Content-Type"))
	}
}

// api-access: Session cookie is not an API credential.
func TestSessionIsNotAnAPICredential(t *testing.T) {
	s := newStack(t, true)
	cookies := s.signIn(t, writeTok)
	wantStatus(t, s.send("GET", "/api/v1/tasks/today", nil, cookies), http.StatusUnauthorized)
}

// A bearer token still works next to the UI, and the probes still answer.
func TestAPIUnchangedWithUI(t *testing.T) {
	s := newStack(t, true)
	wantStatus(t, s.send("GET", "/api/v1/tasks/today", nil, nil, "Authorization", "Bearer "+readTok), http.StatusOK)
	wantStatus(t, s.send("GET", "/livez", nil, nil), http.StatusOK)
	res := s.send("GET", "/api/v1/nope", nil, nil)
	if !strings.HasPrefix(res.Header().Get("Content-Type"), "application/problem+json") {
		t.Fatalf("unknown API path answered %s", res.Header().Get("Content-Type"))
	}
}

// web-ui: Valid token, Cookie attributes.
func TestSignIn(t *testing.T) {
	s := newStack(t, true)
	res := s.send("POST", "/login", url.Values{"token": {" " + writeTok + "\n"}}, nil)
	wantRedirect(t, res, "/")
	c := res.cookie(sessionCookie)
	if c == nil || strings.Contains(c.Value, writeTok) {
		t.Fatalf("cookie %+v", c)
	}
	if !c.HttpOnly || !c.Secure || c.SameSite != http.SameSiteStrictMode {
		t.Fatalf("cookie attributes %+v", c)
	}
	if !c.Expires.Equal(s.now.Add(30 * 24 * time.Hour)) {
		t.Fatalf("expires %v", c.Expires)
	}
	second := s.send("POST", "/login", url.Values{"token": {writeTok}}, nil).cookie(sessionCookie)
	if second.Value == c.Value {
		t.Fatal("two sign-ins share a session ID")
	}
}

// web-ui: Insecure cookie on request.
func TestInsecureCookie(t *testing.T) {
	s := newStack(t, false)
	if c := s.send("POST", "/login", url.Values{"token": {writeTok}}, nil).cookie(sessionCookie); c.Secure {
		t.Fatal("cookie is Secure")
	}
}

// web-ui: Invalid token, Too many failures.
func TestFailedSignIn(t *testing.T) {
	s := newStack(t, true)
	for range 3 {
		res := s.send("POST", "/login", url.Values{"token": {"wrong"}}, nil)
		wantStatus(t, res, http.StatusUnauthorized)
		if res.cookie(sessionCookie) != nil {
			t.Fatal("failed sign-in set a cookie")
		}
	}
	res := s.send("POST", "/login", url.Values{"token": {writeTok}}, nil)
	wantStatus(t, res, http.StatusTooManyRequests)
	if res.cookie(sessionCookie) != nil {
		t.Fatal("blocked sign-in set a cookie")
	}
}

// web-ui: Token in the query string.
func TestTokenInQuery(t *testing.T) {
	s := newStack(t, true)
	res := s.send("POST", "/login?token="+writeTok, url.Values{}, nil)
	wantStatus(t, res, http.StatusUnauthorized)
}

// web-ui: Token without read.
func TestTokenWithoutRead(t *testing.T) {
	s := newStack(t, true)
	res := s.send("POST", "/login", url.Values{"token": {noteTok}}, nil)
	wantStatus(t, res, http.StatusForbidden)
	if res.cookie(sessionCookie) != nil {
		t.Fatal("cookie set")
	}
}

// web-ui: No session, Expired session, Sign-out.
func TestSessionLifetime(t *testing.T) {
	s := newStack(t, true)
	wantRedirect(t, s.send("GET", "/", nil, nil), "/login")
	wantRedirect(t, s.send("POST", "/tasks", url.Values{"title": {"x"}, "key": {"k"}}, nil), "/login")

	cookies := s.signIn(t, writeTok)
	wantStatus(t, s.send("GET", "/", nil, cookies), http.StatusOK)
	s.now = s.now.Add(30*24*time.Hour - time.Second)
	wantStatus(t, s.send("GET", "/", nil, cookies), http.StatusOK)
	s.now = s.now.Add(time.Second)
	wantRedirect(t, s.send("GET", "/", nil, cookies), "/login")

	cookies = s.signIn(t, writeTok)
	wantRedirect(t, s.send("POST", "/logout", url.Values{}, cookies), "/login")
	wantRedirect(t, s.send("GET", "/", nil, cookies), "/login")
}

func TestSessionStoreBound(t *testing.T) {
	store := newSessions(time.Now)
	tok := &auth.Token{Name: "x"}
	for range maxSessions + 5 {
		if _, _, err := store.create(tok); err != nil {
			t.Fatal(err)
		}
	}
	if len(store.entries) != maxSessions {
		t.Fatalf("%d sessions kept", len(store.entries))
	}
}

// web-ui: Security headers.
func TestSecurityHeaders(t *testing.T) {
	s := newStack(t, true)
	for _, path := range []string{"/login", "/nope", "/static/style.css"} {
		res := s.send("GET", path, nil, nil)
		csp := res.Header().Get("Content-Security-Policy")
		if !strings.Contains(csp, "default-src 'none'") || !strings.Contains(csp, "frame-ancestors 'none'") || strings.Contains(csp, "script-src") {
			t.Errorf("%s: CSP %q", path, csp)
		}
		if res.Header().Get("Referrer-Policy") != "no-referrer" || res.Header().Get("Cache-Control") != "no-store" {
			t.Errorf("%s: headers %v", path, res.Header())
		}
	}
	if res := s.send("GET", "/login", nil, nil); strings.Contains(res.text(), "<script") {
		t.Error("page has a script")
	}
}

// web-ui: Cross-origin form.
func TestCrossOriginForm(t *testing.T) {
	s := newStack(t, true)
	cookies := s.signIn(t, writeTok)
	res := s.send("POST", "/tasks", url.Values{"title": {"evil"}, "key": {"k"}}, cookies, "Sec-Fetch-Site", "cross-site")
	wantStatus(t, res, http.StatusForbidden)
	res = s.send("POST", "/tasks", url.Values{"title": {"evil"}, "key": {"k"}}, cookies, "Origin", "https://evil.example")
	wantStatus(t, res, http.StatusForbidden)
	if len(s.engine.created) != 0 {
		t.Fatal("task created")
	}
}

// web-ui: Markup in a title.
func TestMarkupEscaped(t *testing.T) {
	s := newStack(t, true)
	s.engine.tasks[plainID]["title"] = "<script>alert(1)</script>"
	res := s.send("GET", "/tasks", nil, s.signIn(t, writeTok))
	if strings.Contains(res.text(), "<script>") || !strings.Contains(res.text(), "&lt;script&gt;") {
		t.Fatal(res.text())
	}
}

// web-ui: Tabs.
func TestTabs(t *testing.T) {
	s := newStack(t, true)
	cookies := s.signIn(t, writeTok)
	today := s.send("GET", "/", nil, cookies).text()
	if !strings.Contains(today, "Monthly bill") || strings.Contains(today, "Plain task") {
		t.Fatal(today)
	}
	all := s.send("GET", "/tasks", nil, cookies).text()
	if !strings.Contains(all, "Monthly bill") || !strings.Contains(all, "Plain task") || !strings.Contains(all, "2026-10-25") {
		t.Fatal(all)
	}
	wantStatus(t, s.send("GET", "/waiting", nil, cookies), http.StatusOK)
}

// web-ui: Engine unavailable.
func TestEngineUnavailable(t *testing.T) {
	s := newStack(t, true)
	cookies := s.signIn(t, writeTok)
	s.engine.down = true
	res := s.send("GET", "/", nil, cookies)
	wantStatus(t, res, http.StatusServiceUnavailable)
	if !strings.Contains(res.text(), "unavailable") || strings.Contains(res.text(), "down") {
		t.Fatal(res.text())
	}
}

func actionForm(back string) url.Values {
	return url.Values{"expected_state": {"NEXT"}, "expected_version": {"v1"}, "back": {back}}
}

// web-ui: Complete a task.
func TestCompletePlain(t *testing.T) {
	s := newStack(t, true)
	cookies := s.signIn(t, writeTok)
	wantRedirect(t, s.send("POST", "/tasks/"+plainID+"/complete", actionForm("/tasks"), cookies), "/tasks")
	if len(s.engine.transitions) != 1 || s.engine.transitions[0]["action"] != "complete" {
		t.Fatalf("%v", s.engine.transitions)
	}
}

// web-ui: Repeating task asks first, Confirmed.
func TestRepeatingAsksFirst(t *testing.T) {
	s := newStack(t, true)
	cookies := s.signIn(t, writeTok)
	for _, action := range []string{"complete", "skip"} {
		res := s.send("POST", "/tasks/"+repeatID+"/"+action, actionForm("/"), cookies)
		wantStatus(t, res, http.StatusSeeOther)
		if len(s.engine.transitions) != 0 {
			t.Fatal("changed without confirmation")
		}
		page := s.send("GET", res.Header().Get("Location"), nil, cookies)
		wantStatus(t, page, http.StatusOK)
		if !strings.Contains(page.text(), "Monthly bill") || !strings.Contains(page.text(), `name="confirmed"`) {
			t.Fatal(page.text())
		}
	}
	form := actionForm("/")
	form.Set("confirmed", "1")
	wantRedirect(t, s.send("POST", "/tasks/"+repeatID+"/complete", form, cookies), "/")
	if len(s.engine.transitions) != 1 {
		t.Fatal("not applied")
	}
	// Starting changes no dates, so it does not ask.
	wantRedirect(t, s.send("POST", "/tasks/"+repeatID+"/start", actionForm("/"), cookies), "/")
}

// web-ui: Changed in the meantime.
func TestConflict(t *testing.T) {
	s := newStack(t, true)
	cookies := s.signIn(t, writeTok)
	form := actionForm("/tasks")
	form.Set("expected_version", "old")
	res := s.send("POST", "/tasks/"+plainID+"/complete", form, cookies)
	wantRedirect(t, res, "/tasks?notice=conflict")
	if len(s.engine.transitions) != 0 {
		t.Fatal("changed")
	}
	if page := s.send("GET", "/tasks?notice=conflict", nil, cookies); !strings.Contains(page.text(), "changed in the meantime") {
		t.Fatal(page.text())
	}
	// The query selects a known notice; other text never reaches the page.
	if page := s.send("GET", "/tasks?notice=<b>hi", nil, cookies); strings.Contains(page.text(), "hi") && strings.Contains(page.text(), "<b>") {
		t.Fatal(page.text())
	}
}

// web-ui: Read-only session.
func TestReadOnlySession(t *testing.T) {
	s := newStack(t, true)
	cookies := s.signIn(t, readTok)
	page := s.send("GET", "/tasks", nil, cookies).text()
	if strings.Contains(page, `action="/tasks"`) || strings.Contains(page, "formaction") {
		t.Fatal(page)
	}
	wantRedirect(t, s.send("POST", "/tasks/"+plainID+"/start", actionForm("/tasks"), cookies), "/tasks?notice=forbidden")
	wantRedirect(t, s.send("POST", "/tasks", url.Values{"title": {"x"}, "key": {"k"}}, cookies), "/?notice=forbidden")
	if len(s.engine.transitions) != 0 || len(s.engine.created) != 0 {
		t.Fatal("changed")
	}
}

// web-ui: Open redirect refused.
func TestOpenRedirect(t *testing.T) {
	s := newStack(t, true)
	cookies := s.signIn(t, writeTok)
	wantRedirect(t, s.send("POST", "/tasks/"+plainID+"/start", actionForm("https://example.org/"), cookies), "/")
	wantRedirect(t, s.send("POST", "/language", url.Values{"lang": {"en"}, "back": {"//example.org"}}, nil), "/")
}

// web-ui: Add, Double submit, Rejected input.
func TestQuickAdd(t *testing.T) {
	s := newStack(t, true)
	cookies := s.signIn(t, writeTok)
	form := url.Values{"title": {"Buy milk"}, "deadline": {"2026-11-01"}, "key": {"form-1"}, "back": {"/tasks"}}
	wantRedirect(t, s.send("POST", "/tasks", form, cookies), "/tasks")
	wantRedirect(t, s.send("POST", "/tasks", form, cookies), "/tasks")
	if len(s.engine.created) != 1 {
		t.Fatalf("created %d", len(s.engine.created))
	}
	got := s.engine.created[0]
	if got["state"] != "TODO" || got["deadline"].(map[string]any)["date"] != "2026-11-01" {
		t.Fatalf("%v", got)
	}
	wantRedirect(t, s.send("POST", "/tasks", url.Values{"title": {"  "}, "key": {"form-2"}}, cookies), "/?notice=invalid")
	if len(s.engine.created) != 1 {
		t.Fatal("empty title created")
	}
}

// A second tap while the first is still in progress is not an error to report.
func TestDoubleTapInProgress(t *testing.T) {
	if got := addNotice(&client.APIError{Status: http.StatusConflict}); got != "" {
		t.Fatalf("notice %q", got)
	}
	if got := addNotice(&client.APIError{Status: http.StatusUnprocessableEntity}); got != "invalid" {
		t.Fatalf("notice %q", got)
	}
}

// Only the stylesheet is served, not a listing of the directory.
func TestStaticFiles(t *testing.T) {
	s := newStack(t, true)
	wantStatus(t, s.send("GET", "/static/style.css", nil, nil), http.StatusOK)
	wantStatus(t, s.send("GET", "/static/", nil, nil), http.StatusNotFound)
}

// web-ui: Initial language, Unsupported language, Switch wins, Invalid language.
func TestLanguages(t *testing.T) {
	s := newStack(t, true)
	ko := s.send("GET", "/login", nil, nil, "Accept-Language", "ko-KR,ko;q=0.9,en;q=0.8").text()
	if !strings.Contains(ko, `lang="ko"`) || !strings.Contains(ko, "로그인") {
		t.Fatal(ko)
	}
	fr := s.send("GET", "/login", nil, nil, "Accept-Language", "fr").text()
	if !strings.Contains(fr, `<html lang="en"`) || !strings.Contains(fr, "Sign in") {
		t.Fatal(fr)
	}
	if got := acceptedLang("en;q=0.5, ko;q=0.8"); got != "ko" {
		t.Fatalf("q order: %s", got)
	}
	if got := acceptedLang("ko;q=0, en"); got != "en" {
		t.Fatalf("q=0: %s", got)
	}

	res := s.send("POST", "/language", url.Values{"lang": {"en"}, "back": {"/login"}}, nil)
	wantRedirect(t, res, "/login")
	c := res.cookie(langCookie)
	if c == nil {
		t.Fatal("no language cookie")
	}
	switched := s.send("GET", "/login", nil, []*http.Cookie{c}, "Accept-Language", "ko").text()
	if !strings.Contains(switched, `<html lang="en"`) {
		t.Fatal(switched)
	}
	if res := s.send("POST", "/language", url.Values{"lang": {"xx"}}, nil); res.cookie(langCookie) != nil {
		t.Fatal("invalid language stored")
	}
	// A forged cookie falls back rather than selecting a missing catalog.
	bad := s.send("GET", "/login", nil, []*http.Cookie{{Name: langCookie, Value: "xx"}}).text()
	if !strings.Contains(bad, `<html lang="en"`) {
		t.Fatal(bad)
	}
}

// Both languages have the same keys, and everything a page can ask for exists.
func TestCatalog(t *testing.T) {
	for key := range catalog["en"] {
		if _, ok := catalog["ko"][key]; !ok {
			t.Errorf("ko lacks %s", key)
		}
	}
	for key := range catalog["ko"] {
		if _, ok := catalog["en"][key]; !ok {
			t.Errorf("en lacks %s", key)
		}
	}
	keys := []string{
		"login.invalid", "login.noread", "login.blocked", "confirm.complete", "confirm.skip",
		"error.unavailable", "error.forbidden", "error.notfound", "error.internal",
		"kind.scheduled", "kind.past-scheduled", "kind.deadline", "kind.upcoming-deadline", "kind.event",
	}
	for action := range webActions {
		keys = append(keys, "action."+action)
	}
	for notice := range notices {
		keys = append(keys, "notice."+notice)
	}
	files, _ := fs.Glob(templateFiles, "templates/*.html")
	literal := regexp.MustCompile(`t \$?\.Lang "([^"]+)"`)
	for _, file := range files {
		src, _ := templateFiles.ReadFile(file)
		for _, m := range literal.FindAllStringSubmatch(string(src), -1) {
			keys = append(keys, m[1])
		}
	}
	for _, key := range keys {
		if _, ok := catalog["en"][key]; !ok {
			t.Errorf("no text for %s", key)
		}
	}
}
