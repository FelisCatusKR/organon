//go:build e2e

package e2e

import (
	"os"
	"reflect"
	"strings"
	"testing"
	"time"
)

// Tests run in this order (go test keeps source order, files sorted by name)
// and share one stack. Fault tests come after the functional ones; recreation
// and the stray-file check are in z_final_test.go, so they run last.

// ---- 8.1 smoke ---------------------------------------------------------------

func TestSmoke(t *testing.T) {
	r := call(t, "", "GET", "/healthz", nil)
	mustStatus(t, r, 200)
	meta := api(t, "GET", "/api/v1/meta", nil)
	mustStatus(t, meta, 200)
	if meta.Body["calendar_tz"] != "Asia/Seoul" {
		t.Fatalf("meta = %s", meta.Raw)
	}
}

// api-access: Missing token / Read-only token tries to write
func TestTokensAndScopes(t *testing.T) {
	mustStatus(t, call(t, "", "GET", "/api/v1/tasks/today", nil), 401)
	mustStatus(t, call(t, readToken, "GET", "/api/v1/tasks/today", nil), 200)
	mustStatus(t, call(t, readToken, "POST", "/api/v1/tasks", map[string]any{"title": "nope"}), 403)
	if strings.Contains(readData(t, "org/tasks/inbox.org"), "nope") {
		t.Fatal("read-only token wrote a task")
	}
}

// ---- 8.2 acceptance S1–S4 ------------------------------------------------------

// S1 / task-lifecycle: Task is persisted as an Org heading
func TestS1CreateTaskWritesOrgFile(t *testing.T) {
	task := createTask(t, map[string]any{"title": "Spotify 가족 요금제 납부"})
	inbox := readData(t, "org/tasks/inbox.org")
	if !strings.Contains(inbox, "* TODO Spotify 가족 요금제 납부") || !strings.Contains(inbox, ":ID:       "+task["id"].(string)) {
		t.Fatalf("inbox.org:\n%s", inbox)
	}
	got := api(t, "GET", "/api/v1/tasks/"+task["id"].(string), nil)
	mustStatus(t, got, 200)
	if got.Body["title"] != "Spotify 가족 요금제 납부" {
		t.Fatalf("get = %s", got.Raw)
	}
}

// S2 / task-recurrence: Cumulative repeater (+), computed by Org
func TestS2RecurringTaskAdvancesByOrgRepeater(t *testing.T) {
	task := createTask(t, map[string]any{
		"title": "Monthly payment", "state": "NEXT",
		"deadline": map[string]any{"date": "2026-08-25", "repeat": "+1m", "warning_days": 3},
	})
	// Without expected_version a repeating task refuses to transition.
	r := api(t, "POST", "/api/v1/tasks/"+task["id"].(string)+"/complete", map[string]any{"expected_state": "NEXT"})
	mustStatus(t, r, 422)

	r = complete(t, task)
	mustStatus(t, r, 200)
	after := r.Body["task"].(map[string]any)
	deadline := after["deadline"].(map[string]any)
	if after["state"] != "NEXT" || deadline["date"] != "2026-09-25" || deadline["repeat"] != "+1m" || after["id"] != task["id"] {
		t.Fatalf("after completion: %s", r.Raw)
	}
	if !strings.Contains(readData(t, "org/tasks/inbox.org"), "DEADLINE: <2026-09-25 Fri +1m -3d>") {
		t.Fatal("deadline not advanced in the file")
	}
	// task-lifecycle: Retried completion of a repeating task
	mustStatus(t, complete(t, task), 409)
	if strings.Contains(readData(t, "org/tasks/inbox.org"), "2026-10-25 Sun +1m -3d") {
		t.Fatal("retry moved the deadline a second time")
	}
}

// S3 / agenda-queries: Kinds on a fixed day, Default window, Overdue subset
func TestS3TodayFollowsAgendaSemantics(t *testing.T) {
	ids := map[string]string{}
	for title, planning := range map[string]map[string]any{
		"e2e scheduled today": {"scheduled": map[string]any{"date": "2026-10-02", "time": "15:00"}},
		"e2e scheduled past":  {"scheduled": map[string]any{"date": "2026-09-29"}},
		"e2e deadline past":   {"deadline": map[string]any{"date": "2026-09-28"}},
		"e2e deadline in 5":   {"deadline": map[string]any{"date": "2026-10-07"}},
		"e2e deadline in 10":  {"deadline": map[string]any{"date": "2026-10-12"}},
	} {
		body := map[string]any{"title": title, "state": "NEXT"}
		for k, v := range planning {
			body[k] = v
		}
		ids[title] = createTask(t, body)["id"].(string)
	}
	today := items(api(t, "GET", "/api/v1/tasks/today?date=2026-10-02", nil))
	want := map[string]string{
		"e2e scheduled today": "scheduled", "e2e scheduled past": "past-scheduled",
		"e2e deadline past": "deadline", "e2e deadline in 5": "upcoming-deadline",
	}
	for title, kind := range want {
		task := findByID(today, ids[title])
		if task == nil || task["agenda"].(map[string]any)["kind"] != kind {
			t.Errorf("%s: got %v, want kind %s", title, task, kind)
		}
	}
	if findByID(today, ids["e2e deadline in 10"]) != nil {
		t.Error("deadline in 10 days is outside the 7-day default window")
	}
	overdue := items(api(t, "GET", "/api/v1/tasks/overdue?date=2026-10-02", nil))
	for title, isOverdue := range map[string]bool{
		"e2e scheduled past": true, "e2e deadline past": true, "e2e scheduled today": false, "e2e deadline in 5": false,
	} {
		if (findByID(overdue, ids[title]) != nil) != isOverdue {
			t.Errorf("%s: overdue = %v, want %v", title, !isOverdue, isOverdue)
		}
	}
}

// S4 / task-lifecycle: Complete a non-repeating task
func TestS4CompleteMarksDone(t *testing.T) {
	task := createTask(t, map[string]any{"title": "One-off errand", "state": "NEXT"})
	r := complete(t, task)
	mustStatus(t, r, 200)
	done := r.Body["task"].(map[string]any)
	if done["state"] != "DONE" || done["closed_at"] == nil {
		t.Fatalf("after completion: %s", r.Raw)
	}
	inbox := readData(t, "org/tasks/inbox.org")
	if !strings.Contains(inbox, "* DONE One-off errand") || !strings.Contains(inbox, `- State "DONE"       from "NEXT"`) {
		t.Fatalf("inbox.org:\n%s", inbox)
	}
	// task-lifecycle: Retried completion of a non-repeating task
	retry := complete(t, task)
	mustStatus(t, retry, 409)
	if retry.Body["actual_state"] != "DONE" {
		t.Fatalf("retry: %s", retry.Raw)
	}
	completed := items(api(t, "GET", "/api/v1/tasks/completed", nil))
	if findByID(completed, task["id"].(string)) == nil {
		t.Fatal("not listed as completed today")
	}
}

// task-lifecycle: Same key, same payload
func TestIdempotentCreate(t *testing.T) {
	body := map[string]any{"title": "Created once"}
	a := api(t, "POST", "/api/v1/tasks", body, "Idempotency-Key", "e2e-once")
	b := api(t, "POST", "/api/v1/tasks", body, "Idempotency-Key", "e2e-once")
	mustStatus(t, a, 201)
	mustStatus(t, b, 201)
	if a.Body["id"] != b.Body["id"] || strings.Count(readData(t, "org/tasks/inbox.org"), "Created once") != 1 {
		t.Fatal("idempotent create created twice")
	}
}

// ---- 8.3 data integrity ---------------------------------------------------------

// data-integrity: Korean, emoji and quotes
func TestUnicodeRoundTrip(t *testing.T) {
	title := `한글 ✓ "quote" 🎉`
	task := createTask(t, map[string]any{"title": title})
	if task["title"] != title || !strings.Contains(readData(t, "org/tasks/inbox.org"), title) {
		t.Fatalf("title = %q", task["title"])
	}
	got := api(t, "GET", "/api/v1/tasks/"+task["id"].(string), nil)
	if got.Body["title"] != title {
		t.Fatalf("read back %q", got.Body["title"])
	}
}

// data-integrity: Edited while engine runs
func TestExternalEditIsPickedUp(t *testing.T) {
	time.Sleep(1100 * time.Millisecond) // make the modification time differ
	ctl(t, "host-append", "org/tasks/inbox.org", "* TODO Added by another process\n")
	createTask(t, map[string]any{"title": "Created after the external edit"})
	inbox := readData(t, "org/tasks/inbox.org")
	if !strings.Contains(inbox, "Added by another process") || !strings.Contains(inbox, "Created after the external edit") {
		t.Fatalf("inbox.org:\n%s", inbox)
	}
}

// data-integrity: Durable after engine kill
func TestDurableAfterEngineKill(t *testing.T) {
	task := createTask(t, map[string]any{"title": "Survives a kill"})
	ctl(t, "kill-engine")
	mustStatus(t, api(t, "GET", "/api/v1/tasks/"+task["id"].(string), nil), 503)
	ctl(t, "start-engine")
	got := api(t, "GET", "/api/v1/tasks/"+task["id"].(string), nil)
	mustStatus(t, got, 200)
	if got.Body["title"] != "Survives a kill" {
		t.Fatalf("got %s", got.Raw)
	}
}

// data-integrity: Paused engine
func TestPausedEngineGives503ThenRecovers(t *testing.T) {
	ctl(t, "pause-engine")
	start := time.Now()
	r := api(t, "GET", "/api/v1/tasks/today", nil)
	elapsed := time.Since(start)
	ctl(t, "unpause-engine")
	mustStatus(t, r, 503)
	if r.Body["code"] != "engine_unavailable" || elapsed > 11*time.Second {
		t.Fatalf("got %s after %s", r.Raw, elapsed)
	}
	mustStatus(t, api(t, "GET", "/api/v1/tasks/today", nil), 200)
}

// task-lifecycle: Retry after an engine timeout
func TestRetryAfterEngineTimeoutCreatesOnce(t *testing.T) {
	const title = "Created while the engine was paused"
	body := map[string]any{"title": title}
	// The API gives up after ORGANON_ENGINE_TIMEOUT, but the request is already
	// queued at the engine, which completes it once it runs again.
	ctl(t, "pause-engine")
	first := api(t, "POST", "/api/v1/tasks", body, "Idempotency-Key", "e2e-engine-timeout")
	ctl(t, "unpause-engine")
	mustStatus(t, first, 503)

	retry := api(t, "POST", "/api/v1/tasks", body, "Idempotency-Key", "e2e-engine-timeout")
	mustStatus(t, retry, 201)
	if n := strings.Count(readData(t, "org/tasks/inbox.org"), title); n != 1 {
		t.Fatalf("%d headings titled %q", n, title)
	}
	got := api(t, "GET", "/api/v1/tasks/"+retry.Body["id"].(string), nil)
	mustStatus(t, got, 200)
	if got.Body["title"] != title {
		t.Fatalf("got %s", got.Raw)
	}
}

// ---- 8.4 deployment ---------------------------------------------------------------

// deployment: Network disabled
func TestEngineHasNoNetwork(t *testing.T) {
	ifaces := strings.Fields(ctl(t, "engine-interfaces"))
	if !reflect.DeepEqual(ifaces, []string{"lo"}) {
		t.Fatalf("engine interfaces: %v", ifaces)
	}
}

// deployment: Custom UID (and the default): files belong to the container user
func TestDataOwnedByContainerUser(t *testing.T) {
	if out := strings.TrimSpace(ctl(t, "owner-violations")); out != "" {
		t.Fatalf("files not owned by uid %s:\n%s", os.Getenv("ORGANON_E2E_UID"), out)
	}
}
