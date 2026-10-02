package httpapi

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/FelisCatusKR/organon/api/internal/auth"
	"github.com/FelisCatusKR/organon/api/internal/idem"
	"github.com/FelisCatusKR/organon/api/internal/rpc"
)

const (
	readToken  = "read-token"
	writeToken = "write-token"
	taskID     = "11111111-1111-4111-8111-000000000001"
)

// fakeEngine records calls and answers from a table of canned results.
type fakeEngine struct {
	mu      sync.Mutex
	calls   []string
	params  []map[string]any
	answers map[string]func(params map[string]any) (any, error)
}

func (f *fakeEngine) Call(_ context.Context, method string, params, out any) error {
	raw, _ := json.Marshal(params)
	var p map[string]any
	_ = json.Unmarshal(raw, &p)
	f.mu.Lock()
	f.calls = append(f.calls, method)
	f.params = append(f.params, p)
	answer := f.answers[method]
	f.mu.Unlock()
	if answer == nil {
		return &rpc.Error{Code: rpc.CodeInternal, Message: "no fake answer for " + method}
	}
	result, err := answer(p)
	if err != nil {
		return err
	}
	if out != nil {
		raw, _ := json.Marshal(result)
		return json.Unmarshal(raw, out)
	}
	return nil
}

func (f *fakeEngine) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

func sampleTask(id string) map[string]any {
	return map[string]any{
		"id": id, "title": "Spotify 가족 요금제 납부", "state": "NEXT", "priority": nil, "tags": []string{},
		"scheduled":       nil,
		"deadline":        map[string]any{"date": "2026-10-25", "time": nil, "repeat": "+1m", "warning_days": 3},
		"repeat_to_state": nil, "closed_at": nil, "project": nil, "body": "",
		"version": "abc", "location_hint": "tasks/inbox.org",
	}
}

func newTestServer(t *testing.T) (*fakeEngine, http.Handler) {
	t.Helper()
	engine := &fakeEngine{answers: map[string]func(map[string]any) (any, error){
		"ping": func(map[string]any) (any, error) { return map[string]string{"status": "ok"}, nil },
		"meta": func(map[string]any) (any, error) {
			return map[string]any{"calendar_tz": "Asia/Seoul", "today": "2026-10-02", "doing_limit": 3, "engine_version": "e"}, nil
		},
		"task.get": func(p map[string]any) (any, error) { return sampleTask(p["id"].(string)), nil },
		"task.create": func(p map[string]any) (any, error) {
			if strings.Contains(p["title"].(string), "\n") {
				return nil, &rpc.Error{Code: rpc.CodeInvalid, Message: "title must be a single line"}
			}
			return sampleTask(taskID), nil
		},
		"task.transition": func(p map[string]any) (any, error) {
			if p["expected_state"] != "NEXT" {
				return nil, &rpc.Error{Code: rpc.CodeConflict, Message: "expected state differs",
					Data: map[string]any{"actual_state": "NEXT", "actual_version": "abc"}}
			}
			return map[string]any{"task": sampleTask(taskID), "warnings": []string{}}, nil
		},
		"tasks.today": func(map[string]any) (any, error) { return []any{}, nil },
	}}
	tokens, err := auth.Parse(strings.NewReader(
		"reader read " + auth.Hash(readToken) + "\nwriter read,tasks:write " + auth.Hash(writeToken) + "\n"))
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{
		Engine:      engine,
		Tokens:      tokens,
		Limiter:     auth.NewFailureLimiter(10, time.Minute),
		Idempotency: idem.New(100, 24*time.Hour),
		Version:     "test",
	}
	return engine, s.Handler()
}

type result struct {
	status int
	header http.Header
	body   map[string]any
	raw    string
}

func do(t *testing.T, h http.Handler, method, target, token, body string, headers ...string) result {
	t.Helper()
	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, target, reader)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	for i := 0; i+1 < len(headers); i += 2 {
		req.Header.Set(headers[i], headers[i+1])
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	res := result{status: rec.Code, header: rec.Header(), raw: rec.Body.String()}
	_ = json.Unmarshal(rec.Body.Bytes(), &res.body)
	return res
}

// api-access: Missing token
func TestMissingTokenIsRejectedBeforeEngine(t *testing.T) {
	engine, h := newTestServer(t)
	res := do(t, h, "GET", "/api/v1/tasks/today", "", "")
	if res.status != 401 || res.body["code"] != "unauthorized" {
		t.Fatalf("got %d %s", res.status, res.raw)
	}
	if res.header.Get("Content-Type") != "application/problem+json" {
		t.Fatalf("content type %q", res.header.Get("Content-Type"))
	}
	if engine.count() != 0 {
		t.Fatal("engine was contacted")
	}
}

// api-access: Health without token
func TestHealthNeedsNoToken(t *testing.T) {
	engine, h := newTestServer(t)
	if res := do(t, h, "GET", "/healthz", "", ""); res.status != 200 || res.body["status"] != "ok" {
		t.Fatalf("got %d %s", res.status, res.raw)
	}
	engine.answers["ping"] = func(map[string]any) (any, error) {
		return nil, &rpc.Error{Code: rpc.CodeUnavailable, Message: "Missing organon.json"}
	}
	if res := do(t, h, "GET", "/healthz", "", ""); res.status != 503 || res.body["code"] != "engine_unavailable" {
		t.Fatalf("got %d %s", res.status, res.raw)
	}
}

// api-access: Read-only token tries to write
func TestReadOnlyTokenCannotWrite(t *testing.T) {
	engine, h := newTestServer(t)
	res := do(t, h, "POST", "/api/v1/tasks", readToken, `{"title":"x"}`)
	if res.status != 403 || res.body["code"] != "forbidden" {
		t.Fatalf("got %d %s", res.status, res.raw)
	}
	if engine.count() != 0 {
		t.Fatal("engine was contacted")
	}
}

// api-access: Brute force
func TestFailedAuthenticationIsRateLimited(t *testing.T) {
	_, h := newTestServer(t)
	for i := 0; i < 10; i++ {
		if res := do(t, h, "GET", "/api/v1/meta", "wrong", ""); res.status != 401 {
			t.Fatalf("attempt %d: %d", i, res.status)
		}
	}
	if res := do(t, h, "GET", "/api/v1/meta", "wrong", ""); res.status != 429 {
		t.Fatalf("11th attempt: %d", res.status)
	}
	// Even a valid token from that address is refused for the rest of the window.
	if res := do(t, h, "GET", "/api/v1/meta", readToken, ""); res.status != 429 {
		t.Fatalf("valid token while blocked: %d", res.status)
	}
}

func TestClientIPHeaderSeparatesClients(t *testing.T) {
	engine, _ := newTestServer(t)
	tokens, _ := auth.Parse(strings.NewReader("reader read " + auth.Hash(readToken) + "\n"))
	h := (&Server{Engine: engine, Tokens: tokens, Limiter: auth.NewFailureLimiter(1, time.Minute),
		Idempotency: idem.New(10, time.Hour), ClientIPHeader: "CF-Connecting-IP"}).Handler()
	do(t, h, "GET", "/api/v1/meta", "wrong", "", "CF-Connecting-IP", "203.0.113.1")
	if res := do(t, h, "GET", "/api/v1/meta", "wrong", "", "CF-Connecting-IP", "203.0.113.1"); res.status != 429 {
		t.Fatalf("same client: %d", res.status)
	}
	if res := do(t, h, "GET", "/api/v1/meta", readToken, "", "CF-Connecting-IP", "203.0.113.2"); res.status != 200 {
		t.Fatalf("other client: %d %s", res.status, res.raw)
	}
}

// api-access: Non-UUID identifier
func TestNonUUIDIdentifierIsRejectedBeforeEngine(t *testing.T) {
	engine, h := newTestServer(t)
	for _, target := range []string{
		"/api/v1/tasks/..%2F..%2Fetc%2Fpasswd",
		"/api/v1/tasks/ABCDEF00-1111-4111-8111-000000000001",
		"/api/v1/tasks/not-a-uuid",
	} {
		res := do(t, h, "GET", target, readToken, "")
		if res.status != 422 && res.status != 404 {
			t.Fatalf("%s: got %d %s", target, res.status, res.raw)
		}
	}
	if res := do(t, h, "GET", "/api/v1/tasks/..%2F..%2Fetc%2Fpasswd", readToken, ""); res.status != 422 {
		t.Fatalf("encoded traversal: got %d %s", res.status, res.raw)
	}
	if engine.count() != 0 {
		t.Fatalf("engine was contacted: %v", engine.calls)
	}
}

// agenda-queries: Invalid date
func TestInvalidDateIsRejected(t *testing.T) {
	engine, h := newTestServer(t)
	for _, date := range []string{"2026-02-30", "..%2F..%2Fetc", "2026-1-2", "20261002"} {
		if res := do(t, h, "GET", "/api/v1/tasks/today?date="+date, readToken, ""); res.status != 422 {
			t.Fatalf("%s: got %d", date, res.status)
		}
	}
	if engine.count() != 0 {
		t.Fatal("engine was contacted")
	}
	if res := do(t, h, "GET", "/api/v1/tasks/today?date=2026-10-02", readToken, ""); res.status != 200 {
		t.Fatalf("valid date: %d %s", res.status, res.raw)
	}
	if engine.params[0]["date"] != "2026-10-02" {
		t.Fatalf("date not forwarded: %v", engine.params[0])
	}
}

// api-access: Conflict error body
func TestConflictIsProblemWithActualState(t *testing.T) {
	_, h := newTestServer(t)
	res := do(t, h, "POST", "/api/v1/tasks/"+taskID+"/complete", writeToken, `{"expected_state":"DOING"}`)
	if res.status != 409 || res.body["code"] != "conflict" || res.body["actual_state"] != "NEXT" {
		t.Fatalf("got %d %s", res.status, res.raw)
	}
	if res.header.Get("Content-Type") != "application/problem+json" {
		t.Fatalf("content type %q", res.header.Get("Content-Type"))
	}
}

// task-lifecycle: Missing expected_state
func TestTransitionRequiresExpectedState(t *testing.T) {
	engine, h := newTestServer(t)
	for _, body := range []string{`{}`, `{"expected_state":""}`, ``} {
		if res := do(t, h, "POST", "/api/v1/tasks/"+taskID+"/complete", writeToken, body); res.status != 422 {
			t.Fatalf("%q: got %d", body, res.status)
		}
	}
	if engine.count() != 0 {
		t.Fatal("engine was contacted")
	}
	if res := do(t, h, "POST", "/api/v1/tasks/"+taskID+"/explode", writeToken, `{"expected_state":"NEXT"}`); res.status != 404 {
		t.Fatalf("unknown action: %d", res.status)
	}
	res := do(t, h, "POST", "/api/v1/tasks/"+taskID+"/complete", writeToken,
		`{"expected_state":"NEXT","expected_version":"abc"}`)
	if res.status != 200 || engine.params[0]["expected_version"] != "abc" || engine.params[0]["action"] != "complete" {
		t.Fatalf("got %d %s / %v", res.status, res.raw, engine.params)
	}
}

// task-lifecycle: Title with a newline (validated by the engine, mapped to 422)
func TestEngineValidationErrorIs422(t *testing.T) {
	_, h := newTestServer(t)
	res := do(t, h, "POST", "/api/v1/tasks", writeToken, `{"title":"two\nlines"}`)
	if res.status != 422 || res.body["code"] != "invalid" {
		t.Fatalf("got %d %s", res.status, res.raw)
	}
}

func TestUnknownFieldsAreRejected(t *testing.T) {
	engine, h := newTestServer(t)
	for _, body := range []string{`{"title":"x","file":"/etc/passwd"}`, `{"title":"x"} {}`, `[1]`, `not json`} {
		if res := do(t, h, "POST", "/api/v1/tasks", writeToken, body); res.status != 422 {
			t.Fatalf("%q: got %d", body, res.status)
		}
	}
	if engine.count() != 0 {
		t.Fatal("engine was contacted")
	}
}

func TestCreateReturns201WithLocation(t *testing.T) {
	_, h := newTestServer(t)
	res := do(t, h, "POST", "/api/v1/tasks", writeToken, `{"title":"Spotify 가족 요금제 납부"}`)
	if res.status != 201 || res.header.Get("Location") != "/api/v1/tasks/"+taskID || res.body["id"] != taskID {
		t.Fatalf("got %d %s", res.status, res.raw)
	}
	if tags, ok := res.body["tags"].([]any); !ok || len(tags) != 0 {
		t.Fatalf("tags should be [], got %#v", res.body["tags"])
	}
}

// task-lifecycle: Same key, same payload / Same key, different payload
func TestIdempotentCreate(t *testing.T) {
	engine, h := newTestServer(t)
	first := do(t, h, "POST", "/api/v1/tasks", writeToken, `{"title":"Pay rent"}`, "Idempotency-Key", "k-1")
	second := do(t, h, "POST", "/api/v1/tasks", writeToken, `{ "title" : "Pay rent" }`, "Idempotency-Key", "k-1")
	if first.status != 201 || second.status != 201 || first.raw != second.raw {
		t.Fatalf("first %d %s / second %d %s", first.status, first.raw, second.status, second.raw)
	}
	if second.header.Get("Idempotent-Replayed") != "true" {
		t.Fatal("replay not marked")
	}
	if engine.count() != 1 {
		t.Fatalf("engine called %d times", engine.count())
	}
	if res := do(t, h, "POST", "/api/v1/tasks", writeToken, `{"title":"Pay gas"}`, "Idempotency-Key", "k-1"); res.status != 422 {
		t.Fatalf("different payload: %d", res.status)
	}
	if engine.count() != 1 {
		t.Fatal("mismatched key reached the engine")
	}
}

func TestMetaAddsAPIVersion(t *testing.T) {
	_, h := newTestServer(t)
	res := do(t, h, "GET", "/api/v1/meta", readToken, "")
	if res.status != 200 || res.body["version"] != "test" || res.body["calendar_tz"] != "Asia/Seoul" {
		t.Fatalf("got %d %s", res.status, res.raw)
	}
}

func TestUnknownPathIsProblem(t *testing.T) {
	_, h := newTestServer(t)
	if res := do(t, h, "GET", "/api/v1/nope", readToken, ""); res.status != 404 || res.body["code"] != "not_found" {
		t.Fatalf("got %d %s", res.status, res.raw)
	}
}
