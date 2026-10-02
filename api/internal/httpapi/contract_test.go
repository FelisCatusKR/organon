package httpapi

import (
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

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
