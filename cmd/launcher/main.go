package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"
)

func resolveBinary(ctx context.Context, herdr string) (string, error) {
	out, err := exec.CommandContext(ctx, herdr, "plugin", "list", "--plugin", "herdr-spotify", "--json").Output()
	if err != nil {
		return "", fmt.Errorf("find installed plugin: %w", err)
	}
	var response struct {
		Result struct {
			Plugins []struct {
				ID      string `json:"plugin_id"`
				Root    string `json:"plugin_root"`
				Enabled bool   `json:"enabled"`
			} `json:"plugins"`
		} `json:"result"`
	}
	if err := json.Unmarshal(out, &response); err != nil {
		return "", fmt.Errorf("read plugin registry: %w", err)
	}
	for _, plugin := range response.Result.Plugins {
		if plugin.ID != "herdr-spotify" {
			continue
		}
		if !plugin.Enabled {
			return "", fmt.Errorf("herdr-spotify is disabled; enable it in Herdr first")
		}
		if !filepath.IsAbs(plugin.Root) {
			return "", fmt.Errorf("Herdr returned an invalid plugin directory")
		}
		binary := filepath.Join(plugin.Root, "bin", "herdr-spotify")
		info, err := os.Stat(binary)
		if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0111 == 0 {
			return "", fmt.Errorf("plugin binary is missing or not executable; rebuild or reinstall herdr-spotify")
		}
		return binary, nil
	}
	return "", fmt.Errorf("herdr-spotify is not installed; install or link it in Herdr first")
}

func launch() error {
	herdr := os.Getenv("HERDR_BIN_PATH")
	if herdr == "" {
		herdr = "herdr"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	binary, err := resolveBinary(ctx, herdr)
	if err != nil {
		return err
	}
	return syscall.Exec(binary, append([]string{binary}, os.Args[1:]...), os.Environ())
}

func main() {
	if err := launch(); err != nil {
		fmt.Fprintln(os.Stderr, "herdr-spotify:", err)
		os.Exit(1)
	}
}
