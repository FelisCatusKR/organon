package httpapi

// Unit tests for the knowledge node endpoints (specs: knowledge-nodes,
// api-access) against a fake engine.

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/FelisCatusKR/organon/api/internal/rpc"
)

const (
	nodeID        = "33333333-3333-4333-8333-000000000001"
	missingNodeID = "99999999-9999-4999-8999-000000000404"
)

func sampleNode() map[string]any {
	return map[string]any{
		"id": nodeID, "title": "Emacs 설정 노트", "aliases": []string{"init.el"}, "tags": []string{"emacs"},
		"body": "Notes.", "location_hint": "knowledge/20261002142900-emacs_설정_노트.org",
	}
}

// withNodesHandler adds fake answers for the node methods.
func withNodesHandler(t *testing.T) (*fakeEngine, http.Handler) {
	engine, h := newTestServer(t)
	notFound := func(p map[string]any) error {
		if p["id"] == missingNodeID {
			return &rpc.Error{Code: rpc.CodeNotFound, Message: "no node with id " + missingNodeID}
		}
		return nil
	}
	engine.answers["node.create"] = func(p map[string]any) (any, error) {
		if strings.Contains(p["title"].(string), "\n") {
			return nil, &rpc.Error{Code: rpc.CodeInvalid, Message: "title must be a single line"}
		}
		return sampleNode(), nil
	}
	engine.answers["node.get"] = func(p map[string]any) (any, error) {
		if err := notFound(p); err != nil {
			return nil, err
		}
		return sampleNode(), nil
	}
	engine.answers["nodes.search"] = func(map[string]any) (any, error) {
		node := sampleNode()
		delete(node, "body")
		bare := map[string]any{"id": missingNodeID, "title": "Bare", "aliases": nil, "tags": nil, "location_hint": "knowledge/bare.org"}
		return []any{node, bare}, nil
	}
	refs := func(p map[string]any) (any, error) {
		if err := notFound(p); err != nil {
			return nil, err
		}
		return []any{
			map[string]any{"id": nodeID, "title": "Key bindings", "kind": "node"},
			map[string]any{"id": taskID, "title": "Read the notes", "kind": "task"},
		}, nil
	}
	engine.answers["node.backlinks"] = refs
	engine.answers["node.links"] = refs
	return engine, h
}

// knowledge-nodes: Node is persisted as an Org file (API side: status, Location)
func TestCreateNode(t *testing.T) {
	engine, h := withNodesHandler(t)
	res := do(t, h, "POST", "/api/v1/nodes", noteToken, `{"title":"Emacs 설정 노트","tags":["emacs"],"aliases":["init.el"],"body":"Notes."}`)
	if res.status != 201 || res.header.Get("Location") != "/api/v1/nodes/"+nodeID || res.body["id"] != nodeID {
		t.Fatalf("got %d %v %s", res.status, res.header, res.raw)
	}
	p := engine.params[len(engine.params)-1]
	if p["title"] != "Emacs 설정 노트" || p["body"] != "Notes." || len(p["tags"].([]any)) != 1 || len(p["aliases"].([]any)) != 1 {
		t.Fatalf("engine params = %v", p)
	}
}

// knowledge-nodes: Retried creation (API side)
func TestCreateNodeIsIdempotent(t *testing.T) {
	engine, h := withNodesHandler(t)
	body := `{"title":"Once"}`
	first := do(t, h, "POST", "/api/v1/nodes", noteToken, body, "Idempotency-Key", "k1")
	second := do(t, h, "POST", "/api/v1/nodes", noteToken, body, "Idempotency-Key", "k1")
	if first.status != 201 || second.status != 201 || first.raw != second.raw {
		t.Fatalf("first %d %s, second %d %s", first.status, first.raw, second.status, second.raw)
	}
	if second.header.Get("Idempotent-Replayed") != "true" || second.header.Get("Location") != "/api/v1/nodes/"+nodeID {
		t.Fatalf("replay headers = %v", second.header)
	}
	if engine.count() != 1 {
		t.Fatalf("engine called %d times", engine.count())
	}
	if res := do(t, h, "POST", "/api/v1/nodes", noteToken, `{"title":"Other"}`, "Idempotency-Key", "k1"); res.status != 422 {
		t.Fatalf("reused key with another body: %d", res.status)
	}
}

func TestCreateNodeRejectsBadBodies(t *testing.T) {
	_, h := withNodesHandler(t)
	for _, body := range []string{
		`{"title":"x","file":"../../etc/passwd"}`, // unknown field
		`{"title":"two\nlines"}`,                  // the engine says invalid
		`not json`,
	} {
		if res := do(t, h, "POST", "/api/v1/nodes", noteToken, body); res.status != 422 {
			t.Fatalf("%s: got %d %s", body, res.status, res.raw)
		}
	}
}

// knowledge-nodes: Read a created node; Unknown node
func TestGetNode(t *testing.T) {
	_, h := withNodesHandler(t)
	if res := do(t, h, "GET", "/api/v1/nodes/"+nodeID, readToken, ""); res.status != 200 || res.body["title"] != "Emacs 설정 노트" {
		t.Fatalf("got %d %s", res.status, res.raw)
	}
	if res := do(t, h, "GET", "/api/v1/nodes/"+missingNodeID, readToken, ""); res.status != 404 || res.body["code"] != "not_found" {
		t.Fatalf("missing node: %d %s", res.status, res.raw)
	}
}

// api-access: No code execution or file addressing through the API (node paths)
func TestNodePathsRequireUUIDs(t *testing.T) {
	engine, h := withNodesHandler(t)
	for _, target := range []string{
		"/api/v1/nodes/..%2F..%2Fetc%2Fpasswd",
		"/api/v1/nodes/ABCDEF00-0000-4000-8000-000000000001",
		"/api/v1/nodes/x/backlinks",
		"/api/v1/nodes/x/links",
	} {
		if res := do(t, h, "GET", target, readToken, ""); res.status != 422 {
			t.Fatalf("%s: got %d %s", target, res.status, res.raw)
		}
	}
	if engine.count() != 0 {
		t.Fatalf("engine was contacted %d times", engine.count())
	}
}

// knowledge-nodes: Search nodes; valid filters reach the engine unchanged
func TestSearchNodesPassesFilters(t *testing.T) {
	engine, h := withNodesHandler(t)
	res := do(t, h, "GET", "/api/v1/nodes?q=INIT.EL%20%EC%84%A4%EC%A0%95&tag=emacs", readToken, "")
	if res.status != 200 {
		t.Fatalf("got %d %s", res.status, res.raw)
	}
	p := engine.params[0]
	if p["q"] != "INIT.EL 설정" || p["tag"] != "emacs" || len(p) != 2 {
		t.Fatalf("engine params = %v", p)
	}
	items := res.body["items"].([]any)
	bare := items[1].(map[string]any)
	if len(items) != 2 || bare["aliases"] == nil || bare["tags"] == nil {
		t.Fatalf("items = %v (empty lists must be [])", items)
	}
	if res := do(t, h, "GET", "/api/v1/nodes", readToken, ""); res.status != 200 || len(engine.params[1]) != 0 {
		t.Fatalf("no filters: %d, params %v", res.status, engine.params[1])
	}
}

// knowledge-nodes: Invalid search
func TestSearchNodesRejectsBadFilters(t *testing.T) {
	engine, h := withNodesHandler(t)
	for _, query := range []string{
		"q=" + strings.Repeat("%EC%84%A4", 201), // 201 characters
		"q=a%09b",                               // tab
		"q=a%7Fb",                               // DEL
		"tag=a%20b",
		"tag=..%2Fx",
	} {
		if res := do(t, h, "GET", "/api/v1/nodes?"+query, readToken, ""); res.status != 422 {
			t.Fatalf("%s: got %d %s", query, res.status, res.raw)
		}
	}
	if res := do(t, h, "GET", "/api/v1/nodes?q="+strings.Repeat("%EC%84%A4", 200), readToken, ""); res.status != 200 {
		t.Fatalf("200 characters: got %d %s", res.status, res.raw)
	}
	if engine.count() != 1 {
		t.Fatalf("engine was contacted %d times", engine.count())
	}
}

// knowledge-nodes: Backlinks from a note and a task; Links of a note (API side)
func TestNodeBacklinksAndLinks(t *testing.T) {
	engine, h := withNodesHandler(t)
	for _, kind := range []string{"backlinks", "links"} {
		res := do(t, h, "GET", "/api/v1/nodes/"+nodeID+"/"+kind, readToken, "")
		items, _ := res.body["items"].([]any)
		if res.status != 200 || len(items) != 2 || items[1].(map[string]any)["kind"] != "task" {
			t.Fatalf("%s: got %d %s", kind, res.status, res.raw)
		}
		if res := do(t, h, "GET", "/api/v1/nodes/"+missingNodeID+"/"+kind, readToken, ""); res.status != 404 {
			t.Fatalf("%s of a missing node: %d", kind, res.status)
		}
	}
	if engine.calls[0] != "node.backlinks" || engine.calls[2] != "node.links" {
		t.Fatalf("calls = %v", engine.calls)
	}
}

// api-access: Read-only token tries to create a node; Note-taking token cannot change tasks
func TestNodeScopes(t *testing.T) {
	engine, h := withNodesHandler(t)
	for _, c := range []struct{ token, target, body string }{
		{readToken, "/api/v1/nodes", `{"title":"x"}`},
		{writeToken, "/api/v1/nodes", `{"title":"x"}`},
		{noteToken, "/api/v1/tasks", `{"title":"x"}`},
		{noteToken, "/api/v1/projects", `{"title":"x"}`},
		{noteToken, "/api/v1/tasks/" + taskID + "/complete", `{"expected_state":"NEXT"}`},
	} {
		if res := do(t, h, "POST", c.target, c.token, c.body); res.status != 403 || res.body["code"] != "forbidden" {
			t.Fatalf("POST %s: got %d %s", c.target, res.status, res.raw)
		}
	}
	if engine.count() != 0 {
		t.Fatalf("engine was contacted %d times", engine.count())
	}
	if res := do(t, h, "GET", "/api/v1/nodes/"+nodeID, noteToken, ""); res.status != 200 {
		t.Fatalf("note token reading: %d", res.status)
	}
}

// api-access: Responses match the schema (node endpoints)
func TestNodeResponsesMatchContract(t *testing.T) {
	v := newContractValidator(t)
	_, h := withNodesHandler(t)
	for _, c := range []struct {
		status                      int
		method, target, token, body string
	}{
		{201, "POST", "/api/v1/nodes", noteToken, `{"title":"Emacs 설정 노트","tags":["emacs"],"aliases":["init.el"]}`},
		{403, "POST", "/api/v1/nodes", readToken, `{"title":"x"}`},
		{422, "POST", "/api/v1/nodes", noteToken, `{"title":"a\nb"}`},
		{200, "GET", "/api/v1/nodes?q=emacs&tag=emacs", readToken, ""},
		{422, "GET", "/api/v1/nodes?tag=a%20b", readToken, ""},
		{200, "GET", "/api/v1/nodes/" + nodeID, readToken, ""},
		{404, "GET", "/api/v1/nodes/" + missingNodeID, readToken, ""},
		{200, "GET", "/api/v1/nodes/" + nodeID + "/backlinks", readToken, ""},
		{200, "GET", "/api/v1/nodes/" + nodeID + "/links", readToken, ""},
		{404, "GET", "/api/v1/nodes/" + missingNodeID + "/links", readToken, ""},
	} {
		req := httptest.NewRequest(c.method, c.target, strings.NewReader(c.body))
		if c.body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		req.Header.Set("Authorization", "Bearer "+c.token)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != c.status {
			t.Fatalf("%s %s: status %d, want %d: %s", c.method, c.target, rec.Code, c.status, rec.Body)
		}
		vreq := httptest.NewRequest(c.method, c.target, strings.NewReader(c.body))
		vreq.Header = req.Header
		validateExchange(t, v, vreq, rec)
	}
}
