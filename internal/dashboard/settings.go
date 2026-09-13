package dashboard

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

type settings struct {
	SettingsVersion   int                      `json:"settings_version,omitempty"`
	Views             []savedView              `json:"saved_views,omitempty"`
	SystemdActiveOnly bool                     `json:"systemd_active_only"`
	SSHConnections    map[string]sshPreference `json:"ssh_connections,omitempty"`
	Theme             string                   `json:"theme"`
	RefreshSeconds    int                      `json:"refresh_seconds"`
	Projects          []project                `json:"projects,omitempty"`
	Favorites         []string                 `json:"favorites,omitempty"`
	Layout            string                   `json:"layout,omitempty"`
	PaneRatio         int                      `json:"pane_ratio,omitempty"`
	WrapLogs          bool                     `json:"wrap_logs"`
	Splash            bool                     `json:"splash"`
	NerdIcons         bool                     `json:"nerd_icons"`
	Accent            string                   `json:"accent,omitempty"`
	Background        string                   `json:"background,omitempty"`
	GraphMode         string                   `json:"graph_mode,omitempty"`
}

func settingsPath() (string, error) {
	if dir := os.Getenv("XDG_CONFIG_HOME"); filepath.IsAbs(dir) {
		return filepath.Join(dir, "systemdoc", "settings.json"), nil
	}
	dir, err := os.UserConfigDir()
	return filepath.Join(dir, "systemdoc", "settings.json"), err
}

func readSettings() (settings, error) {
	s := settings{SettingsVersion: 1, Theme: "Cathedral", RefreshSeconds: 5, Layout: "auto", PaneRatio: 46, Splash: true, NerdIcons: true, GraphMode: "blocks"}
	path, err := settingsPath()
	if err != nil {
		return s, err
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return s, err
	}
	defaults := s
	// A missing version must remain distinguishable from the current default
	// while JSON is layered over the other default values.
	s.SettingsVersion = 0
	if err = json.Unmarshal(data, &s); err != nil {
		return defaults, err
	}
	if s.SettingsVersion < 1 {
		// The former default was false. Promote existing installations once;
		// subsequent saves record the schema and honor an explicit fallback.
		s.NerdIcons = true
	}
	s.SettingsVersion = 1
	if s.RefreshSeconds < 2 || s.RefreshSeconds > 300 {
		s.RefreshSeconds = 5
	}
	if s.PaneRatio < 30 || s.PaneRatio > 70 {
		s.PaneRatio = 46
	}
	s.Theme = themes[themeIndex(s.Theme)].name
	s.GraphMode = graphMode(s.GraphMode)
	return s, nil
}

func writeSettings(s settings) error {
	path, err := settingsPath()
	if err != nil {
		return err
	}
	if err = os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	s.SettingsVersion = 1
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".settings-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}
