package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// Config is where the CLI finds the API (spec: cli, Configuration).
type Config struct {
	URL   string `json:"url"`
	Token string `json:"token"`
}

const configHelp = `no API configured. Either set
  ORGANON_URL=https://your-instance  ORGANON_TOKEN=org_...
or create ~/.config/organon/client.json (mode 0600):
  {"url": "https://your-instance", "token": "org_..."}`

// ConfigPath is $XDG_CONFIG_HOME/organon/client.json, or ~/.config/organon/client.json.
func ConfigPath() (string, error) {
	if dir := os.Getenv("XDG_CONFIG_HOME"); dir != "" {
		return filepath.Join(dir, "organon", "client.json"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "organon", "client.json"), nil
}

// LoadConfig reads the environment first and falls back to the file. A file
// that other users could read is refused, because it holds the token.
func LoadConfig() (Config, error) {
	cfg := Config{URL: os.Getenv("ORGANON_URL"), Token: os.Getenv("ORGANON_TOKEN")}
	if cfg.URL != "" && cfg.Token != "" {
		return cfg, nil
	}
	path, err := ConfigPath()
	if err != nil {
		return cfg, err
	}
	info, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		if cfg.URL == "" || cfg.Token == "" {
			return cfg, errors.New(configHelp)
		}
		return cfg, nil
	}
	if err != nil {
		return cfg, err
	}
	if info.Mode().Perm()&0o077 != 0 {
		return cfg, fmt.Errorf("%s is readable by other users (mode %04o); run: chmod 600 %s",
			path, info.Mode().Perm(), path)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return cfg, err
	}
	var file Config
	if err := json.Unmarshal(raw, &file); err != nil {
		return cfg, fmt.Errorf("%s: %w", path, err)
	}
	if cfg.URL == "" {
		cfg.URL = file.URL
	}
	if cfg.Token == "" {
		cfg.Token = file.Token
	}
	if cfg.URL == "" || cfg.Token == "" {
		return cfg, errors.New(configHelp)
	}
	return cfg, nil
}
