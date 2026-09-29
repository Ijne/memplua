package config

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestTimeoutConfigCompatibilityAndRoundTrip(t *testing.T) {
	clearMempluaEnvironment(t)
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte("[models]\nrequest_timeout = \"45m\"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Models.ResponseHeaderTimeout != 90*time.Second || cfg.Models.StreamIdleTimeout != 90*time.Second {
		t.Fatal("legacy defaults missing")
	}
	manager := NewManager(path, cfg)
	header, idle := "3m", "4m"
	next, err := manager.Update(SettingsPatch{ModelResponseHeaderTimeout: &header, ModelStreamIdleTimeout: &idle})
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Models.ResponseHeaderTimeout != next.Models.ResponseHeaderTimeout || loaded.Models.StreamIdleTimeout != next.Models.StreamIdleTimeout || loaded.Models.RequestTimeout != 45*time.Minute {
		t.Fatal("round trip changed timeouts")
	}
	t.Setenv("MEMPLUA_MODEL_RESPONSE_HEADER_TIMEOUT", "7m")
	t.Setenv("MEMPLUA_MODEL_STREAM_IDLE_TIMEOUT", "8m")
	loaded, err = Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Models.ResponseHeaderTimeout != 7*time.Minute || loaded.Models.StreamIdleTimeout != 8*time.Minute {
		t.Fatal("environment did not override file")
	}
}

func TestTimeoutPatchValidationIsAtomic(t *testing.T) {
	clearMempluaEnvironment(t)
	path := filepath.Join(t.TempDir(), "config.toml")
	manager := NewManager(path, Default())
	valid := "45m"
	if _, err := manager.Update(SettingsPatch{ModelRequestTimeout: &valid}); err != nil {
		t.Fatal(err)
	}
	before := manager.Current()
	disk, _ := os.ReadFile(path)
	setters := []func(*string) SettingsPatch{
		func(v *string) SettingsPatch { return SettingsPatch{ModelStartupTimeout: v} },
		func(v *string) SettingsPatch { return SettingsPatch{ModelResponseHeaderTimeout: v} },
		func(v *string) SettingsPatch { return SettingsPatch{ModelStreamIdleTimeout: v} },
		func(v *string) SettingsPatch { return SettingsPatch{ModelRequestTimeout: v} },
		func(v *string) SettingsPatch { return SettingsPatch{AudioTranscriptionTimeout: v} },
		func(v *string) SettingsPatch { return SettingsPatch{ConspectMaxDuration: v} },
		func(v *string) SettingsPatch { return SettingsPatch{ConspectIdleTimeout: v} },
	}
	for _, setter := range setters {
		for _, value := range []string{"0", "0s", "-1m", "bad", "9999999999999999999h"} {
			patch := setter(&value)
			language := "en"
			patch.UILanguage = &language
			if _, err := manager.Update(patch); err == nil {
				t.Fatalf("accepted %q", value)
			}
			afterDisk, _ := os.ReadFile(path)
			if !reflect.DeepEqual(before, manager.Current()) || string(disk) != string(afterDisk) {
				t.Fatal("invalid patch mutated configuration")
			}
		}
	}
}

func TestNewTimeoutConfigRejectsInvalidFileAndEnvironment(t *testing.T) {
	for _, key := range []string{"response_header_timeout", "stream_idle_timeout"} {
		for _, value := range []string{"0s", "-1m", "bad"} {
			t.Run(key+value, func(t *testing.T) {
				clearMempluaEnvironment(t)
				path := filepath.Join(t.TempDir(), "config.toml")
				if err := os.WriteFile(path, []byte("[models]\n"+key+" = \""+value+"\"\n"), 0600); err != nil {
					t.Fatal(err)
				}
				if _, err := Load(path); err == nil {
					t.Fatal("accepted invalid file duration")
				}
				if err := os.WriteFile(path, []byte("[models]\n"), 0600); err != nil {
					t.Fatal(err)
				}
				env := "MEMPLUA_MODEL_RESPONSE_HEADER_TIMEOUT"
				if key == "stream_idle_timeout" {
					env = "MEMPLUA_MODEL_STREAM_IDLE_TIMEOUT"
				}
				t.Setenv(env, value)
				if _, err := Load(path); err == nil {
					t.Fatal("accepted invalid environment duration")
				}
			})
		}
	}
}
