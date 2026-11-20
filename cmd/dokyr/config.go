package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"

	"panel/internal/brand"
)

// config is what the CLI remembers between runs: one context per panel, and which one
// commands use. The token is a credential, so the file is readable by its owner only.
type config struct {
	Current  string                  `json:"current"`
	Contexts map[string]panelContext `json:"contexts"`
}

// panelContext is a sign-in on one panel.
type panelContext struct {
	URL   string `json:"url"`
	User  string `json:"user"`
	Role  string `json:"role"`
	Token string `json:"token"`
}

// configPath is ~/.config/dokyr/config.json ($XDG_CONFIG_HOME when set); on Windows the
// user's AppData folder.
func configPath() (string, error) {
	dir := os.Getenv("XDG_CONFIG_HOME")
	if dir == "" {
		if runtime.GOOS == "windows" {
			d, err := os.UserConfigDir()
			if err != nil {
				return "", err
			}
			dir = d
		} else {
			home, err := os.UserHomeDir()
			if err != nil {
				return "", err
			}
			dir = filepath.Join(home, ".config")
		}
	}
	return filepath.Join(dir, brand.Slug, "config.json"), nil
}

func loadConfig() (config, error) {
	cfg := config{Contexts: map[string]panelContext{}}
	path, err := configPath()
	if err != nil {
		return cfg, err
	}
	raw, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return cfg, nil
	}
	if err != nil {
		return cfg, err
	}
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return cfg, fmt.Errorf("%s is not valid JSON: %w", path, err)
	}
	if cfg.Contexts == nil {
		cfg.Contexts = map[string]panelContext{}
	}
	return cfg, nil
}

// saveConfig writes the file through a temporary one, so a crash leaves the old sign-in.
func saveConfig(cfg config) error {
	path, err := configPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".config-*.json") // created with mode 600
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(append(raw, '\n')); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// current is the sign-in commands use, or an error that says how to get one.
func (c config) current() (panelContext, error) {
	pc, ok := c.Contexts[c.Current]
	if !ok || pc.Token == "" {
		return panelContext{}, fmt.Errorf("not signed in; run `%s login https://your-panel`", brand.Slug)
	}
	return pc, nil
}
