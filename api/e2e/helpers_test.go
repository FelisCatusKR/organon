//go:build e2e

// Package e2e runs the scenarios of openspec/specs against a
// real stack started by scripts/e2e.sh. Every response is validated against
// api/openapi.yaml.
package e2e

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/pb33f/libopenapi"
	validator "github.com/pb33f/libopenapi-validator"
)

var (
	baseURL   = os.Getenv("ORGANON_E2E_URL")
	token     = os.Getenv("ORGANON_E2E_TOKEN")
	readToken = os.Getenv("ORGANON_E2E_READ_TOKEN")
	dataDir   = os.Getenv("ORGANON_E2E_DATA")
	ctlCmd    = strings.Fields(os.Getenv("ORGANON_E2E_CTL"))

	contractOnce sync.Once
	contract     validator.Validator
)

func TestMain(m *testing.M) {
	if baseURL == "" || token == "" || dataDir == "" {
		os.Stderr.WriteString("run through scripts/e2e.sh\n")
		os.Exit(2)
	}
	os.Exit(m.Run())
}

func loadContract(t *testing.T) validator.Validator {
	t.Helper()
	contractOnce.Do(func() {
		spec, err := os.ReadFile("../openapi.yaml")
		if err != nil {
			t.Fatal(err)
		}
		doc, err := libopenapi.NewDocument(spec)
		if err != nil {
			t.Fatal(err)
		}
		v, errs := validator.NewValidator(doc)
		if len(errs) > 0 {
			t.Fatalf("validator: %v", errs)
		}
		contract = v
	})
	return contract
}

type response struct {
	Status int
	Body   map[string]any
	Raw    []byte
}

// call sends a request with the given token ("" for none) and validates the
// response against the contract.
func call(t *testing.T, tok, method, path string, body any, headers ...string) response {
	t.Helper()
	var payload []byte
	if body != nil {
		var err error
		if payload, err = json.Marshal(body); err != nil {
			t.Fatal(err)
		}
	}
	req, err := http.NewRequest(method, baseURL+path, bytes.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if tok != "" {
		req.Header.Set("Authorization", "Bearer "+tok)
	}
	for i := 0; i+1 < len(headers); i += 2 {
		req.Header.Set(headers[i], headers[i+1])
	}
	resp, err := (&http.Client{Timeout: 30 * time.Second}).Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	raw, _ := io.ReadAll(resp.Body)
	resp.Body.Close()

	// Validate a copy of the exchange against the contract.
	vreq, _ := http.NewRequest(method, baseURL+path, bytes.NewReader(payload))
	vreq.Header = req.Header
	vresp := &http.Response{StatusCode: resp.StatusCode, Header: resp.Header, Body: io.NopCloser(bytes.NewReader(raw)), Request: vreq}
	if ok, errs := loadContract(t).ValidateHttpResponse(vreq, vresp); !ok {
		for _, e := range errs {
			t.Errorf("contract: %s %s -> %d: %s (%s)", method, path, resp.StatusCode, e.Message, e.Reason)
		}
	}

	out := response{Status: resp.StatusCode, Raw: raw}
	_ = json.Unmarshal(raw, &out.Body)
	return out
}

func api(t *testing.T, method, path string, body any, headers ...string) response {
	t.Helper()
	return call(t, token, method, path, body, headers...)
}

func mustStatus(t *testing.T, r response, want int) {
	t.Helper()
	if r.Status != want {
		t.Fatalf("status %d, want %d: %s", r.Status, want, r.Raw)
	}
}

func createTask(t *testing.T, body map[string]any) map[string]any {
	t.Helper()
	r := api(t, "POST", "/api/v1/tasks", body)
	mustStatus(t, r, 201)
	return r.Body
}

func complete(t *testing.T, task map[string]any) response {
	t.Helper()
	return api(t, "POST", "/api/v1/tasks/"+task["id"].(string)+"/complete", map[string]any{
		"expected_state": task["state"], "expected_version": task["version"],
	})
}

func items(r response) []map[string]any {
	var out []map[string]any
	for _, it := range r.Body["items"].([]any) {
		out = append(out, it.(map[string]any))
	}
	return out
}

func findByID(list []map[string]any, id string) map[string]any {
	for _, it := range list {
		if it["id"] == id {
			return it
		}
	}
	return nil
}

func ctl(t *testing.T, args ...string) string {
	t.Helper()
	cmd := exec.Command(ctlCmd[0], append(ctlCmd[1:], args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("ctl %v: %v\n%s", args, err, out)
	}
	return string(out)
}

func readData(t *testing.T, rel string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dataDir, rel))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
