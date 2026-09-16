package desktop

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

type windowBounds struct {
	X       int    `json:"x"`
	Y       int    `json:"y"`
	Width   int    `json:"width"`
	Height  int    `json:"height"`
	Monitor string `json:"monitor,omitempty"`
}

type uiState struct {
	Version int                     `json:"version"`
	Windows map[string]windowBounds `json:"windows"`
}

func loadState(path string) uiState {
	state := uiState{Version: 1, Windows: map[string]windowBounds{}}
	data, err := os.ReadFile(path)
	if err != nil {
		return state
	}
	var saved uiState
	if json.Unmarshal(data, &saved) == nil && saved.Version == 1 && saved.Windows != nil {
		return saved
	}
	return state
}

func saveState(path string, state uiState) error {
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".ui-state-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	if _, err = file.Write(data); err != nil {
		return errors.Join(err, file.Close())
	}
	if err = file.Sync(); err != nil {
		return errors.Join(err, file.Close())
	}
	if err = file.Close(); err != nil {
		return err
	}
	return os.Rename(file.Name(), path)
}

// Work areas are expressed in Wails device-independent pixels. The complete
// rectangle is kept visible when DPI changes or a previously used display leaves.
func clampBounds(bounds, work windowBounds) windowBounds {
	if work.Width <= 0 || work.Height <= 0 {
		return bounds
	}
	bounds.Width = min(max(bounds.Width, 1), work.Width)
	bounds.Height = min(max(bounds.Height, 1), work.Height)
	bounds.X = min(max(bounds.X, work.X), work.X+work.Width-bounds.Width)
	bounds.Y = min(max(bounds.Y, work.Y), work.Y+work.Height-bounds.Height)
	return bounds
}
