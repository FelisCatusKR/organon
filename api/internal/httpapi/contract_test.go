package httpapi

import (
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/FelisCatusKR/organon/api/internal/rpc"
	"github.com/pb33f/libopenapi"
	validator "github.com/pb33f/libopenapi-validator"
)

// newContractValidator loads api/openapi.yaml. Test-only dependency: the
// production binary uses the standard library only.
func newContractValidator(t *testing.T) validator.Validator {
	t.Helper()
	spec, err := os.ReadFile("../../openapi.yaml")
	if err != nil {
		t.Fatal(err)
	}
	doc, err := libopenapi.NewDocument(spec)
	if err != nil {
		t.Fatal(err)
	}
	v, errs := validator.NewValidator(doc)
	if len(errs) > 0 {
		t.Fatalf("building validator: %v", errs)
	}
	if ok, errs := v.ValidateDocument(); !ok {
		t.Fatalf("openapi.yaml is invalid: %v", errs)
	}
	return v
}

// ValidateResponse checks one recorded exchange against the contract. It is
// shared with the e2e suite through the same document.
func validateExchange(t *testing.T, v validator.Validator, req *http.Request, rec *httptest.ResponseRecorder) {
	t.Helper()
	resp := rec.Result()
	resp.Request = req
	if ok, errs := v.ValidateHttpResponse(req, resp); !ok {
		for _, e := range errs {
			t.Errorf("%s %s -> %d: %s (%s)", req.Method, req.URL.Path, rec.Code, e.Message, e.Reason)
			for _, se := range e.SchemaValidationErrors {
				t.Errorf("    %s: %s", se.FieldPath, se.Reason)
			}
		}
	}
}

// api-access: Responses match the schema (unit level, against a fake engine).
func TestResponsesMatchContract(t *testing.T) {
	v := newContractValidator(t)
	engine, h := newTestServer(t)
	engine.answers["tasks.today"] = func(map[string]any) (any, error) {
		task := sampleTask(taskID)
		task["agenda"] = map[string]any{"kind": "upcoming-deadline", "date": "2026-10-25"}
		return []any{task}, nil
	}
	engine.answers["agenda.day"] = func(map[string]any) (any, error) {
		return []any{
			map[string]any{"kind": "event", "date": "2026-10-02", "id": nil, "title": "Dinner", "task": nil},
			map[string]any{"kind": "deadline", "date": "2026-10-02", "id": taskID, "title": "x", "task": sampleTask(taskID)},
		}, nil
	}

	cases := []struct {
		method, target, token, body string
	}{
		{"GET", "/healthz", "", ""},
		{"GET", "/api/v1/meta", readToken, ""},
		{"GET", "/api/v1/meta", "", ""},
		{"GET", "/api/v1/agenda?date=2026-10-02", readToken, ""},
		{"GET", "/api/v1/tasks/today", readToken, ""},
		{"GET", "/api/v1/tasks/today?date=2026-02-30", readToken, ""},
		{"GET", "/api/v1/tasks/" + taskID, readToken, ""},
		{"POST", "/api/v1/tasks", writeToken, `{"title":"Pay","deadline":{"date":"2026-10-25","repeat":"+1m","warning_days":3}}`},
		{"POST", "/api/v1/tasks", readToken, `{"title":"Pay"}`},
		{"POST", "/api/v1/tasks/" + taskID + "/complete", writeToken, `{"expected_state":"NEXT","expected_version":"abc"}`},
		{"POST", "/api/v1/tasks/" + taskID + "/complete", writeToken, `{"expected_state":"DONE"}`},
	}
	for _, c := range cases {
		var body *strings.Reader
		if c.body != "" {
			body = strings.NewReader(c.body)
		} else {
			body = strings.NewReader("")
		}
		req := httptest.NewRequest(c.method, c.target, body)
		if c.body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		if c.token != "" {
			req.Header.Set("Authorization", "Bearer "+c.token)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		// Re-create the request for validation (the body was consumed).
		vreq := httptest.NewRequest(c.method, c.target, strings.NewReader(c.body))
		vreq.Header = req.Header
		validateExchange(t, v, vreq, rec)
	}
}

// api-access: Responses match the schema, for error and replay responses.
func TestErrorResponsesMatchContract(t *testing.T) {
	v := newContractValidator(t)
	engine, h := newTestServer(t)
	const missingID = "99999999-9999-4999-8999-999999999999"
	engine.answers["task.get"] = func(p map[string]any) (any, error) {
		if p["id"] == missingID {
			return nil, &rpc.Error{Code: rpc.CodeNotFound, Message: "no entry with id " + missingID}
		}
		return sampleTask(p["id"].(string)), nil
	}
	engine.answers["meta"] = func(map[string]any) (any, error) {
		return nil, &rpc.Error{Code: rpc.CodeUnavailable, Message: "Missing organon.json"}
	}

	exchange := func(method, target, token, body string, header ...string) (*http.Request, *httptest.ResponseRecorder) {
		req := httptest.NewRequest(method, target, strings.NewReader(body))
		if body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		for i := 0; i+1 < len(header); i += 2 {
			req.Header.Set(header[i], header[i+1])
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		vreq := httptest.NewRequest(method, target, strings.NewReader(body))
		vreq.Header = req.Header
		return vreq, rec
	}
	check := func(status int, method, target, token, body string, header ...string) {
		t.Helper()
		req, rec := exchange(method, target, token, body, header...)
		if rec.Code != status {
			t.Fatalf("%s %s: status %d, want %d: %s", req.Method, req.URL, rec.Code, status, rec.Body)
		}
		validateExchange(t, v, req, rec)
	}

	check(404, "GET", "/api/v1/tasks/"+missingID, readToken, "")
	check(503, "GET", "/api/v1/meta", readToken, "")
	check(422, "POST", "/api/v1/tasks", writeToken, `{"title":"`+strings.Repeat("x", maxBodyBytes)+`"}`)
	exchange("POST", "/api/v1/tasks", writeToken, `{"title":"Pay"}`, "Idempotency-Key", "contract-1")
	check(201, "POST", "/api/v1/tasks", writeToken, `{"title":"Pay"}`, "Idempotency-Key", "contract-1")
	for i := 0; i < 10; i++ {
		exchange("GET", "/api/v1/tasks/today", "wrong", "")
	}
	check(429, "GET", "/api/v1/tasks/today", "wrong", "")
}
