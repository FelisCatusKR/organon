package client

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestDoSendsTokenAndDecodes(t *testing.T) {
	var gotAuth, gotPath, gotQuery, gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth, gotPath, gotQuery = r.Header.Get("Authorization"), r.URL.Path, r.URL.RawQuery
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"items":[{"id":"a","title":"t","state":"NEXT","tags":[]}]}`))
	}))
	defer srv.Close()
	c := New(srv.URL+"/", "tok")
	list, raw, err := c.ListTasks(context.Background(), []string{"DONE", "CANCELLED"}, "", "bills")
	if err != nil || len(list.Items) != 1 || list.Items[0].Title != "t" {
		t.Fatalf("list=%v err=%v", list, err)
	}
	if gotAuth != "Bearer tok" || gotPath != "/api/v1/tasks" || gotQuery != "state=DONE%2CCANCELLED&tag=bills" || gotBody != "" {
		t.Fatalf("auth=%q path=%q query=%q body=%q", gotAuth, gotPath, gotQuery, gotBody)
	}
	if string(raw) == "" {
		t.Fatal("raw body missing")
	}
	if _, _, err := c.UpdateTask(context.Background(), "x", "v1", map[string]any{"priority": nil, "title": "T"}); err != nil {
		t.Fatal(err)
	}
	var body map[string]any
	json.Unmarshal([]byte(gotBody), &body)
	if v, ok := body["priority"]; !ok || v != nil || body["expected_version"] != "v1" || body["title"] != "T" {
		t.Fatalf("patch body %s", gotBody)
	}
}

func TestProblemBecomesAPIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/problem+json")
		w.WriteHeader(409)
		w.Write([]byte(`{"code":"conflict","detail":"expected state NEXT but the task is DONE","status":409}`))
	}))
	defer srv.Close()
	_, raw, err := New(srv.URL, "t").GetTask(context.Background(), "x")
	var ae *APIError
	if !errors.As(err, &ae) || ae.Code != "conflict" || ae.Error() != "expected state NEXT but the task is DONE" || string(raw) != string(ae.Body) {
		t.Fatalf("err=%#v", err)
	}
	if OutcomeUnknown(err) {
		t.Fatal("a 409 is a known outcome")
	}
}

func TestTimeoutAndUnavailableAreUnknownOutcomes(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/tasks/slow" {
			time.Sleep(300 * time.Millisecond)
		}
		w.WriteHeader(503)
		w.Write([]byte(`{"code":"engine_unavailable","detail":"engine did not answer"}`))
	}))
	defer srv.Close()
	c := New(srv.URL, "t")
	_, _, err := c.GetTask(context.Background(), "x")
	if !OutcomeUnknown(err) {
		t.Fatalf("503: %v", err)
	}
	c.HTTP.Timeout = 50 * time.Millisecond
	_, _, err = c.GetTask(context.Background(), "slow")
	var te *TransportError
	if !errors.As(err, &te) || !OutcomeUnknown(err) {
		t.Fatalf("timeout: %#v", err)
	}
}
