package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestStorageLocationsStayConsistentAndPreserveLegacyData(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("HERDR_PLUGIN_CONFIG_DIR", "")
	t.Setenv("HERDR_PLUGIN_STATE_DIR", "")
	legacy := &store{dir: filepath.Join(home, ".config", "herdr-spotify")}
	unlock, err := legacy.lock()
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Hour)
	if err := legacy.save("auth.json", credentials{RefreshToken: "preserve-me"}); err != nil {
		t.Fatal(err)
	}
	if err := legacy.save("status.json", cache{RateUntil: deadline}); err != nil {
		t.Fatal(err)
	}
	unlock()
	config, state := filepath.Join(home, "plugin-config"), filepath.Join(home, "plugin-state")
	t.Setenv("HERDR_PLUGIN_CONFIG_DIR", config)
	t.Setenv("HERDR_PLUGIN_STATE_DIR", state)
	s, err := openStore()
	if err != nil {
		t.Fatal(err)
	}
	if s.dir != config || s.stateDir != state {
		t.Fatalf("wrong plugin paths: %+v", s)
	}
	var auth credentials
	var cached cache
	if err := s.load("auth.json", &auth); err != nil || auth.RefreshToken != "preserve-me" {
		t.Fatalf("credentials were not migrated: %+v, %v", auth, err)
	}
	if err := s.load("status.json", &cached); err != nil || !cached.RateUntil.Equal(deadline) {
		t.Fatalf("cooldown was not migrated: %+v, %v", cached, err)
	}
	for _, path := range []string{filepath.Join(config, "auth.json"), filepath.Join(state, "status.json"), filepath.Join(legacy.dir, "auth.json")} {
		info, err := os.Stat(path)
		if err != nil || info.Mode().Perm() != 0600 {
			t.Fatalf("missing or unsafe file %s: %v, %v", path, info, err)
		}
	}
	t.Setenv("HERDR_PLUGIN_CONFIG_DIR", "")
	t.Setenv("HERDR_PLUGIN_STATE_DIR", "")
	direct, err := openStore()
	if err != nil || direct.dir != config || direct.stateDir != state {
		t.Fatalf("direct command did not reuse plugin paths: %+v, %v", direct, err)
	}
	if err := direct.save("auth.json", credentials{RefreshToken: "new-token"}); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HERDR_PLUGIN_CONFIG_DIR", config)
	t.Setenv("HERDR_PLUGIN_STATE_DIR", state)
	again, err := openStore()
	if err != nil {
		t.Fatal(err)
	}
	if err := again.load("auth.json", &auth); err != nil || auth.RefreshToken != "new-token" {
		t.Fatalf("existing credentials were overwritten: %+v, %v", auth, err)
	}
}

func TestStorageHonorsXDGAndRejectsRelativePaths(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "xdg"))
	t.Setenv("HERDR_PLUGIN_CONFIG_DIR", "")
	t.Setenv("HERDR_PLUGIN_STATE_DIR", "")
	s, err := openStore()
	if err != nil || s.dir != filepath.Join(home, "xdg", "herdr-spotify") || s.stateDir != s.dir {
		t.Fatalf("incorrect fallback: %+v, %v", s, err)
	}
	t.Setenv("HERDR_PLUGIN_CONFIG_DIR", "relative")
	if _, err := openStore(); err == nil {
		t.Fatal("relative config directory must be rejected")
	}
}
