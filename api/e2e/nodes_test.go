//go:build e2e

package e2e

// Scenarios of openspec/specs (knowledge-nodes, knowledge-index, api-access,
// cli) against the real stack: S5 (node with a stable ID), S6 (backlinks) and
// S7 (rebuild from the files) of docs/architecture.md §15.

import (
	"net/url"
	"os"
	"reflect"
	"regexp"
	"strings"
	"testing"
)

var (
	notesToken = os.Getenv("ORGANON_E2E_NOTES_TOKEN") // read,nodes:write
	uuidRE     = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
)

func createNode(t *testing.T, body map[string]any) map[string]any {
	t.Helper()
	r := call(t, notesToken, "POST", "/api/v1/nodes", body)
	mustStatus(t, r, 201)
	return r.Body
}

func link(node map[string]any, label string) string {
	return "[[id:" + node["id"].(string) + "][" + label + "]]"
}

// refs returns the backlinks or links of a node as "kind id" strings.
func refs(t *testing.T, id, which string) []string {
	t.Helper()
	r := api(t, "GET", "/api/v1/nodes/"+id+"/"+which, nil)
	mustStatus(t, r, 200)
	var out []string
	for _, it := range items(r) {
		out = append(out, it["kind"].(string)+" "+it["id"].(string))
	}
	return out
}

func searchIDs(t *testing.T, query string) []string {
	t.Helper()
	r := api(t, "GET", "/api/v1/nodes?"+query, nil)
	mustStatus(t, r, 200)
	var ids []string
	for _, it := range items(r) {
		ids = append(ids, it["id"].(string))
	}
	return ids
}

// indexSnapshot is every answer the index gives: the node list, and the
// backlinks and links of each node.
func indexSnapshot(t *testing.T) map[string]string {
	t.Helper()
	list := api(t, "GET", "/api/v1/nodes", nil)
	mustStatus(t, list, 200)
	snap := map[string]string{"list": string(list.Raw)}
	for _, it := range items(list) {
		id := it["id"].(string)
		for _, which := range []string{"backlinks", "links"} {
			snap[which+" "+id] = string(api(t, "GET", "/api/v1/nodes/"+id+"/"+which, nil).Raw)
		}
	}
	return snap
}

// knowledge-nodes: Node is persisted as an Org file; Tags and aliases; Read a created node; Match on an alias
func TestNodesS5CreateReadSearch(t *testing.T) {
	r := call(t, notesToken, "POST", "/api/v1/nodes", map[string]any{
		"title": "Emacs 설정 노트 e2e", "tags": []string{"e2e", "emacs"}, "aliases": []string{"init.el e2e"},
		"body": "Notes about * my config."})
	mustStatus(t, r, 201)
	node := r.Body
	id := node["id"].(string)
	if !uuidRE.MatchString(id) || r.Body["title"] != "Emacs 설정 노트 e2e" {
		t.Fatalf("created: %s", r.Raw)
	}
	file := readData(t, "org/"+node["location_hint"].(string))
	if !strings.HasPrefix(node["location_hint"].(string), "knowledge/") ||
		!strings.Contains(file, ":ID:       "+id+"\n") || !strings.Contains(file, "#+title: Emacs 설정 노트 e2e\n") ||
		!strings.Contains(file, "#+filetags: :e2e:emacs:\n") {
		t.Fatalf("%s:\n%s", node["location_hint"], file)
	}

	got := api(t, "GET", "/api/v1/nodes/"+id, nil)
	mustStatus(t, got, 200)
	if string(got.Raw) != string(r.Raw) {
		t.Fatalf("GET differs from POST:\n%s\n%s", got.Raw, r.Raw)
	}
	if ids := searchIDs(t, "q="+url.QueryEscape("INIT.EL E2E")); !reflect.DeepEqual(ids, []string{id}) {
		t.Fatalf("search by alias: %v", ids)
	}
	if ids := searchIDs(t, "q="+url.QueryEscape("설정 노트 e2e")+"&tag=e2e"); !reflect.DeepEqual(ids, []string{id}) {
		t.Fatalf("search by title and tag: %v", ids)
	}
	mustStatus(t, api(t, "GET", "/api/v1/nodes?tag=a%20b", nil), 422)
	mustStatus(t, api(t, "GET", "/api/v1/nodes/99999999-9999-4999-8999-999999999999", nil), 404)
}

// knowledge-nodes: Backlinks from a note and a task; Links of a note
// knowledge-index: Task that links to a note
func TestNodesS6Backlinks(t *testing.T) {
	target := createNode(t, map[string]any{"title": "Backlink target e2e"})
	note := createNode(t, map[string]any{"title": "Linking note e2e",
		"body": "Twice: " + link(target, "one") + " and " + link(target, "two")})
	task := createTask(t, map[string]any{"title": "Task linking e2e", "body": "Read " + link(target, "it")})

	want := []string{"node " + note["id"].(string), "task " + task["id"].(string)}
	if got := refs(t, target["id"].(string), "backlinks"); !reflect.DeepEqual(got, want) {
		t.Fatalf("backlinks = %v, want %v", got, want)
	}
	if got := refs(t, note["id"].(string), "links"); !reflect.DeepEqual(got, []string{"node " + target["id"].(string)}) {
		t.Fatalf("links = %v", got)
	}
	// Tasks are not nodes.
	mustStatus(t, api(t, "GET", "/api/v1/nodes/"+task["id"].(string), nil), 404)
}

// knowledge-index: Note written by another process (here: a link appended to a node's file)
func TestNodesExternalEditIsPickedUp(t *testing.T) {
	target := createNode(t, map[string]any{"title": "External target e2e"})
	edited := createNode(t, map[string]any{"title": "Edited outside e2e"})
	ctl(t, "host-append", "org/"+edited["location_hint"].(string), "\nAdded outside: "+link(target, "x")+"\n")
	if got := refs(t, target["id"].(string), "backlinks"); !reflect.DeepEqual(got, []string{"node " + edited["id"].(string)}) {
		t.Fatalf("backlinks after an external edit = %v", got)
	}
}

// api-access: Read-only token tries to create a node; Note-taking token cannot change tasks
func TestNodesScopes(t *testing.T) {
	mustStatus(t, call(t, readToken, "POST", "/api/v1/nodes", map[string]any{"title": "nope"}), 403)
	mustStatus(t, call(t, notesToken, "POST", "/api/v1/tasks", map[string]any{"title": "nope"}), 403)
	mustStatus(t, call(t, notesToken, "GET", "/api/v1/tasks", nil), 200)
	if ids := searchIDs(t, "q=nope"); len(ids) != 0 {
		t.Fatalf("a forbidden request created something: %v", ids)
	}
}

// knowledge-index: Rebuild after the index is deleted; Corrupt index
func TestNodesS7RebuildFromFiles(t *testing.T) {
	before := indexSnapshot(t)
	if len(before) < 4 {
		t.Fatalf("expected nodes from earlier tests: %v", before)
	}
	for _, verb := range []string{"drop-index", "corrupt-index"} {
		ctl(t, verb)
		if after := indexSnapshot(t); !reflect.DeepEqual(before, after) {
			t.Fatalf("after %s:\nbefore %v\nafter  %v", verb, before, after)
		}
	}
}

// cli: Add a note; Backlinks of a note; Node prefix
func TestNodesCLISmoke(t *testing.T) {
	out := organon(t, "node", "add", "CLI note e2e", "--tag", "cli", "--alias", "cli alias")
	short := strings.Fields(out)[0] // SHORTID TITLE ...
	var id string
	for _, line := range strings.Split(out, "\n") {
		if f := strings.Fields(line); len(f) == 2 && f[0] == "id" {
			id = f[1]
		}
	}
	if !uuidRE.MatchString(id) || !strings.HasPrefix(id, short) {
		t.Fatalf("node add printed:\n%s", out)
	}
	if !strings.Contains(organon(t, "node", "search", "cli ALIAS"), short+"  CLI note e2e") {
		t.Fatal("new node not found by `organon node search`")
	}
	organon(t, "node", "add", "CLI linking note e2e", "--body", "See [[id:"+id+"][the CLI note]]")
	if backlinks := organon(t, "node", "backlinks", short); !strings.Contains(backlinks, "CLI linking note e2e") ||
		!strings.HasPrefix(backlinks, "node ") {
		t.Fatalf("node backlinks:\n%s", backlinks)
	}
	if !strings.Contains(organon(t, "node", "show", short, "--json"), `"title":"CLI note e2e"`) {
		t.Fatal("node show --json")
	}
}
