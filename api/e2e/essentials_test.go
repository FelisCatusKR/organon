//go:build e2e

package e2e

// Scenarios of openspec/changes/task-essentials against the real stack.
// go test runs files in name order, so these run after e2e_test.go.

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// 4.1: edit, reopen, list, projects ---------------------------------------------

func TestEssentialsEditTask(t *testing.T) {
	task := createTask(t, map[string]any{"title": "Postpone me", "priority": "A",
		"deadline": map[string]any{"date": "2026-10-25"}})
	r := api(t, "PATCH", "/api/v1/tasks/"+task["id"].(string), map[string]any{
		"expected_version": task["version"], "deadline": map[string]any{"date": "2026-10-30"}, "priority": nil})
	mustStatus(t, r, 200)
	if r.Body["deadline"].(map[string]any)["date"] != "2026-10-30" || r.Body["priority"] != nil || r.Body["title"] != "Postpone me" {
		t.Fatalf("after edit: %s", r.Raw)
	}
	inbox := readData(t, "org/tasks/inbox.org")
	if !strings.Contains(inbox, "* TODO Postpone me\nDEADLINE: <2026-10-30 Fri>") {
		t.Fatalf("inbox.org:\n%s", inbox)
	}
	// The version moved on: the same PATCH again is a conflict.
	mustStatus(t, api(t, "PATCH", "/api/v1/tasks/"+task["id"].(string), map[string]any{
		"expected_version": task["version"], "title": "x"}), 409)
}

func TestEssentialsReopen(t *testing.T) {
	task := createTask(t, map[string]any{"title": "Done by mistake", "state": "NEXT"})
	r := complete(t, task)
	mustStatus(t, r, 200)
	done := r.Body["task"].(map[string]any)
	r = api(t, "POST", "/api/v1/tasks/"+task["id"].(string)+"/todo", map[string]any{
		"expected_state": "DONE", "expected_version": done["version"]})
	mustStatus(t, r, 200)
	if reopened := r.Body["task"].(map[string]any); reopened["state"] != "TODO" || reopened["closed_at"] != nil {
		t.Fatalf("reopened: %s", r.Raw)
	}
}

func TestEssentialsListAndProjects(t *testing.T) {
	backlog := createTask(t, map[string]any{"title": "Undated backlog e2e", "tags": []string{"e2e"}})
	r := api(t, "POST", "/api/v1/projects", map[string]any{"title": "E2E project"})
	mustStatus(t, r, 201)
	project := r.Body
	child := createTask(t, map[string]any{"title": "Inside the project", "state": "NEXT", "project_id": project["id"]})

	open := items(api(t, "GET", "/api/v1/tasks", nil))
	if findByID(open, backlog["id"].(string)) == nil || findByID(open, child["id"].(string)) == nil {
		t.Fatal("open tasks missing from the default list")
	}
	next := items(api(t, "GET", "/api/v1/tasks?state=NEXT&project="+project["id"].(string), nil))
	if len(next) != 1 || next[0]["id"] != child["id"] {
		t.Fatalf("NEXT in project: %v", next)
	}
	tagged := items(api(t, "GET", "/api/v1/tasks?tag=e2e", nil))
	if len(tagged) != 1 || tagged[0]["id"] != backlog["id"] {
		t.Fatalf("tagged: %v", tagged)
	}
	mustStatus(t, api(t, "GET", "/api/v1/tasks?state=SOMEDAY", nil), 422)

	var found map[string]any
	for _, p := range items(api(t, "GET", "/api/v1/projects", nil)) {
		if p["id"] == project["id"] {
			found = p
		}
	}
	if found == nil || found["open_tasks"] != float64(1) {
		t.Fatalf("project list: %v", found)
	}
	if _, err := os.Stat(filepath.Join(dataDir, "org/projects/e2e-project.org")); err != nil {
		t.Fatalf("project file: %v", err)
	}
	// Read-only tokens cannot create projects or edit tasks.
	mustStatus(t, call(t, readToken, "POST", "/api/v1/projects", map[string]any{"title": "nope"}), 403)
	mustStatus(t, call(t, readToken, "PATCH", "/api/v1/tasks/"+backlog["id"].(string),
		map[string]any{"expected_version": backlog["version"], "title": "nope"}), 403)
}

// 4.2: CLI smoke test ------------------------------------------------------------

var (
	cliOnce sync.Once
	cliPath string
	cliErr  error
)

func organon(t *testing.T, args ...string) string {
	t.Helper()
	cliOnce.Do(func() {
		// A directory of its own, so concurrent runs do not share a binary.
		dir, err := os.MkdirTemp("", "organon-e2e-cli-")
		if err != nil {
			cliErr = err
			return
		}
		cliPath = filepath.Join(dir, "organon")
		out, err := exec.Command("go", "build", "-o", cliPath, "../cmd/organon").CombinedOutput()
		if err != nil {
			cliErr = err
			t.Logf("%s", out)
		}
	})
	if cliErr != nil {
		t.Fatalf("building the CLI: %v", cliErr)
	}
	cmd := exec.Command(cliPath, args...)
	cmd.Env = append(os.Environ(), "ORGANON_URL="+baseURL, "ORGANON_TOKEN="+token, "XDG_CONFIG_HOME="+t.TempDir())
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("organon %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return string(out)
}

func TestEssentialsCLISmoke(t *testing.T) {
	today := api(t, "GET", "/api/v1/meta", nil).Body["today"].(string) // read, not computed

	out := organon(t, "task", "add", "CLI smoke task", "--next", "--scheduled", today, "--tag", "cli")
	short := strings.Fields(out)[1] // STATE SHORTID ...
	if !strings.Contains(organon(t, "today"), "CLI smoke task") {
		t.Fatal("new task not in `organon today`")
	}
	if done := organon(t, "task", "done", short); !strings.HasPrefix(done, "DONE") {
		t.Fatalf("task done: %s", done)
	}
	if !strings.Contains(organon(t, "completed"), "CLI smoke task") {
		t.Fatal("not in `organon completed`")
	}

	organon(t, "project", "add", "CLI project")
	var prefix string
	for _, line := range strings.Split(organon(t, "project", "list"), "\n") {
		if strings.HasSuffix(line, "CLI project") {
			prefix = strings.Fields(line)[0]
		}
	}
	if prefix == "" {
		t.Fatal("project not listed")
	}
	if out := organon(t, "task", "add", "In the CLI project", "--project", prefix); !strings.Contains(out, "(CLI project)") {
		t.Fatalf("task add --project: %s", out)
	}
	if !strings.Contains(organon(t, "task", "list", "--project", prefix, "--json"), `"In the CLI project"`) {
		t.Fatal("task not listed under the project")
	}
}
