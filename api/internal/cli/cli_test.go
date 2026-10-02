package cli

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

const (
	idA = "3f2a1111-1111-4111-8111-000000000001"
	idB = "3f2a2222-1111-4111-8111-000000000002"
	idC = "9c001111-1111-4111-8111-000000000003"
	prj = "77770000-1111-4111-8111-000000000100"
)

type request struct {
	Method, Path, Query string
	Body                map[string]any
}

// fakeAPI answers like the Organon API and records every request.
type fakeAPI struct {
	mu       sync.Mutex
	requests []request
	srv      *httptest.Server
	repeat   bool // task idA repeats
}

func task(id, state, title string, repeat bool) map[string]any {
	deadline := map[string]any{"date": "2026-10-25", "time": nil, "repeat": nil, "warning_days": nil}
	if repeat {
		deadline["repeat"] = "+1m"
	}
	return map[string]any{"id": id, "title": title, "state": state, "priority": nil, "tags": []string{},
		"scheduled": nil, "deadline": deadline, "repeat_to_state": nil, "closed_at": nil, "project": nil,
		"body": "", "version": "v-" + id[:4], "location_hint": "tasks/inbox.org"}
}

func newFakeAPI(t *testing.T) *fakeAPI {
	f := &fakeAPI{}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var body map[string]any
		json.Unmarshal(raw, &body)
		f.mu.Lock()
		f.requests = append(f.requests, request{r.Method, r.URL.Path, r.URL.RawQuery, body})
		f.mu.Unlock()
		if r.Header.Get("Authorization") != "Bearer tok" {
			w.WriteHeader(401)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		send := func(status int, v any) { w.WriteHeader(status); json.NewEncoder(w).Encode(v) }
		all := []any{task(idA, "NEXT", "Spotify", f.repeat), task(idB, "DONE", "Old", false), task(idC, "TODO", "Other", false)}
		switch {
		case r.Method == "GET" && r.URL.Path == "/api/v1/tasks/today":
			w.Write([]byte(`{"items":[]}` + "\n"))
		case r.Method == "GET" && r.URL.Path == "/api/v1/tasks":
			send(200, map[string]any{"items": all})
		case r.Method == "GET" && strings.HasPrefix(r.URL.Path, "/api/v1/tasks/"):
			id := strings.TrimPrefix(r.URL.Path, "/api/v1/tasks/")
			send(200, task(id, "NEXT", "Spotify", f.repeat))
		case r.Method == "POST" && r.URL.Path == "/api/v1/tasks":
			send(201, task(idA, "NEXT", "Spotify", true))
		case r.Method == "POST" && strings.HasSuffix(r.URL.Path, "/complete"):
			if body["expected_version"] != "v-3f2a" {
				w.Header().Set("Content-Type", "application/problem+json")
				send(409, map[string]any{"code": "conflict", "detail": "the task changed since expected_version was read", "status": 409})
				return
			}
			next := task(idA, "NEXT", "Spotify", true)
			next["deadline"].(map[string]any)["date"] = "2026-11-25"
			send(200, map[string]any{"task": next, "warnings": []string{}})
		case r.Method == "PATCH":
			send(200, task(idA, "NEXT", "New", false))
		case r.Method == "GET" && r.URL.Path == "/api/v1/projects":
			send(200, map[string]any{"items": []any{map[string]any{"id": prj, "title": "Household", "open_tasks": 1, "location_hint": "p"}}})
		default:
			w.WriteHeader(404)
		}
	}))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeAPI) writes() []request {
	var out []request
	for _, r := range f.requests {
		if r.Method != "GET" {
			out = append(out, r)
		}
	}
	return out
}

func run(t *testing.T, f *fakeAPI, args ...string) (int, string, string) {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	if f != nil {
		t.Setenv("ORGANON_URL", f.srv.URL)
		t.Setenv("ORGANON_TOKEN", "tok")
	}
	var out, errb bytes.Buffer
	code := Run(args, &out, &errb)
	return code, out.String(), errb.String()
}

// ---- 3.2 configuration -------------------------------------------------------

func writeConfig(t *testing.T, dir string, mode os.FileMode, cfg string) {
	t.Helper()
	os.MkdirAll(filepath.Join(dir, "organon"), 0o700)
	if err := os.WriteFile(filepath.Join(dir, "organon", "client.json"), []byte(cfg), mode); err != nil {
		t.Fatal(err)
	}
	os.Chmod(filepath.Join(dir, "organon", "client.json"), mode)
}

// cli: Environment wins over the file
func TestEnvironmentWinsOverFile(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	writeConfig(t, dir, 0o600, `{"url":"https://from-file.example","token":"file-token"}`)
	t.Setenv("ORGANON_URL", "https://from-env.example")
	t.Setenv("ORGANON_TOKEN", "")
	cfg, err := LoadConfig()
	if err != nil || cfg.URL != "https://from-env.example" || cfg.Token != "file-token" {
		t.Fatalf("cfg=%+v err=%v", cfg, err)
	}
}

// cli: Missing configuration
func TestMissingConfiguration(t *testing.T) {
	t.Setenv("ORGANON_URL", "")
	t.Setenv("ORGANON_TOKEN", "")
	code, out, errs := run(t, nil, "today")
	if code == 0 || out != "" || !strings.Contains(errs, "ORGANON_URL") || !strings.Contains(errs, "client.json") {
		t.Fatalf("code=%d out=%q err=%q", code, out, errs)
	}
}

// cli: Config file readable by others
func TestConfigFileReadableByOthers(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("ORGANON_URL", "")
	t.Setenv("ORGANON_TOKEN", "")
	writeConfig(t, dir, 0o644, `{"url":"https://x.example","token":"secret-token"}`)
	_, err := LoadConfig()
	if err == nil || !strings.Contains(err.Error(), "chmod 600") || strings.Contains(err.Error(), "secret-token") {
		t.Fatalf("err=%v", err)
	}
	os.Chmod(filepath.Join(dir, "organon", "client.json"), 0o600)
	if cfg, err := LoadConfig(); err != nil || cfg.Token != "secret-token" {
		t.Fatalf("0600: cfg=%+v err=%v", cfg, err)
	}
}

// ---- 3.3 short IDs ---------------------------------------------------------------

// cli: Unique prefix (closed tasks are found too, so they can be reopened)
func TestUniquePrefix(t *testing.T) {
	f := newFakeAPI(t)
	code, out, errs := run(t, f, "task", "show", "9c00")
	if code != 0 || !strings.Contains(out, "Spotify") {
		t.Fatalf("code=%d out=%q err=%q", code, out, errs)
	}
	if f.requests[0].Query != "state=TODO%2CNEXT%2CDOING%2CWAITING%2CDONE%2CCANCELLED" || f.requests[1].Path != "/api/v1/tasks/"+idC {
		t.Fatalf("requests %+v", f.requests)
	}
}

// cli: Ambiguous prefix
func TestAmbiguousPrefix(t *testing.T) {
	f := newFakeAPI(t)
	code, _, errs := run(t, f, "task", "done", "3f2a")
	if code == 0 || !strings.Contains(errs, idA) || !strings.Contains(errs, idB) {
		t.Fatalf("code=%d err=%q", code, errs)
	}
	if len(f.writes()) != 0 {
		t.Fatal("something was changed")
	}
	for _, arg := range []string{"dead", "3f2"} {
		if code, _, _ := run(t, f, "task", "done", arg); code == 0 {
			t.Fatalf("%s accepted", arg)
		}
	}
	if len(f.writes()) != 0 {
		t.Fatal("something was changed")
	}
}

// ---- 3.4 commands and output -------------------------------------------------------

// cli: Add a monthly bill (exact request body)
func TestAddMonthlyBill(t *testing.T) {
	f := newFakeAPI(t)
	code, out, errs := run(t, f, "task", "add", "Spotify 가족 요금제 납부", "--next", "--deadline", "2026-10-25",
		"--repeat", "+1m", "--warn", "3", "--tag", "bills")
	if code != 0 || !strings.Contains(out, "3f2a1111") {
		t.Fatalf("code=%d out=%q err=%q", code, out, errs)
	}
	got, _ := json.Marshal(f.writes()[0].Body)
	want := `{"deadline":{"date":"2026-10-25","repeat":"+1m","warning_days":3},"state":"NEXT","tags":["bills"],"title":"Spotify 가족 요금제 납부"}`
	if string(got) != want {
		t.Fatalf("body\n got %s\nwant %s", got, want)
	}
}

func TestAddWithTimeAndProjectPrefix(t *testing.T) {
	f := newFakeAPI(t)
	if code, _, errs := run(t, f, "task", "add", "Call", "--scheduled", "2026-10-05 15:00", "--project", "7777"); code != 0 {
		t.Fatal(errs)
	}
	body := f.writes()[0].Body
	if body["project_id"] != prj || body["scheduled"].(map[string]any)["time"] != "15:00" {
		t.Fatalf("body %v", body)
	}
	if code, _, _ := run(t, f, "task", "add", "x", "--repeat", "+1m"); code == 0 {
		t.Fatal("--repeat without a date accepted")
	}
}

// cli: Complete a repeating task (read, then transition with what was read)
func TestCompleteRepeatingTask(t *testing.T) {
	f := newFakeAPI(t)
	f.repeat = true
	code, out, errs := run(t, f, "task", "done", idA)
	if code != 0 || !strings.Contains(out, "2026-11-25 +1m") {
		t.Fatalf("code=%d out=%q err=%q", code, out, errs)
	}
	w := f.writes()
	if len(w) != 1 || w[0].Path != "/api/v1/tasks/"+idA+"/complete" ||
		w[0].Body["expected_state"] != "NEXT" || w[0].Body["expected_version"] != "v-3f2a" {
		t.Fatalf("writes %+v", w)
	}
	if f.requests[0].Method != "GET" || f.requests[0].Path != "/api/v1/tasks/"+idA {
		t.Fatalf("did not read first: %+v", f.requests)
	}
}

// cli: API error (text and --json)
func TestAPIError(t *testing.T) {
	f := newFakeAPI(t)
	code, out, errs := run(t, f, "task", "done", idC)
	if code == 0 || !strings.Contains(errs, "the task changed since expected_version was read") || out != "" {
		t.Fatalf("code=%d out=%q err=%q", code, out, errs)
	}
	code, out, _ = run(t, f, "task", "done", idC, "--json")
	var problem map[string]any
	if code == 0 || json.Unmarshal([]byte(out), &problem) != nil || problem["code"] != "conflict" {
		t.Fatalf("code=%d out=%q", code, out)
	}
}

// cli: JSON output
func TestJSONOutputIsTheAPIBody(t *testing.T) {
	f := newFakeAPI(t)
	code, out, _ := run(t, f, "today", "--json")
	if code != 0 || out != `{"items":[]}`+"\n" {
		t.Fatalf("code=%d out=%q", code, out)
	}
}

func TestEditBuildsPatch(t *testing.T) {
	f := newFakeAPI(t)
	code, _, errs := run(t, f, "task", "edit", idA, "--deadline", "none", "--priority", "A", "--tags", "home,bills", "--title", "New")
	if code != 0 {
		t.Fatal(errs)
	}
	w := f.writes()[0]
	got, _ := json.Marshal(w.Body)
	if w.Method != "PATCH" || string(got) != `{"deadline":null,"expected_version":"v-3f2a","priority":"A","tags":["home","bills"],"title":"New"}` {
		t.Fatalf("%s %s", w.Method, got)
	}
	if code, _, _ := run(t, f, "task", "edit", idA, "--repeat", "+1w"); code == 0 {
		t.Fatal("--repeat without a date accepted")
	}
}

// cli: Postponing keeps the repeater
func TestPostponeKeepsRepeater(t *testing.T) {
	f := newFakeAPI(t)
	f.repeat = true
	if code, _, errs := run(t, f, "task", "edit", idA, "--deadline", "2026-10-30"); code != 0 {
		t.Fatal(errs)
	}
	got, _ := json.Marshal(f.writes()[0].Body["deadline"])
	if string(got) != `{"date":"2026-10-30","repeat":"+1m"}` {
		t.Fatalf("deadline %s", got)
	}
	if code, _, errs := run(t, f, "task", "edit", idA, "--deadline", "2026-10-30", "--repeat", "none"); code != 0 {
		t.Fatal(errs)
	}
	got, _ = json.Marshal(f.writes()[1].Body["deadline"])
	if string(got) != `{"date":"2026-10-30"}` {
		t.Fatalf("deadline with --repeat none %s", got)
	}
}

func TestUnknownOutcomeIsExplained(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" {
			w.Write([]byte(`{"id":"` + idA + `","title":"t","state":"NEXT","tags":[],"version":"v"}`))
			return
		}
		w.WriteHeader(503)
		w.Write([]byte(`{"code":"engine_unavailable","detail":"engine did not answer"}`))
	}))
	defer srv.Close()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("ORGANON_URL", srv.URL)
	t.Setenv("ORGANON_TOKEN", "tok")
	var out, errb bytes.Buffer
	if Run([]string{"task", "done", idA}, &out, &errb) == 0 || !strings.Contains(errb.String(), "organon task show 3f2a1111") {
		t.Fatalf("err=%q", errb.String())
	}
}
