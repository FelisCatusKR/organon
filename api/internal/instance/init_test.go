package instance

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// deployment: Init on an empty directory
func TestInitOnEmptyDirectory(t *testing.T) {
	dir := t.TempDir()
	if err := Init(dir, Config{CalendarTZ: "Asia/Seoul", DoingLimit: 3}); err != nil {
		t.Fatal(err)
	}
	for _, p := range append([]string{"org/tasks/inbox.org", "organon.json"}, Layout...) {
		if _, err := os.Stat(filepath.Join(dir, p)); err != nil {
			t.Fatalf("missing %s", p)
		}
	}
	raw, _ := os.ReadFile(filepath.Join(dir, "organon.json"))
	var cfg Config
	if err := json.Unmarshal(raw, &cfg); err != nil || cfg.CalendarTZ != "Asia/Seoul" || cfg.DoingLimit != 3 {
		t.Fatalf("organon.json = %s", raw)
	}
}

// deployment: Init on an existing instance
func TestInitRefusesExistingInstance(t *testing.T) {
	dir := t.TempDir()
	if err := Init(dir, Config{CalendarTZ: "Asia/Seoul", DoingLimit: 3}); err != nil {
		t.Fatal(err)
	}
	inbox := filepath.Join(dir, "org/tasks/inbox.org")
	os.WriteFile(inbox, []byte("* TODO keep me\n"), 0o644)
	before, _ := os.ReadFile(filepath.Join(dir, "organon.json"))
	err := Init(dir, Config{CalendarTZ: "Europe/Berlin", DoingLimit: 5})
	if !errors.Is(err, ErrExists) {
		t.Fatalf("err = %v", err)
	}
	after, _ := os.ReadFile(filepath.Join(dir, "organon.json"))
	kept, _ := os.ReadFile(inbox)
	if string(before) != string(after) || string(kept) != "* TODO keep me\n" {
		t.Fatal("existing instance was modified")
	}
}

func TestInitRejectsBadZone(t *testing.T) {
	for _, zone := range []string{"", "Local", "Mars/Olympus", "../etc/passwd"} {
		if err := Init(t.TempDir(), Config{CalendarTZ: zone, DoingLimit: 3}); err == nil {
			t.Fatalf("accepted zone %q", zone)
		}
	}
}

func TestInitKeepsExistingInbox(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "org/tasks"), 0o755)
	os.WriteFile(filepath.Join(dir, "org/tasks/inbox.org"), []byte("* TODO old\n"), 0o644)
	if err := Init(dir, Config{CalendarTZ: "UTC", DoingLimit: 3}); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "org/tasks/inbox.org")); string(b) != "* TODO old\n" {
		t.Fatalf("inbox overwritten: %q", b)
	}
}
