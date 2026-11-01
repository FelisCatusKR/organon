//go:build e2e

package e2e

// The web interface against a real stack: a browser's HTTP conversation, with
// redirects followed and cookies kept (specs: web-ui).

import (
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"regexp"
	"strings"
	"testing"
)

func browser(t *testing.T) *http.Client {
	t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	return &http.Client{Jar: jar}
}

func webPost(t *testing.T, c *http.Client, path string, form url.Values) (int, string) {
	t.Helper()
	resp, err := c.PostForm(os.Getenv("ORGANON_E2E_URL")+path, form)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(body)
}

func webGet(t *testing.T, c *http.Client, path string) (int, string) {
	t.Helper()
	resp, err := c.Get(os.Getenv("ORGANON_E2E_URL") + path)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(body)
}

// web-ui: Valid token, Add, Complete a task, Sign-out, against the real engine.
func TestWebSignInAddCompleteSignOut(t *testing.T) {
	c := browser(t)

	status, page := webGet(t, c, "/")
	if status != http.StatusOK || !strings.Contains(page, `name="token"`) {
		t.Fatalf("expected the sign-in page, got %d: %s", status, page)
	}
	status, page = webPost(t, c, "/login", url.Values{"token": {os.Getenv("ORGANON_E2E_TOKEN")}})
	if status != http.StatusOK {
		t.Fatalf("sign-in: %d: %s", status, page)
	}

	const title = "Web e2e 작업"
	key := regexp.MustCompile(`name="key" value="([^"]+)"`).FindStringSubmatch(page)
	if key == nil {
		t.Fatalf("no add form: %s", page)
	}
	status, page = webPost(t, c, "/tasks", url.Values{"title": {title}, "key": {key[1]}, "back": {"/tasks"}})
	if status != http.StatusOK || !strings.Contains(page, title) {
		t.Fatalf("add: %d: %s", status, page)
	}

	var id, version string
	for _, task := range items(api(t, "GET", "/api/v1/tasks", nil)) {
		if task["title"] == title {
			id, _ = task["id"].(string)
			version, _ = task["version"].(string)
		}
	}
	if id == "" || !strings.Contains(page, "/tasks/"+id+"/complete") {
		t.Fatalf("task not listed with an action (id %q): %s", id, page)
	}
	status, page = webPost(t, c, "/tasks/"+id+"/complete", url.Values{
		"expected_state": {"TODO"}, "expected_version": {version}, "back": {"/tasks"},
	})
	if status != http.StatusOK || strings.Contains(page, title) {
		t.Fatalf("complete: %d: %s", status, page)
	}

	if status, page = webPost(t, c, "/logout", url.Values{}); status != http.StatusOK || !strings.Contains(page, `name="token"`) {
		t.Fatalf("sign-out: %d: %s", status, page)
	}
}

// web-ui: Repeating task asks first, Confirmed, against the real engine.
func TestWebRepeatingTaskConfirmation(t *testing.T) {
	c := browser(t)
	if status, page := webPost(t, c, "/login", url.Values{"token": {os.Getenv("ORGANON_E2E_TOKEN")}}); status != http.StatusOK {
		t.Fatalf("sign-in: %d: %s", status, page)
	}
	task := createTask(t, map[string]any{
		"title": "Web e2e repeating", "state": "NEXT",
		"deadline": map[string]any{"date": "2026-10-25", "repeat": "+1w"},
	})
	id := task["id"].(string)
	form := url.Values{"expected_state": {"NEXT"}, "expected_version": {task["version"].(string)}, "back": {"/tasks"}}

	// The redirect ends on the confirmation page, and nothing has changed yet.
	status, page := webPost(t, c, "/tasks/"+id+"/complete", form)
	if status != http.StatusOK || !strings.Contains(page, `name="confirmed"`) || !strings.Contains(page, "Web e2e repeating") {
		t.Fatalf("confirmation: %d: %s", status, page)
	}
	deadline := func() string {
		r := api(t, "GET", "/api/v1/tasks/"+id, nil)
		mustStatus(t, r, http.StatusOK)
		return r.Body["deadline"].(map[string]any)["date"].(string)
	}
	if got := deadline(); got != "2026-10-25" {
		t.Fatalf("moved before confirmation: %s", got)
	}

	form.Set("confirmed", "1")
	if status, page := webPost(t, c, "/tasks/"+id+"/complete", form); status != http.StatusOK {
		t.Fatalf("confirmed: %d: %s", status, page)
	}
	if got := deadline(); got != "2026-11-01" {
		t.Fatalf("deadline after confirmation: %s", got)
	}
}
