package desktop

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRestoreDisconnectedDisplay(t *testing.T) {
	work := windowBounds{X: -1280, Y: 0, Width: 1280, Height: 984}
	got := clampBounds(windowBounds{X: 2450, Y: 1250, Width: 420, Height: 48}, work)
	if got.X != -420 || got.Y != 936 {
		t.Fatalf("off-screen window: %+v", got)
	}
	oversized := clampBounds(windowBounds{X: -3000, Width: 3000, Height: 2000}, work)
	if oversized.X != -1280 || oversized.Width != 1280 || oversized.Height != 984 {
		t.Fatalf("oversized window: %+v", oversized)
	}
}

func TestStateRoundTripAndCorruption(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ui-state.json")
	state := uiState{Version: 1, Windows: map[string]windowBounds{"widget": {X: 25, Y: 16, Width: 420, Height: 48, Monitor: "display-2"}}}
	if err := saveState(path, state); err != nil {
		t.Fatal(err)
	}
	if got := loadState(path); got.Windows["widget"] != state.Windows["widget"] {
		t.Fatalf("state not restored: %+v", got)
	}
	state.Windows["widget"] = windowBounds{X: 30, Width: 420, Height: 48}
	if err := saveState(path, state); err != nil {
		t.Fatal(err)
	}
	if got := loadState(path); got.Windows["widget"].X != 30 {
		t.Fatalf("atomic replacement failed: %+v", got)
	}
	if err := os.WriteFile(path, []byte("{"), 0600); err != nil {
		t.Fatal(err)
	}
	if got := loadState(path); len(got.Windows) != 0 || got.Version != 1 {
		t.Fatalf("corrupt state not recovered: %+v", got)
	}
}
