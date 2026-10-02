// Package instance creates a new data directory (spec: deployment, Data
// directory initialization).
package instance

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
	_ "time/tzdata" // validate zones even where the system has no zoneinfo
)

// Layout lists the directories of a data directory (docs/architecture.md §5).
var Layout = []string{
	"org/tasks", "org/projects", "org/knowledge", "org/journal", "org/archive", "attachments",
}

// Config is organon.json.
type Config struct {
	CalendarTZ string `json:"calendar_tz"`
	DoingLimit int    `json:"doing_limit"`
}

// nonZones load as a Location but do not name a place: "" and Local are the
// host zone, and localtime, posixrules and Factory are zoneinfo files the
// engine rejects (emacs/organon.el, organon-valid-zone-p).
var nonZones = map[string]bool{"": true, "Local": true, "localtime": true, "posixrules": true, "Factory": true}

// ErrExists is returned when the directory already holds an instance.
var ErrExists = errors.New("organon.json already exists; refusing to overwrite an existing instance")

// Init creates the layout, an empty inbox and organon.json in dataDir.
func Init(dataDir string, cfg Config) error {
	if _, err := time.LoadLocation(cfg.CalendarTZ); err != nil || nonZones[cfg.CalendarTZ] {
		return fmt.Errorf("invalid calendar time zone %q (use an IANA name such as Asia/Seoul)", cfg.CalendarTZ)
	}
	if cfg.DoingLimit <= 0 {
		return fmt.Errorf("doing limit must be positive")
	}
	configPath := filepath.Join(dataDir, "organon.json")
	if _, err := os.Stat(configPath); err == nil {
		return ErrExists
	}
	for _, dir := range Layout {
		if err := os.MkdirAll(filepath.Join(dataDir, dir), 0o755); err != nil {
			return err
		}
	}
	inbox := filepath.Join(dataDir, "org/tasks/inbox.org")
	if _, err := os.Stat(inbox); errors.Is(err, os.ErrNotExist) {
		if err := os.WriteFile(inbox, []byte("#+title: Inbox\n"), 0o644); err != nil {
			return err
		}
	}
	body, _ := json.MarshalIndent(cfg, "", "  ")
	// O_EXCL: never overwrite, even if another init raced us.
	f, err := os.OpenFile(configPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			return ErrExists
		}
		return err
	}
	defer f.Close()
	_, err = f.Write(append(body, '\n'))
	return err
}

// DetectZone proposes the host's time zone from $TZ or /etc/localtime.
func DetectZone() string {
	if tz := os.Getenv("TZ"); tz != "" && !strings.HasPrefix(tz, ":") {
		if _, err := time.LoadLocation(tz); err == nil {
			return tz
		}
	}
	if target, err := os.Readlink("/etc/localtime"); err == nil {
		if _, zone, ok := strings.Cut(target, "zoneinfo/"); ok {
			return zone
		}
	}
	return ""
}
