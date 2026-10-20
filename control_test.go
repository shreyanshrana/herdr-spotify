package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestPlaybackActions(t *testing.T) {
	tests := []struct {
		action, method, path string
		playing              bool
	}{
		{"prev", "POST", "/me/player/previous", false},
		{"next", "POST", "/me/player/next", false},
		{"play", "PUT", "/me/player/play", false},
		{"pause", "PUT", "/me/player/pause", false},
		{"toggle", "PUT", "/me/player/play", false},
		{"toggle", "PUT", "/me/player/pause", true},
	}
	for _, tt := range tests {
		t.Run(tt.action+tt.path, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == "GET" {
					if tt.action != "toggle" || r.URL.Path != "/me/player" {
						t.Errorf("unexpected state lookup: %s", r.URL.Path)
					}
					if tt.playing {
						w.Write([]byte(`{"is_playing":true}`))
					} else {
						w.Write([]byte(`{"is_playing":false}`))
					}
					return
				}
				calls++
				if r.Method != tt.method || r.URL.Path != tt.path {
					t.Errorf("got %s %s; want %s %s", r.Method, r.URL.Path, tt.method, tt.path)
				}
				w.WriteHeader(204)
			}))
			defer server.Close()
			c := &spotifyClient{http: server.Client(), apiURL: server.URL}
			s := testStore(t, credentials{ClientID: "client", AccessToken: "access", RefreshToken: "refresh", Scope: "user-modify-playback-state", ExpiresAt: time.Now().Add(time.Hour)})
			if err := s.save("status.json", cache{Text: "stale track", NextPoll: time.Now().Add(time.Hour)}); err != nil {
				t.Fatal(err)
			}
			if err := c.control(context.Background(), s, tt.action); err != nil {
				t.Fatal(err)
			}
			var cached cache
			if err := s.load("status.json", &cached); err != nil || !cached.NextPoll.IsZero() || calls != 1 {
				t.Fatalf("control did not invalidate cache: %+v calls=%d err=%v", cached, calls, err)
			}
		})
	}
}

func TestControlRateLimitBlocksFurtherRequests(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Retry-After", "120")
		w.WriteHeader(429)
	}))
	defer server.Close()
	c := &spotifyClient{http: server.Client(), apiURL: server.URL}
	s := testStore(t, credentials{ClientID: "client", AccessToken: "access", RefreshToken: "refresh", Scope: "user-modify-playback-state", ExpiresAt: time.Now().Add(time.Hour)})
	for i := 0; i < 2; i++ {
		if err := c.control(context.Background(), s, "next"); err == nil || !strings.Contains(err.Error(), "rate limited") {
			t.Fatalf("expected rate-limit error: %v", err)
		}
	}
	if calls != 1 {
		t.Fatalf("made %d calls during rate limit", calls)
	}
}

func TestControlRequiresNewScope(t *testing.T) {
	s := testStore(t, credentials{ClientID: "client", RefreshToken: "refresh", Scope: "user-read-playback-state"})
	if err := (&spotifyClient{}).control(context.Background(), s, "next"); err == nil || !strings.Contains(err.Error(), "login again") {
		t.Fatalf("expected re-login hint: %v", err)
	}
}

func TestControlErrors(t *testing.T) {
	for _, tt := range []struct {
		status     int
		body, want string
	}{
		{404, `{}`, "no active device"},
		{403, `{"error":{"reason":"PREMIUM_REQUIRED"}}`, "Premium"},
		{403, `{"error":{"message":"Insufficient client scope"}}`, "login again"},
	} {
		res := httptest.NewRecorder()
		res.WriteHeader(tt.status)
		res.WriteString(tt.body)
		if err := controlResponse(res.Result(), &cache{}); err == nil || !strings.Contains(err.Error(), tt.want) {
			t.Fatalf("expected %q: %v", tt.want, err)
		}
	}
}

func TestToggleWithoutActiveDevice(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(204)
	}))
	defer server.Close()
	c := &spotifyClient{http: server.Client(), apiURL: server.URL}
	auth := credentials{AccessToken: "access", ExpiresAt: time.Now().Add(time.Hour)}
	if err := c.sendControl(context.Background(), &auth, &cache{}, "toggle"); err == nil || !strings.Contains(err.Error(), "no active device") || calls != 1 {
		t.Fatalf("toggle should not send playback request: calls=%d err=%v", calls, err)
	}
}
