package cli

// CLI node commands (spec: cli) against a fake API.

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

const (
	nodeA = "a1b20000-1111-4111-8111-000000000001"
	nodeB = "a1c30000-1111-4111-8111-000000000002"
)

func node(id, title string) map[string]any {
	return map[string]any{"id": id, "title": title, "aliases": []string{"init.el"}, "tags": []string{"emacs"},
		"body": "See [[id:" + nodeB + "][keys]]", "location_hint": "knowledge/x.org"}
}

// newNodeAPI answers the node endpoints and records every request.
func newNodeAPI(t *testing.T) *fakeAPI {
	f := &fakeAPI{}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var body map[string]any
		json.Unmarshal(raw, &body)
		f.mu.Lock()
		f.requests = append(f.requests, request{r.Method, r.URL.Path, r.URL.RawQuery, body})
		f.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		send := func(status int, v any) { w.WriteHeader(status); json.NewEncoder(w).Encode(v) }
		switch {
		case r.Method == "POST" && r.URL.Path == "/api/v1/nodes":
			send(201, node(nodeA, body["title"].(string)))
		case r.Method == "GET" && r.URL.Path == "/api/v1/nodes":
			send(200, map[string]any{"items": []any{node(nodeA, "Emacs 설정 노트"), node(nodeB, "Key bindings")}})
		case r.Method == "GET" && strings.HasSuffix(r.URL.Path, "/backlinks"):
			send(200, map[string]any{"items": []any{
				map[string]any{"id": nodeB, "title": "Key bindings", "kind": "node"},
				map[string]any{"id": idA, "title": "Read the notes", "kind": "task"},
			}})
		case r.Method == "GET" && strings.HasPrefix(r.URL.Path, "/api/v1/nodes/"):
			send(200, node(strings.TrimPrefix(r.URL.Path, "/api/v1/nodes/"), "Emacs 설정 노트"))
		default:
			w.WriteHeader(404)
		}
	}))
	t.Cleanup(f.srv.Close)
	return f
}

// cli: Add a note
func TestAddNote(t *testing.T) {
	f := newNodeAPI(t)
	code, out, errOut := run(t, f, "node", "add", "Emacs 설정 노트", "--tag", "emacs", "--alias", "init.el",
		"--body", "See [[id:"+nodeB+"][keys]]")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	w := f.writes()
	want := map[string]any{"title": "Emacs 설정 노트", "tags": []any{"emacs"}, "aliases": []any{"init.el"},
		"body": "See [[id:" + nodeB + "][keys]]"}
	if len(w) != 1 || w[0].Path != "/api/v1/nodes" || !reflect.DeepEqual(w[0].Body, want) {
		t.Fatalf("requests = %+v", f.requests)
	}
	if !strings.HasPrefix(out, "a1b20000  Emacs 설정 노트  :emacs:  (aka init.el)\n") {
		t.Fatalf("output:\n%s", out)
	}
}

// cli: Backlinks of a note
func TestNodeBacklinks(t *testing.T) {
	f := newNodeAPI(t)
	code, out, errOut := run(t, f, "node", "backlinks", nodeA)
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	if out != "node  a1c30000  Key bindings\ntask  3f2a1111  Read the notes\n" {
		t.Fatalf("output:\n%s", out)
	}
	if f.requests[0].Path != "/api/v1/nodes/"+nodeA+"/backlinks" {
		t.Fatalf("requests = %+v", f.requests)
	}
}

// cli: Node prefix
func TestNodePrefix(t *testing.T) {
	f := newNodeAPI(t)
	code, out, errOut := run(t, f, "node", "show", "a1b2")
	if code != 0 || !strings.Contains(out, "id       "+nodeA) {
		t.Fatalf("exit %d: %s%s", code, out, errOut)
	}
	if f.requests[0].Path != "/api/v1/nodes" || f.requests[1].Path != "/api/v1/nodes/"+nodeA {
		t.Fatalf("requests = %+v", f.requests)
	}
	// A prefix must have 4 characters and match a node.
	if code, _, _ := run(t, f, "node", "links", "a1"); code == 0 {
		t.Fatal("a 2-character prefix was accepted")
	}
	if code, _, errOut := run(t, f, "node", "show", "ffff"); code == 0 || !strings.Contains(errOut, "no node ID starts with ffff") {
		t.Fatalf("unknown prefix: exit %d: %s", code, errOut)
	}
}

func TestNodeSearchAndJSON(t *testing.T) {
	f := newNodeAPI(t)
	code, out, _ := run(t, f, "node", "search", "설정", "--tag", "emacs", "--json")
	if code != 0 || f.requests[0].Query != "q=%EC%84%A4%EC%A0%95&tag=emacs" {
		t.Fatalf("exit %d, requests %+v", code, f.requests)
	}
	var list map[string]any
	if err := json.Unmarshal([]byte(out), &list); err != nil || len(list["items"].([]any)) != 2 {
		t.Fatalf("--json output is not the API body: %s", out)
	}
	if code, out, _ := run(t, f, "node", "search"); code != 0 || f.requests[1].Query != "" || !strings.Contains(out, "Key bindings") {
		t.Fatalf("search without text: exit %d, %s", code, out)
	}
}
