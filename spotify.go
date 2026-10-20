package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unicode"
)

type credentials struct {
	ClientID     string    `json:"client_id"`
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	ExpiresAt    time.Time `json:"expires_at"`
	Scope        string    `json:"scope,omitempty"`
}

type cache struct {
	Text         string    `json:"text"`
	NextPoll     time.Time `json:"next_poll"`
	Failures     int       `json:"failures"`
	RateUntil    time.Time `json:"rate_until,omitempty"`
	LikeURI      string    `json:"like_uri,omitempty"`
	Liked        *bool     `json:"liked,omitempty"`
	NextLikePoll time.Time `json:"next_like_poll,omitempty"`
}

type spotifyClient struct {
	http     *http.Client
	apiURL   string
	tokenURL string
	desktop  *desktopPlayer
}

func newSpotifyClient() *spotifyClient {
	return &spotifyClient{
		http:     &http.Client{Timeout: 5 * time.Second},
		apiURL:   "https://api.spotify.com/v1",
		tokenURL: "https://accounts.spotify.com/api/token",
		desktop:  newDesktopPlayer(),
	}
}

func (c *spotifyClient) token(ctx context.Context, values url.Values, auth *credentials) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.tokenURL, strings.NewReader(values.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	res, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	var body struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int    `json:"expires_in"`
		Error        string `json:"error"`
		Scope        string `json:"scope"`
	}
	if err := json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(&body); err != nil {
		return err
	}
	if body.Error == "invalid_grant" {
		auth.AccessToken, auth.RefreshToken = "", ""
		return errors.New("Spotify session expired; run herdr-spotify login")
	}
	if res.StatusCode != http.StatusOK || body.AccessToken == "" || body.ExpiresIn <= 0 {
		return fmt.Errorf("Spotify sign-in failed (HTTP %d, %s)", res.StatusCode, body.Error)
	}
	auth.AccessToken = body.AccessToken
	if body.Scope != "" {
		auth.Scope = body.Scope
	}
	if body.RefreshToken != "" {
		auth.RefreshToken = body.RefreshToken
	}
	auth.ExpiresAt = time.Now().Add(time.Duration(body.ExpiresIn) * time.Second)
	return nil
}

func (c *spotifyClient) status(ctx context.Context, s *store, now time.Time) (string, error) {
	unlock, err := s.lock()
	if errors.Is(err, syscall.EWOULDBLOCK) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	defer unlock()
	var cached cache
	if err := s.load("status.json", &cached); err != nil {
		return "", err
	}
	if c.desktop != nil {
		text, uri, err := c.desktop.playback(ctx)
		if err == nil {
			if text != "" && trackURI(uri) {
				var auth credentials
				if err := s.load("auth.json", &auth); err != nil {
					return "", err
				}
				text = c.likeStatus(ctx, &auth, &cached, text, uri, now)
				if err := s.save("auth.json", auth); err != nil {
					return "", err
				}
				if err := s.save("status.json", cached); err != nil {
					return "", err
				}
			}
			return text, nil
		}
		if !errors.Is(err, errDesktopNotRunning) {
			fmt.Fprintln(os.Stderr, "herdr-spotify:", err)
		}
	}
	if now.Before(cached.RateUntil) {
		return cooldownStatus(cached, now), nil
	}
	var auth credentials
	if err := s.load("auth.json", &auth); err != nil {
		return "", err
	}
	if auth.RefreshToken == "" || auth.ClientID == "" {
		return "Spotify · run herdr-spotify login", nil
	}
	if now.Before(cached.NextPoll) {
		return cached.Text, nil
	}
	text, uri, delay, pollErr := c.pollPlayback(ctx, &auth, &cached)
	if pollErr == nil && text != "" && trackURI(uri) {
		text = c.likeStatus(ctx, &auth, &cached, text, uri, now)
	}
	// Persist rotated tokens, including invalidated credentials, before releasing the lock.
	if err := s.save("auth.json", auth); err != nil {
		return "", err
	}
	if pollErr != nil {
		cached.Failures = min(cached.Failures+1, 4)
		delay = time.Duration(5*(1<<cached.Failures)) * time.Second
		text = "Spotify · unavailable"
		if auth.RefreshToken == "" {
			text = "Spotify · run herdr-spotify login"
		}
		fmt.Fprintln(os.Stderr, "herdr-spotify:", pollErr)
	} else {
		cached.Failures = 0
		if time.Now().Before(cached.RateUntil) && text == "" {
			text = "Spotify · rate limited"
		}
	}
	cached.Text = text
	cached.NextPoll = time.Now().Add(delay)
	if time.Now().Before(cached.RateUntil) {
		cached.NextPoll = cached.RateUntil
	}
	if err := s.save("status.json", cached); err != nil {
		return "", err
	}
	if time.Now().Before(cached.RateUntil) {
		return cooldownStatus(cached, time.Now()), nil
	}
	return text, nil
}

func (c *spotifyClient) poll(ctx context.Context, auth *credentials) (string, time.Duration, error) {
	text, _, delay, err := c.pollPlayback(ctx, auth, nil)
	return text, delay, err
}

func (c *spotifyClient) pollPlayback(ctx context.Context, auth *credentials, cached *cache) (string, string, time.Duration, error) {
	res, err := c.request(ctx, auth, http.MethodGet, "/me/player?additional_types=episode")
	if err != nil {
		return "", "", 0, err
	}
	defer res.Body.Close()
	text, uri, delay, err := playbackDetails(res)
	if res.StatusCode == http.StatusTooManyRequests && cached != nil {
		cached.RateUntil = time.Now().Add(delay)
	}
	return text, uri, delay, err
}

func (c *spotifyClient) request(ctx context.Context, auth *credentials, method, path string) (*http.Response, error) {
	for attempt := 0; attempt < 2; attempt++ {
		if auth.AccessToken == "" || time.Now().Add(10*time.Second).After(auth.ExpiresAt) {
			if err := c.token(ctx, url.Values{"grant_type": {"refresh_token"}, "refresh_token": {auth.RefreshToken}, "client_id": {auth.ClientID}}, auth); err != nil {
				return nil, err
			}
		}
		req, err := http.NewRequestWithContext(ctx, method, c.apiURL+path, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+auth.AccessToken)
		res, err := c.http.Do(req)
		if err != nil {
			return nil, err
		}
		if res.StatusCode == http.StatusUnauthorized {
			res.Body.Close()
			auth.AccessToken = ""
			continue
		}
		return res, nil
	}
	return nil, errors.New("Spotify rejected the refreshed token")
}

func playbackResponse(res *http.Response) (string, time.Duration, error) {
	text, _, delay, err := playbackDetails(res)
	return text, delay, err
}

func playbackDetails(res *http.Response) (string, string, time.Duration, error) {
	const interval = 5 * time.Second
	switch res.StatusCode {
	case http.StatusNoContent:
		return "", "", interval, nil
	case http.StatusTooManyRequests:
		seconds, err := strconv.Atoi(res.Header.Get("Retry-After"))
		if err != nil || seconds < 1 {
			seconds = 30
		}
		return "", "", time.Duration(min(seconds, 86400)) * time.Second, nil
	case http.StatusForbidden:
		return "Spotify · access denied (check app users)", "", time.Minute, nil
	case http.StatusOK:
	default:
		return "", "", 0, fmt.Errorf("Spotify HTTP %d", res.StatusCode)
	}
	var body struct {
		Playing  bool  `json:"is_playing"`
		Progress int64 `json:"progress_ms"`
		Item     *struct {
			Name     string `json:"name"`
			Type     string `json:"type"`
			URI      string `json:"uri"`
			Duration int64  `json:"duration_ms"`
			Artists  []struct {
				Name string `json:"name"`
			} `json:"artists"`
			Show struct {
				Name string `json:"name"`
			} `json:"show"`
		} `json:"item"`
	}
	if err := json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(&body); err != nil {
		return "", "", 0, err
	}
	if body.Item == nil {
		return "", "", interval, nil
	}
	var artists []string
	for _, artist := range body.Item.Artists {
		artists = append(artists, artist.Name)
	}
	subtitle := strings.Join(artists, ", ")
	if body.Item.Type == "episode" {
		subtitle = body.Item.Show.Name
	}
	return playbackText(body.Item.Name, subtitle, body.Playing, body.Progress, body.Item.Duration) + " · 🌐", body.Item.URI, interval, nil
}

func playbackText(name, subtitle string, playing bool, progress, duration int64) string {
	icon := "⏸"
	if !playing {
		icon = "▶"
	}
	text := "⏮ ,   " + icon + " Space   ⏭ .  │  " + clean(name, 28)
	if subtitle = clean(subtitle, 20); subtitle != "" {
		text += " — " + subtitle
	}
	text += " · " + formatTime(min(progress, duration)) + " / " + formatTime(duration)
	return text
}

func clean(text string, limit int) string {
	text = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || unicode.IsSpace(r) || unicode.Is(unicode.Cf, r) {
			return ' '
		}
		return r
	}, text)
	runes := []rune(strings.Join(strings.Fields(text), " "))
	if len(runes) > limit {
		return string(runes[:limit-1]) + "…"
	}
	return string(runes)
}

func formatTime(ms int64) string {
	seconds := max(ms, 0) / 1000
	return fmt.Sprintf("%d:%02d", seconds/60, seconds%60)
}
