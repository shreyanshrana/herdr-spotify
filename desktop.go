package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"runtime"
	"time"
)

type desktopPlayer struct {
	run func(context.Context, string) ([]byte, error)
}

var errDesktopNotRunning = errors.New("Spotify desktop app is not running")

func newDesktopPlayer() *desktopPlayer {
	if runtime.GOOS != "darwin" {
		return nil
	}
	return &desktopPlayer{run: func(ctx context.Context, script string) ([]byte, error) {
		ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
		defer cancel()
		out, err := exec.CommandContext(ctx, "/usr/bin/osascript", "-l", "JavaScript", "-e", script).CombinedOutput()
		if err != nil {
			return nil, fmt.Errorf("Spotify desktop automation: %w (%s)", err, clean(string(out), 160))
		}
		return out, nil
	}}
}

const desktopStatusScript = `
const spotify = Application("com.spotify.client");
if (!spotify.running()) {
    JSON.stringify({running:false});
} else {
    const state = spotify.playerState();
    if (state === "stopped") {
        JSON.stringify({running:true, state:state});
    } else {
        const track = spotify.currentTrack();
        JSON.stringify({running:true, state:state, name:track.name(), artist:track.artist(), uri:track.id(), duration:track.duration(), position:spotify.playerPosition()});
    }
}`

func (d *desktopPlayer) status(ctx context.Context) (string, error) {
	text, _, err := d.playback(ctx)
	return text, err
}

func (d *desktopPlayer) playback(ctx context.Context) (string, string, error) {
	out, err := d.run(ctx, desktopStatusScript)
	if err != nil {
		return "", "", err
	}
	var body struct {
		Running  bool    `json:"running"`
		State    string  `json:"state"`
		Name     string  `json:"name"`
		Artist   string  `json:"artist"`
		URI      string  `json:"uri"`
		Duration int64   `json:"duration"`
		Position float64 `json:"position"`
	}
	if err := json.Unmarshal(out, &body); err != nil {
		return "", "", fmt.Errorf("read Spotify desktop playback: %w", err)
	}
	if !body.Running {
		return "", "", errDesktopNotRunning
	}
	if body.State == "stopped" || body.Name == "" {
		return "", "", nil
	}
	// Spotify returns duration in milliseconds despite its scripting dictionary.
	return playbackText(body.Name, body.Artist, body.State == "playing", int64(body.Position*1000), body.Duration) + " · 🖥", body.URI, nil
}

func (d *desktopPlayer) control(ctx context.Context, action string) error {
	commands := map[string]string{
		"prev": "previousTrack", "next": "nextTrack", "toggle": "playpause", "play": "play", "pause": "pause",
	}
	command, ok := commands[action]
	if !ok {
		return fmt.Errorf("unknown playback action %q", action)
	}
	script := `const spotify = Application("com.spotify.client");
if (!spotify.running()) { throw new Error("Open the Spotify desktop app first"); }
spotify.` + command + `();`
	_, err := d.run(ctx, script)
	return err
}

func cooldownStatus(cached cache, now time.Time) string {
	remaining := cached.RateUntil.Sub(now).Round(time.Second)
	return "Spotify · rate limited · retry in " + remaining.String() + " · 🌐"
}
