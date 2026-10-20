package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"syscall"
	"time"
)

func (c *spotifyClient) control(ctx context.Context, s *store, action string) error {
	var unlock func()
	for {
		var err error
		unlock, err = s.lock()
		if !errors.Is(err, syscall.EWOULDBLOCK) {
			if err != nil {
				return err
			}
			break
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(50 * time.Millisecond):
		}
	}
	defer unlock()
	if c.desktop != nil && action != "like" {
		if _, err := c.desktop.status(ctx); err == nil {
			return c.desktop.control(ctx, action)
		}
	}
	var cached cache
	if err := s.load("status.json", &cached); err != nil {
		return err
	}
	if time.Now().Before(cached.RateUntil) {
		return fmt.Errorf("Spotify is rate limited; retry in %s", time.Until(cached.RateUntil).Round(time.Second))
	}
	var auth credentials
	if err := s.load("auth.json", &auth); err != nil {
		return err
	}
	if auth.ClientID == "" || auth.RefreshToken == "" {
		return fmt.Errorf("run herdr-spotify login first")
	}
	if action == "like" {
		if !hasScope(auth, "user-library-read") || !hasScope(auth, "user-library-modify") {
			return fmt.Errorf("run herdr-spotify login again to grant like/unlike permission")
		}
	} else if !hasScope(auth, "user-modify-playback-state") {
		return fmt.Errorf("run herdr-spotify login again to grant playback control permission")
	}
	var controlErr error
	if action == "like" {
		controlErr = c.toggleLike(ctx, &auth, &cached)
	} else {
		controlErr = c.sendControl(ctx, &auth, &cached, action)
	}
	if err := s.save("auth.json", auth); err != nil {
		return err
	}
	if controlErr == nil && !time.Now().Before(cached.RateUntil) {
		cached.NextPoll = time.Time{}
		cached.Failures = 0
	}
	if err := s.save("status.json", cached); err != nil {
		return err
	}
	return controlErr
}

func (c *spotifyClient) sendControl(ctx context.Context, auth *credentials, cached *cache, action string) error {
	method, path := http.MethodPut, "/me/player/"+action
	switch action {
	case "prev":
		method, path = http.MethodPost, "/me/player/previous"
	case "next":
		method = http.MethodPost
	case "play", "pause":
	case "toggle":
		res, err := c.request(ctx, auth, http.MethodGet, "/me/player")
		if err != nil {
			return err
		}
		if res.StatusCode != http.StatusOK {
			err := controlResponse(res, cached)
			res.Body.Close()
			if err == nil {
				err = fmt.Errorf("no active device; start playing something on Spotify first")
			}
			return err
		}
		var body struct {
			Playing bool `json:"is_playing"`
		}
		err = json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(&body)
		res.Body.Close()
		if err != nil {
			return err
		}
		path = "/me/player/play"
		if body.Playing {
			path = "/me/player/pause"
		}
	default:
		return fmt.Errorf("unknown playback action %q", action)
	}
	res, err := c.request(ctx, auth, method, path)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	return controlResponse(res, cached)
}

func controlResponse(res *http.Response, cached *cache) error {
	if res.StatusCode >= 200 && res.StatusCode < 300 {
		return nil
	}
	if res.StatusCode == http.StatusTooManyRequests {
		_, delay, _ := playbackResponse(res)
		cached.RateUntil = time.Now().Add(delay)
		cached.NextPoll = cached.RateUntil
		cached.Text = "Spotify · rate limited"
		return fmt.Errorf("Spotify is rate limited; retry in %s", delay)
	}
	var body struct {
		Error struct {
			Message string `json:"message"`
			Reason  string `json:"reason"`
		} `json:"error"`
	}
	_ = json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(&body)
	if res.StatusCode == http.StatusNotFound || body.Error.Reason == "NO_ACTIVE_DEVICE" {
		return fmt.Errorf("no active device; start playing something on Spotify first")
	}
	if body.Error.Reason == "PREMIUM_REQUIRED" || strings.Contains(strings.ToLower(body.Error.Message), "premium") {
		return fmt.Errorf("Spotify Premium is required for playback control")
	}
	if strings.Contains(strings.ToLower(body.Error.Message), "scope") {
		return fmt.Errorf("run herdr-spotify login again to grant playback control permission")
	}
	if body.Error.Message != "" {
		return fmt.Errorf("Spotify HTTP %d: %s", res.StatusCode, clean(body.Error.Message, 140))
	}
	return fmt.Errorf("Spotify HTTP %d", res.StatusCode)
}
