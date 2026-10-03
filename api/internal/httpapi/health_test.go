package httpapi

// Probes (spec: service-health) and index_rebuilding (spec: knowledge-index).

import (
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/FelisCatusKR/organon/api/internal/rpc"
)

// service-health: Engine paused (liveness); Engine unavailable
func TestLivenessNeverContactsTheEngine(t *testing.T) {
	defer func(d time.Duration) { healthCacheFor = d }(healthCacheFor)
	healthCacheFor = 0
	engine, h := newTestServer(t)
	engine.answers["ping"] = func(map[string]any) (any, error) {
		return nil, &rpc.Error{Code: rpc.CodeUnavailable, Message: "engine did not answer"}
	}
	res := do(t, h, "GET", "/livez", "", "")
	if res.status != 200 || res.body["status"] != "pass" || res.header.Get("Content-Type") != "application/health+json" {
		t.Fatalf("livez: %d %v %s", res.status, res.header, res.raw)
	}
	if engine.count() != 0 {
		t.Fatalf("livez contacted the engine %d times", engine.count())
	}
	if res := do(t, h, "GET", "/readyz", "", ""); res.status != 503 || res.body["status"] != "fail" ||
		res.body["output"] != "engine unavailable" {
		t.Fatalf("readyz: %d %s", res.status, res.raw)
	}
}

// service-health: Ready; Index rebuilding; Same answer
func TestReadinessReportsTheIndex(t *testing.T) {
	defer func(d time.Duration) { healthCacheFor = d }(healthCacheFor)
	healthCacheFor = 0
	engine, h := newTestServer(t)
	for _, c := range []struct{ index, status, output string }{
		{"ready", "pass", ""},
		{"rebuilding", "warn", "note index rebuilding"},
		{"failed", "warn", "note index failed"},
	} {
		engine.answers["ping"] = func(map[string]any) (any, error) {
			return map[string]any{"status": "ok", "index": c.index}, nil
		}
		ready := do(t, h, "GET", "/readyz", "", "")
		legacy := do(t, h, "GET", "/healthz", "", "")
		if ready.status != 200 || ready.body["status"] != c.status || (c.output != "" && ready.body["output"] != c.output) {
			t.Fatalf("index %s: %d %s", c.index, ready.status, ready.raw)
		}
		if legacy.status != ready.status || legacy.raw != ready.raw {
			t.Fatalf("healthz differs from readyz: %d %s vs %d %s", legacy.status, legacy.raw, ready.status, ready.raw)
		}
	}
}

// knowledge-index: Node request during a rebuild (API side)
func TestIndexRebuildingIs503WithRetryAfter(t *testing.T) {
	engine, h := withNodesHandler(t)
	rebuilding := func(map[string]any) (any, error) {
		return nil, &rpc.Error{Code: rpc.CodeIndexRebuilding, Message: "the note index is being rebuilt; try again shortly"}
	}
	engine.answers["nodes.search"] = rebuilding
	engine.answers["node.create"] = rebuilding
	v := newContractValidator(t)
	for _, c := range []struct{ method, target, token, body string }{
		{"GET", "/api/v1/nodes", readToken, ""},
		{"POST", "/api/v1/nodes", noteToken, `{"title":"x"}`},
	} {
		req := httptest.NewRequest(c.method, c.target, strings.NewReader(c.body))
		if c.body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		req.Header.Set("Authorization", "Bearer "+c.token)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != 503 || rec.Header().Get("Retry-After") != "10" || !strings.Contains(rec.Body.String(), `"code":"index_rebuilding"`) {
			t.Fatalf("%s %s: %d %v %s", c.method, c.target, rec.Code, rec.Header(), rec.Body)
		}
		vreq := httptest.NewRequest(c.method, c.target, strings.NewReader(c.body))
		vreq.Header = req.Header
		validateExchange(t, v, vreq, rec)
	}
}

// knowledge-index: Progress (API side: meta passes the index through)
func TestMetaCarriesIndexProgress(t *testing.T) {
	engine, h := newTestServer(t)
	engine.answers["meta"] = func(map[string]any) (any, error) {
		return map[string]any{"calendar_tz": "Asia/Seoul", "today": "2026-10-02", "doing_limit": 3, "engine_version": "e",
			"index": map[string]any{"state": "rebuilding", "files_done": 120, "files_total": 5000}}, nil
	}
	v := newContractValidator(t)
	req := httptest.NewRequest("GET", "/api/v1/meta", nil)
	req.Header.Set("Authorization", "Bearer "+readToken)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"index":{"files_done":120,"files_total":5000,"state":"rebuilding"}`) {
		t.Fatalf("meta: %d %s", rec.Code, rec.Body)
	}
	validateExchange(t, v, httptest.NewRequest("GET", "/api/v1/meta", nil), rec)
}

// service-health: probes also match the contract
func TestProbesMatchContract(t *testing.T) {
	v := newContractValidator(t)
	_, h := newTestServer(t)
	for _, path := range []string{"/livez", "/readyz", "/healthz"} {
		req := httptest.NewRequest("GET", path, nil)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		validateExchange(t, v, httptest.NewRequest("GET", path, nil), rec)
	}
}
