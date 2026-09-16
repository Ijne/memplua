package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDesktopPreferencesAndLegacyLanguageRoundTrip(t *testing.T) {
	clearKnowledgeCrawlerEnvironment(t)
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte("language = \"en\"\n[ui]\nlanguage = \"ru\"\ntheme = \"dark\"\nalways_on_top = false\nstart_with_windows = true\n[conspect]\nlanguage = \"ru\"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Language != "ru" || cfg.Conspect.Language != "ru" || cfg.UI.Theme != "dark" || cfg.UI.AlwaysOnTop || !cfg.UI.StartWithWindows {
		t.Fatalf("configuration=%+v", cfg)
	}
	manager := NewManager(path, cfg)
	language := "en"
	if _, err = manager.Update(SettingsPatch{ConspectLanguage: &language}); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Conspect.Language != "en" || loaded.Language != "en" || loaded.UI != cfg.UI {
		t.Fatalf("round trip=%+v", loaded)
	}
	if err = os.WriteFile(path, []byte("language = \"en\"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	legacy, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if legacy.Conspect.Language != "en" {
		t.Fatal("legacy language not applied")
	}
}

func TestInvalidDesktopSettingsAreAtomic(t *testing.T) {
	cfg := Default()
	manager := NewManager(filepath.Join(t.TempDir(), "config.toml"), cfg)
	invalid := "neon"
	if _, err := manager.Update(SettingsPatch{Theme: &invalid}); err == nil {
		t.Fatal("accepted unsupported theme")
	}
	if manager.Current().UI != cfg.UI {
		t.Fatal("invalid update changed effective settings")
	}
}
