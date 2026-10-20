package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestPlaybackResponse(t *testing.T) {
	tests := []struct {
		name, body, want string
		status           int
	}{
		{"track", `{"is_playing":true,"progress_ms":84000,"item":{"name":"Song","duration_ms":232000,"artists":[{"name":"Artist"}]}}`, "⏮ ,   ⏸ Space   ⏭ .  │  Song — Artist · 1:24 / 3:52 · 🌐", 200},
		{"podcast paused", `{"is_playing":false,"progress_ms":61000,"item":{"type":"episode","name":"Episode","duration_ms":3600000,"show":{"name":"Show"}}}`, "⏮ ,   ▶ Space   ⏭ .  │  Episode — Show · 1:01 / 60:00 · 🌐", 200},
		{"no playback", "", "", 204},
		{"no item", `{"item":null}`, "", 200},
		{"controls stripped", "{\"item\":{\"name\":\"A\\nB\\u001bC\",\"duration_ms\":1000}}", "⏮ ,   ▶ Space   ⏭ .  │  A B C · 0:00 / 0:01 · 🌐", 200},
		{"access denied", "", "Spotify · access denied (check app users)", 403},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := httptest.NewRecorder()
			res.WriteHeader(tt.status)
			res.WriteString(tt.body)
			text, _, err := playbackResponse(res.Result())
			if err != nil || text != tt.want {
				t.Fatalf("got %q, %v; want %q", text, err, tt.want)
			}
		})
	}
}

func testStore(t *testing.T, auth credentials) *store {
	t.Helper()
	s := &store{dir: filepath.Join(t.TempDir(), "config")}
	unlock, err := s.lock()
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	if err := s.save("auth.json", auth); err != nil {
		t.Fatal(err)
	}
	return s
}

func TestRefreshAndCacheAcrossInvocations(t *testing.T) {
	refreshes, polls := 0, 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/token" {
			refreshes++
			if err := r.ParseForm(); err != nil {
				t.Error(err)
			}
			if r.Form.Get("refresh_token") != "old-refresh" || r.Form.Get("client_id") != "client" {
				t.Error("incorrect refresh parameters")
			}
			w.Write([]byte(`{"access_token":"new-access","refresh_token":"new-refresh","expires_in":3600}`))
			return
		}
		polls++
		if r.Header.Get("Authorization") != "Bearer new-access" {
			t.Error("refreshed access token was not used")
		}
		w.Write([]byte(`{"is_playing":true,"item":{"name":"Song","duration_ms":1000}}`))
	}))
	defer server.Close()
	c := &spotifyClient{http: server.Client(), apiURL: server.URL, tokenURL: server.URL + "/token"}
	s := testStore(t, credentials{ClientID: "client", RefreshToken: "old-refresh"})
	first, err := c.status(context.Background(), s, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	second, err := c.status(context.Background(), s, time.Now())
	if err != nil || first != second || refreshes != 1 || polls != 1 {
		t.Fatalf("cache not reused: first=%q second=%q refreshes=%d polls=%d err=%v", first, second, refreshes, polls, err)
	}
	var saved credentials
	if err := s.load("auth.json", &saved); err != nil || saved.RefreshToken != "new-refresh" {
		t.Fatalf("rotated refresh token not persisted: %+v %v", saved, err)
	}
	info, err := os.Stat(filepath.Join(s.dir, "auth.json"))
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("credentials must be owner-only: %v %v", info, err)
	}
}

func TestRateLimitPersistsBetweenInvocations(t *testing.T) {
	polls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		polls++
		w.Header().Set("Retry-After", "120")
		w.WriteHeader(429)
	}))
	defer server.Close()
	c := &spotifyClient{http: server.Client(), apiURL: server.URL}
	s := testStore(t, credentials{ClientID: "client", AccessToken: "access", RefreshToken: "refresh", ExpiresAt: time.Now().Add(time.Hour)})
	for i := 0; i < 2; i++ {
		text, err := c.status(context.Background(), s, time.Now())
		if err != nil || !strings.HasPrefix(text, "Spotify · rate limited · retry in ") {
			t.Fatalf("got %q, %v", text, err)
		}
	}
	var cached cache
	if err := s.load("status.json", &cached); err != nil || time.Until(cached.NextPoll) < 115*time.Second || polls != 1 {
		t.Fatalf("rate-limit not persisted: %+v polls=%d err=%v", cached, polls, err)
	}
}

func TestInvalidGrantRequiresLogin(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(400)
		w.Write([]byte(`{"error":"invalid_grant"}`))
	}))
	defer server.Close()
	c := &spotifyClient{http: server.Client(), tokenURL: server.URL}
	s := testStore(t, credentials{ClientID: "client", RefreshToken: "expired"})
	text, err := c.status(context.Background(), s, time.Now())
	if err != nil || text != "Spotify · run herdr-spotify login" {
		t.Fatalf("got %q, %v", text, err)
	}
	var saved credentials
	if err := s.load("auth.json", &saved); err != nil || saved.RefreshToken != "" {
		t.Fatalf("invalid token retained: %+v, %v", saved, err)
	}
}

func TestShortPlaybackRateLimitBlocksPollingAndControls(t *testing.T) {
	for _, retryAfter := range []string{"1", "2", "5"} {
		t.Run(retryAfter, func(t *testing.T) {
			requests := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests++
				w.Header().Set("Retry-After", retryAfter)
				w.WriteHeader(http.StatusTooManyRequests)
			}))
			defer server.Close()
			c := &spotifyClient{http: server.Client(), apiURL: server.URL}
			s := testStore(t, credentials{ClientID: "client", AccessToken: "access", RefreshToken: "refresh", ExpiresAt: time.Now().Add(time.Hour)})
			for i := 0; i < 2; i++ {
				text, err := c.status(context.Background(), s, time.Now())
				if err != nil || !strings.Contains(text, "rate limited") {
					t.Fatalf("expected cooldown, got %q, %v", text, err)
				}
			}
			var cached cache
			if err := s.load("status.json", &cached); err != nil || !cached.RateUntil.After(time.Now()) || !cached.NextPoll.Equal(cached.RateUntil) {
				t.Fatalf("cooldown not persisted: %+v, %v", cached, err)
			}
			if err := c.control(context.Background(), s, "next"); err == nil || !strings.Contains(err.Error(), "rate limited") {
				t.Fatalf("control did not honor cooldown: %v", err)
			}
			if requests != 1 {
				t.Fatalf("expected only the initial request, got %d", requests)
			}
		})
	}
}

func TestUnauthorizedRetriesWithRefreshedToken(t *testing.T) {
	polls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/token" {
			json.NewEncoder(w).Encode(map[string]any{"access_token": "fresh", "expires_in": 3600})
			return
		}
		polls++
		if r.Header.Get("Authorization") != "Bearer fresh" {
			w.WriteHeader(401)
			return
		}
		w.WriteHeader(204)
	}))
	defer server.Close()
	c := &spotifyClient{http: server.Client(), apiURL: server.URL, tokenURL: server.URL + "/token"}
	auth := credentials{ClientID: "client", RefreshToken: "refresh", AccessToken: "rejected", ExpiresAt: time.Now().Add(time.Hour)}
	_, _, err := c.poll(context.Background(), &auth)
	if err != nil || polls != 2 || auth.AccessToken != "fresh" || auth.RefreshToken != "refresh" {
		t.Fatalf("retry failed: %+v polls=%d err=%v", auth, polls, err)
	}
}

func TestInvalidCallbackDoesNotEndLogin(t *testing.T) {
	result := make(chan string, 1)
	handler := callbackHandler("expected", result)
	for _, path := range []string{"/callback?state=wrong&code=bad", "/callback?state=expected", "/favicon.ico"} {
		handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", path, nil))
	}
	select {
	case <-result:
		t.Fatal("invalid callback consumed login")
	default:
	}
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/callback?state=expected&code=good", nil))
	if code := <-result; code != "good" {
		t.Fatalf("got code %q", code)
	}
}

func TestLongUnicodeTitle(t *testing.T) {
	got := clean(strings.Repeat("音", 45), 40)
	if len([]rune(got)) != 40 || !strings.HasSuffix(got, "…") {
		t.Fatalf("bad truncation: %q", got)
	}
}
