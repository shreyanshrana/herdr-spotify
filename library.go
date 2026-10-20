package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

func hasScope(auth credentials, scope string) bool {
	return strings.Contains(" "+auth.Scope+" ", " "+scope+" ")
}

func trackURI(uri string) bool {
	id, ok := strings.CutPrefix(uri, "spotify:track:")
	if !ok || len(id) != 22 {
		return false
	}
	for _, r := range id {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9') {
			return false
		}
	}
	return true
}

func (c *spotifyClient) currentTrackURI(ctx context.Context, auth *credentials, cached *cache) (string, error) {
	if c.desktop != nil {
		text, uri, err := c.desktop.playback(ctx)
		if err == nil {
			if text == "" {
				return "", fmt.Errorf("no current song; start playing something on Spotify first")
			}
			if !trackURI(uri) {
				return "", fmt.Errorf("like/unlike supports Spotify songs only, not podcasts or local files")
			}
			return uri, nil
		}
	}
	res, err := c.request(ctx, auth, http.MethodGet, "/me/player?additional_types=episode")
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	if res.StatusCode == http.StatusNoContent {
		return "", fmt.Errorf("no current song; start playing something on Spotify first")
	}
	if res.StatusCode != http.StatusOK {
		if err := libraryResponse(res, cached); err != nil {
			return "", err
		}
		return "", fmt.Errorf("unexpected Spotify playback response (HTTP %d)", res.StatusCode)
	}
	text, uri, _, err := playbackDetails(res)
	if err != nil {
		return "", err
	}
	if text == "" {
		return "", fmt.Errorf("no current song; start playing something on Spotify first")
	}
	if !trackURI(uri) {
		return "", fmt.Errorf("like/unlike supports Spotify songs only, not podcasts or local files")
	}
	return uri, nil
}

func (c *spotifyClient) savedTrack(ctx context.Context, auth *credentials, cached *cache, uri string) (bool, error) {
	res, err := c.request(ctx, auth, http.MethodGet, "/me/library/contains?"+url.Values{"uris": {uri}}.Encode())
	if err != nil {
		return false, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		if err := libraryResponse(res, cached); err != nil {
			return false, err
		}
		return false, fmt.Errorf("unexpected Spotify liked-state response (HTTP %d)", res.StatusCode)
	}
	var saved []bool
	if err := json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(&saved); err != nil {
		return false, err
	}
	if len(saved) != 1 {
		return false, fmt.Errorf("Spotify returned an invalid liked-state response")
	}
	return saved[0], nil
}

func (c *spotifyClient) toggleLike(ctx context.Context, auth *credentials, cached *cache) error {
	uri, err := c.currentTrackURI(ctx, auth, cached)
	if err != nil {
		return err
	}
	// Always check Spotify before toggling, including changes made in other apps.
	liked, err := c.savedTrack(ctx, auth, cached, uri)
	if err != nil {
		return err
	}
	method := http.MethodPut
	if liked {
		method = http.MethodDelete
	}
	cached.Liked = nil
	cached.NextLikePoll = time.Time{}
	res, err := c.request(ctx, auth, method, "/me/library?"+url.Values{"uris": {uri}}.Encode())
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if err := libraryResponse(res, cached); err != nil {
		return err
	}
	liked = !liked
	cached.LikeURI, cached.Liked = uri, &liked
	cached.NextLikePoll = time.Now().Add(time.Minute)
	return nil
}

func (c *spotifyClient) likeStatus(ctx context.Context, auth *credentials, cached *cache, text, uri string, now time.Time) string {
	if cached.LikeURI != uri {
		cached.LikeURI, cached.Liked = uri, nil
		cached.NextLikePoll = time.Time{}
	}
	if auth.ClientID == "" || auth.RefreshToken == "" || !hasScope(*auth, "user-library-read") {
		return text
	}
	if !now.Before(cached.RateUntil) && !now.Before(cached.NextLikePoll) {
		liked, err := c.savedTrack(ctx, auth, cached, uri)
		cached.NextLikePoll = time.Now().Add(time.Minute)
		cached.Liked = nil
		if err != nil {
			fmt.Fprintln(os.Stderr, "herdr-spotify:", err)
		} else {
			cached.Liked = &liked
		}
	}
	if cached.Liked == nil {
		return text
	}
	icon := "♡"
	if *cached.Liked {
		icon = "♥"
	}
	return strings.Replace(text, "  │  ", "   "+icon+" Alt+L  │  ", 1)
}

func libraryResponse(res *http.Response, cached *cache) error {
	if res.StatusCode >= 200 && res.StatusCode < 300 {
		return nil
	}
	if res.StatusCode == http.StatusTooManyRequests {
		return controlResponse(res, cached)
	}
	var body struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	_ = json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(&body)
	if strings.Contains(strings.ToLower(body.Error.Message), "scope") {
		return fmt.Errorf("run herdr-spotify login again to grant like/unlike permission")
	}
	if body.Error.Message != "" {
		return fmt.Errorf("Spotify HTTP %d: %s", res.StatusCode, clean(body.Error.Message, 140))
	}
	return fmt.Errorf("Spotify HTTP %d", res.StatusCode)
}
