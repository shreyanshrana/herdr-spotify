package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

type storagePaths struct {
	Config string `json:"config"`
	State  string `json:"state"`
}

func openStore() (*store, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	legacy := filepath.Join(home, ".config", "herdr-spotify")
	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" {
		base = filepath.Join(home, ".config")
	}
	if !filepath.IsAbs(base) {
		return nil, fmt.Errorf("XDG_CONFIG_HOME must be absolute")
	}
	fallback := filepath.Join(base, "herdr-spotify")
	locator := &store{dir: filepath.Join(fallback, "location")}
	deadline := time.Now().Add(time.Second)
	var unlock func()
	for {
		unlock, err = locator.lock()
		if !errors.Is(err, syscall.EWOULDBLOCK) || time.Now().After(deadline) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err != nil {
		return nil, err
	}
	defer unlock()
	previous := storagePaths{Config: legacy, State: legacy}
	if err := locator.load("paths.json", &previous); err != nil {
		return nil, err
	}
	paths := previous
	// Without a saved location, honor XDG while preserving legacy data.
	if _, err := os.Stat(filepath.Join(locator.dir, "paths.json")); errors.Is(err, os.ErrNotExist) {
		paths = storagePaths{Config: fallback, State: fallback}
	}
	if dir := os.Getenv("HERDR_PLUGIN_CONFIG_DIR"); dir != "" {
		paths.Config = dir
	}
	if dir := os.Getenv("HERDR_PLUGIN_STATE_DIR"); dir != "" {
		paths.State = dir
	}
	if !filepath.IsAbs(paths.Config) || !filepath.IsAbs(paths.State) {
		return nil, fmt.Errorf("plugin config and state directories must be absolute")
	}
	s := &store{dir: paths.Config, stateDir: paths.State}
	if paths != previous {
		unlock, err := s.lock()
		if err != nil {
			return nil, err
		}
		defer unlock()
		for _, file := range []struct{ name, source string }{
			{"auth.json", previous.Config}, {"status.json", previous.State},
		} {
			if _, err := os.Stat(filepath.Join(s.directory(file.name), file.name)); err == nil {
				continue
			} else if !errors.Is(err, os.ErrNotExist) {
				return nil, err
			}
			data, err := os.ReadFile(filepath.Join(file.source, file.name))
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			if err != nil {
				return nil, err
			}
			if err := s.save(file.name, json.RawMessage(data)); err != nil {
				return nil, err
			}
		}
	}
	if err := locator.save("paths.json", paths); err != nil {
		return nil, err
	}
	return s, nil
}
