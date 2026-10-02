package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"

	"github.com/FelisCatusKR/organon/api/internal/rpc"
)

const projectID = "22222222-2222-4222-8222-000000000001"

func sampleProject() map[string]any {
	return map[string]any{"id": projectID, "title": "Household", "open_tasks": 2, "location_hint": "projects/household.org"}
}

// withEssentialsHandler adds fake answers for the task-essentials methods.
func withEssentialsHandler(t *testing.T) (*fakeEngine, http.Handler) {
	engine, h := newTestServer(t)
	engine.answers["task.update"] = func(p map[string]any) (any, error) {
		if p["expected_version"] != "abc" {
			return nil, &rpc.Error{Code: rpc.CodeConflict, Message: "changed",
				Data: map[string]any{"actual_state": "NEXT", "actual_version": "abc"}}
		}
		return sampleTask(taskID), nil
	}
	engine.answers["tasks.list"] = func(map[string]any) (any, error) { return []any{sampleTask(taskID)}, nil }
	engine.answers["projects.list"] = func(map[string]any) (any, error) { return []any{sampleProject()}, nil }
	engine.answers["project.create"] = func(map[string]any) (any, error) { return sampleProject(), nil }
	return engine, h
}

func withEssentials(t *testing.T) (*fakeEngine, func(method, target, token, body string) result) {
	engine, h := withEssentialsHandler(t)
	return engine, func(method, target, token, body string) result {
		return do(t, h, method, target, token, body)
	}
}

// task-lifecycle: Edit a task (API side: absent vs null)
func TestUpdateSplitsSetAndClear(t *testing.T) {
	engine, call := withEssentials(t)
	res := call("PATCH", "/api/v1/tasks/"+taskID, writeToken,
		`{"expected_version":"abc","title":"New","deadline":{"date":"2026-10-30"},"scheduled":null,"priority":null}`)
	if res.status != 200 || res.body["id"] != taskID {
		t.Fatalf("got %d %s", res.status, res.raw)
	}
	p := engine.params[0]
	set := p["set"].(map[string]any)
	if set["title"] != "New" || set["deadline"].(map[string]any)["date"] != "2026-10-30" || len(set) != 2 {
		t.Fatalf("set = %v", set)
	}
	var clear []string
	for _, c := range p["clear"].([]any) {
		clear = append(clear, c.(string))
	}
	sort.Strings(clear)
	if strings.Join(clear, ",") != "priority,scheduled" || p["expected_version"] != "abc" || p["id"] != taskID {
		t.Fatalf("params = %v", p)
	}
}

// task-lifecycle: Invalid edit (API side)
func TestUpdateRejectsBadBodies(t *testing.T) {
	engine, call := withEssentials(t)
	for _, body := range []string{
		`{"title":"x"}`,                                   // no expected_version
		`{"expected_version":""}`,                         // empty version
		`{"expected_version":"abc"}`,                      // nothing to change
		`{"expected_version":"abc","file":"/etc/passwd"}`, // unknown field
		`{"expected_version":"abc","title":null}`,         // title cannot be cleared
		`{"expected_version":"abc","tags":null}`,          // tags cannot be null
		`not json`,
	} {
		if res := call("PATCH", "/api/v1/tasks/"+taskID, writeToken, body); res.status != 422 {
			t.Fatalf("%s: got %d %s", body, res.status, res.raw)
		}
	}
	if engine.count() != 0 {
		t.Fatalf("engine was contacted: %v", engine.calls)
	}
}

// task-lifecycle: Stale version (mapped from the engine)
func TestUpdateStaleVersionIs409(t *testing.T) {
	_, call := withEssentials(t)
	res := call("PATCH", "/api/v1/tasks/"+taskID, writeToken, `{"expected_version":"old","title":"x"}`)
	if res.status != 409 || res.body["actual_version"] != "abc" {
		t.Fatalf("got %d %s", res.status, res.raw)
	}
}

func TestTodoAndNextActionsReachTheEngine(t *testing.T) {
	engine, call := withEssentials(t)
	for _, action := range []string{"todo", "next"} {
		if res := call("POST", "/api/v1/tasks/"+taskID+"/"+action, writeToken, `{"expected_state":"NEXT"}`); res.status != 200 {
			t.Fatalf("%s: got %d %s", action, res.status, res.raw)
		}
	}
	if engine.params[0]["action"] != "todo" || engine.params[1]["action"] != "next" {
		t.Fatalf("params = %v", engine.params)
	}
}

// task-listing: Invalid filter; valid filters reach the engine unchanged
func TestListTasksFilters(t *testing.T) {
	engine, call := withEssentials(t)
	for _, q := range []string{"state=SOMEDAY", "state=NEXT,", "project=..%2Fx", "project=ABC", "tag=a%20b"} {
		if res := call("GET", "/api/v1/tasks?"+q, readToken, ""); res.status != 422 {
			t.Fatalf("%s: got %d", q, res.status)
		}
	}
	if engine.count() != 0 {
		t.Fatal("engine was contacted for an invalid filter")
	}
	res := call("GET", "/api/v1/tasks?state=DONE,CANCELLED&project="+projectID+"&tag=bills", readToken, "")
	if res.status != 200 || len(res.body["items"].([]any)) != 1 {
		t.Fatalf("got %d %s", res.status, res.raw)
	}
	p := engine.params[0]
	raw, _ := json.Marshal(p)
	if string(raw) != `{"project":"`+projectID+`","states":["DONE","CANCELLED"],"tag":"bills"}` {
		t.Fatalf("params = %s", raw)
	}
	if res := call("GET", "/api/v1/tasks", readToken, ""); res.status != 200 || len(engine.params[1]) != 0 {
		t.Fatalf("no filter: %d, params %v", res.status, engine.params[1])
	}
}

// api-access: Read-only token tries to edit or create a project
func TestReadOnlyTokenCannotEditOrCreateProjects(t *testing.T) {
	engine, call := withEssentials(t)
	if res := call("PATCH", "/api/v1/tasks/"+taskID, readToken, `{"expected_version":"abc","title":"x"}`); res.status != 403 {
		t.Fatalf("patch: %d", res.status)
	}
	if res := call("POST", "/api/v1/projects", readToken, `{"title":"x"}`); res.status != 403 {
		t.Fatalf("create project: %d", res.status)
	}
	if res := call("GET", "/api/v1/projects", readToken, ""); res.status != 200 {
		t.Fatalf("list projects: %d", res.status)
	}
	if engine.count() != 1 {
		t.Fatalf("engine calls: %v", engine.calls)
	}
}

func TestCreateProject(t *testing.T) {
	_, call := withEssentials(t)
	if res := call("POST", "/api/v1/projects", writeToken, `{"title":"Household"}`); res.status != 201 || res.body["id"] != projectID {
		t.Fatalf("got %d %s", res.status, res.raw)
	}
	if res := call("POST", "/api/v1/projects", writeToken, `{"title":"x","file":"y"}`); res.status != 422 {
		t.Fatalf("unknown field: %d", res.status)
	}
}

// api-access: Responses match the schema (new endpoints)
func TestEssentialsResponsesMatchContract(t *testing.T) {
	v := newContractValidator(t)
	_, h := withEssentialsHandler(t)
	for _, c := range []struct{ method, target, token, body string }{
		{"GET", "/api/v1/tasks?state=NEXT", readToken, ""},
		{"GET", "/api/v1/tasks?state=SOMEDAY", readToken, ""},
		{"PATCH", "/api/v1/tasks/" + taskID, writeToken, `{"expected_version":"abc","priority":null,"deadline":{"date":"2026-10-30"}}`},
		{"PATCH", "/api/v1/tasks/" + taskID, writeToken, `{"expected_version":"old","title":"x"}`},
		{"POST", "/api/v1/tasks/" + taskID + "/todo", writeToken, `{"expected_state":"NEXT"}`},
		{"GET", "/api/v1/projects", readToken, ""},
		{"POST", "/api/v1/projects", writeToken, `{"title":"Household"}`},
		{"POST", "/api/v1/projects", readToken, `{"title":"Household"}`},
	} {
		req := httptest.NewRequest(c.method, c.target, strings.NewReader(c.body))
		if c.body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		req.Header.Set("Authorization", "Bearer "+c.token)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		vreq := httptest.NewRequest(c.method, c.target, strings.NewReader(c.body))
		vreq.Header = req.Header
		validateExchange(t, v, vreq, rec)
	}
}

// projects: Retried creation
func TestCreateProjectIsIdempotent(t *testing.T) {
	engine, h := withEssentialsHandler(t)
	a := do(t, h, "POST", "/api/v1/projects", writeToken, `{"title":"Household"}`, "Idempotency-Key", "p-1")
	b := do(t, h, "POST", "/api/v1/projects", writeToken, `{"title":"Household"}`, "Idempotency-Key", "p-1")
	if a.status != 201 || b.status != 201 || a.raw != b.raw || b.header.Get("Idempotent-Replayed") != "true" {
		t.Fatalf("a=%d %s b=%d %s", a.status, a.raw, b.status, b.raw)
	}
	if engine.count() != 1 {
		t.Fatalf("engine called %d times", engine.count())
	}
	// The same key on another endpoint is a different request.
	if c := do(t, h, "POST", "/api/v1/tasks", writeToken, `{"title":"x"}`, "Idempotency-Key", "p-1"); c.status != 201 {
		t.Fatalf("task with same key: %d %s", c.status, c.raw)
	}
}
