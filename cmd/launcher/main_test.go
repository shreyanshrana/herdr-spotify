package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveFollowsRegistryUpdates(t *testing.T) {
	dir := t.TempDir()
	registry := filepath.Join(dir, "registry.json")
	t.Setenv("TEST_REGISTRY", registry)
	herdr := filepath.Join(dir, "herdr")
	script := "#!/bin/sh\n[ \"$*\" = 'plugin list --plugin herdr-spotify --json' ] || exit 2\ncat \"$TEST_REGISTRY\"\n"
	if err := os.WriteFile(herdr, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	for _, install := range []string{"old install", "new install"} {
		root := filepath.Join(dir, install)
		binary := filepath.Join(root, "bin", "herdr-spotify")
		if err := os.MkdirAll(filepath.Dir(binary), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(binary, []byte("#!/bin/sh\n"), 0700); err != nil {
			t.Fatal(err)
		}
		writeRegistry(t, registry, root, true)
		got, err := resolveBinary(context.Background(), herdr)
		if err != nil || got != binary {
			t.Fatalf("got %q, %v; want %q", got, err, binary)
		}
	}
	for _, tc := range []struct {
		name, root, want string
		enabled          bool
	}{
		{"disabled", filepath.Join(dir, "new install"), "disabled", false},
		{"missing binary", filepath.Join(dir, "removed"), "missing", true},
		{"relative path", "relative", "invalid plugin directory", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			writeRegistry(t, registry, tc.root, tc.enabled)
			if _, err := resolveBinary(context.Background(), herdr); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("expected %q, got %v", tc.want, err)
			}
		})
	}
	for _, data := range []string{`{"result":{"plugins":[]}}`, "not json"} {
		if err := os.WriteFile(registry, []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := resolveBinary(context.Background(), herdr); err == nil {
			t.Fatal("invalid or empty registry must not launch a stale binary")
		}
	}
}

func writeRegistry(t *testing.T, path, root string, enabled bool) {
	t.Helper()
	data, err := json.Marshal(map[string]any{"result": map[string]any{"plugins": []any{
		map[string]any{"plugin_id": "herdr-spotify", "plugin_root": root, "enabled": enabled},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
}
