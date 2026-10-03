//go:build e2e

package e2e

// The last tests of the suite (files run in name order): they check the
// state every earlier test left behind.

import (
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func manifest(t *testing.T) map[string]string {
	t.Helper()
	m := map[string]string{}
	err := filepath.WalkDir(dataDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		sum := sha256.Sum256(b)
		rel, _ := filepath.Rel(dataDir, path)
		m[rel] = hex.EncodeToString(sum[:])
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return m
}

// S8 / deployment: Recreate everything
func TestZRecreateEverything(t *testing.T) {
	ids := []string{}
	for _, it := range items(api(t, "GET", "/api/v1/tasks/today?date=2026-10-02", nil)) {
		ids = append(ids, it["id"].(string))
	}
	if len(ids) == 0 {
		t.Fatal("expected tasks from earlier tests")
	}
	before := map[string]string{}
	for _, id := range ids {
		before[id] = string(api(t, "GET", "/api/v1/tasks/"+id, nil).Raw)
	}
	files := manifest(t)
	nodesBefore := indexSnapshot(t)
	if len(nodesBefore) == 0 {
		t.Fatal("expected nodes from earlier tests")
	}

	ctl(t, "recreate") // containers and named volumes removed, then started again

	if after := manifest(t); !reflect.DeepEqual(files, after) {
		t.Fatalf("data directory changed:\nbefore %v\nafter  %v", files, after)
	}
	for _, id := range ids {
		if got := string(api(t, "GET", "/api/v1/tasks/"+id, nil).Raw); got != before[id] {
			t.Fatalf("task %s changed:\n%s\n%s", id, before[id], got)
		}
	}
	// knowledge-index: Recreate the whole stack (the index was in the cache volume)
	if after := indexSnapshot(t); !reflect.DeepEqual(nodesBefore, after) {
		t.Fatalf("index changed:\nbefore %v\nafter  %v", nodesBefore, after)
	}
}

// data-integrity: Directory after a test run
func TestZZNoStrayFiles(t *testing.T) {
	err := filepath.WalkDir(dataDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(dataDir, path)
		if strings.HasSuffix(rel, ".org") || rel == "organon.json" || strings.HasPrefix(rel, "attachments/") {
			return nil
		}
		t.Errorf("stray file in data directory: %s", rel)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
