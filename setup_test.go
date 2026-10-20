package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestSetupPreservesExistingLaunchers(t *testing.T) {
	for _, kind := range []string{"absent", "same symlink", "file", "other symlink", "directory"} {
		for _, force := range []bool{false, true} {
			name := kind
			if force {
				name += " forced"
			}
			t.Run(name, func(t *testing.T) {
				home, root := t.TempDir(), t.TempDir()
				binary := filepath.Join(root, "bin", "herdr-spotify")
				launcher := filepath.Join(home, ".local", "bin", "herdr-spotify")
				for _, dir := range []string{filepath.Dir(binary), filepath.Dir(launcher)} {
					if err := os.MkdirAll(dir, 0700); err != nil {
						t.Fatal(err)
					}
				}
				if err := os.WriteFile(binary, []byte("#!/bin/sh\n"), 0700); err != nil {
					t.Fatal(err)
				}
				var err error
				switch kind {
				case "same symlink":
					err = os.Symlink(binary, launcher)
				case "file":
					err = os.WriteFile(launcher, []byte("keep me"), 0600)
				case "other symlink":
					err = os.Symlink("/missing/unrelated-launcher", launcher)
				case "directory":
					err = os.Mkdir(launcher, 0700)
				}
				if err != nil {
					t.Fatal(err)
				}
				args := []string{"scripts/setup.sh"}
				if force {
					args = append(args, "--force")
				}
				cmd := exec.Command("sh", args...)
				for _, entry := range os.Environ() {
					if !strings.HasPrefix(entry, "HOME=") && !strings.HasPrefix(entry, "HERDR_PLUGIN_ROOT=") {
						cmd.Env = append(cmd.Env, entry)
					}
				}
				cmd.Env = append(cmd.Env, "HOME="+home, "HERDR_PLUGIN_ROOT="+root)
				out, err := cmd.CombinedOutput()
				wantSuccess := kind == "absent" || kind == "same symlink" || (force && kind != "directory")
				if (err == nil) != wantSuccess {
					t.Fatalf("success=%v, want %v: %s", err == nil, wantSuccess, out)
				}
				if wantSuccess {
					if target, err := os.Readlink(launcher); err != nil || target != binary {
						t.Fatalf("incorrect launcher: %q, %v", target, err)
					}
				} else {
					switch kind {
					case "file":
						if data, err := os.ReadFile(launcher); err != nil || string(data) != "keep me" {
							t.Fatalf("existing file changed: %q, %v", data, err)
						}
					case "other symlink":
						if target, err := os.Readlink(launcher); err != nil || target != "/missing/unrelated-launcher" {
							t.Fatalf("existing symlink changed: %q, %v", target, err)
						}
					case "directory":
						if info, err := os.Stat(launcher); err != nil || !info.IsDir() {
							t.Fatalf("existing directory changed: %v", err)
						}
					}
				}
			})
		}
	}
}
