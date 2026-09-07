package dashboard

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

type settings struct {
	Views             []savedView              `json:"saved_views,omitempty"`
	SystemdActiveOnly bool                     `json:"systemd_active_only"`
	SSHConnections    map[string]sshPreference `json:"ssh_connections,omitempty"`
	Theme             string                   `json:"theme"`
	RefreshSeconds    int                      `json:"refresh_seconds"`
	Projects          []project                `json:"projects,omitempty"`
	Favorites         []string                 `json:"favorites,omitempty"`
	Layout            string                   `json:"layout,omitempty"`
	WrapLogs          bool                     `json:"wrap_logs"`
	Splash            bool                     `json:"splash"`
	NerdIcons         bool                     `json:"nerd_icons"`
	Accent            string                   `json:"accent,omitempty"`
	Background        string                   `json:"background,omitempty"`
}

func settingsPath() (string, error) {
	dir, err := os.UserConfigDir()
	return filepath.Join(dir, "systemdoc", "settings.json"), err
}

func readSettings() (settings, error) {
	s := settings{Theme: "Deep Navy", RefreshSeconds: 5, Layout: "auto", Splash: true}
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
	if err = json.Unmarshal(data, &s); err != nil {
		return defaults, err
	}
	if s.RefreshSeconds < 2 || s.RefreshSeconds > 300 {
		s.RefreshSeconds = 5
	}
	s.Theme = themes[themeIndex(s.Theme)].name
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
