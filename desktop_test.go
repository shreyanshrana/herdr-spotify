package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func fakeDesktop(output string) *desktopPlayer {
	return &desktopPlayer{run: func(context.Context, string) ([]byte, error) {
		return []byte(output), nil
	}}
}

func TestDesktopPlayback(t *testing.T) {
	for _, tt := range []struct{ name, output, want string }{
		{"playing", `{"running":true,"state":"playing","name":"Song","artist":"Artist","duration":160000,"position":32.149}`, "⏮ ,   ⏸ Space   ⏭ .  │  Song — Artist · 0:32 / 2:40 · 🖥"},
		{"paused", `{"running":true,"state":"paused","name":"Song","duration":160000,"position":32}`, "⏮ ,   ▶ Space   ⏭ .  │  Song · 0:32 / 2:40 · 🖥"},
		{"stopped", `{"running":true,"state":"stopped"}`, ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got, err := fakeDesktop(tt.output).status(context.Background())
			if err != nil || got != tt.want {
				t.Fatalf("got %q, %v; want %q", got, err, tt.want)
			}
		})
	}
}

func TestCooldownUsesDesktopWithoutAPIRequests(t *testing.T) {
	apiCalls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		apiCalls++
		w.WriteHeader(204)
	}))
	defer server.Close()
	localCalls := 0
	c := &spotifyClient{http: server.Client(), apiURL: server.URL, desktop: &desktopPlayer{run: func(context.Context, string) ([]byte, error) {
		localCalls++
		return []byte(`{"running":true,"state":"playing","name":"Local","duration":1000}`), nil
	}}}
	s := testStore(t, credentials{ClientID: "client", RefreshToken: "refresh", AccessToken: "access", ExpiresAt: time.Now().Add(time.Hour)})
	deadline := time.Now().Add(time.Hour)
	if err := s.save("status.json", cache{RateUntil: deadline, NextPoll: deadline}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		got, err := c.status(context.Background(), s, time.Now())
		if err != nil || !strings.Contains(got, "Local") || !strings.HasSuffix(got, "🖥") {
			t.Fatalf("fallback failed: %q %v", got, err)
		}
	}
	// Local controls do not require the Web API's playback scope.
	for _, action := range []string{"prev", "next", "toggle", "play", "pause"} {
		if err := c.control(context.Background(), s, action); err != nil {
			t.Fatal(err)
		}
	}
	var cached cache
	if err := s.load("status.json", &cached); err != nil || !cached.RateUntil.Equal(deadline) || !cached.NextPoll.Equal(deadline) {
		t.Fatalf("local operations changed cooldown: %+v %v", cached, err)
	}
	if apiCalls != 0 || localCalls != 12 {
		t.Fatalf("api calls=%d local calls=%d", apiCalls, localCalls)
	}
	if err := s.save("status.json", cache{RateUntil: time.Now().Add(-time.Second)}); err != nil {
		t.Fatal(err)
	}
	if _, err := c.status(context.Background(), s, time.Now()); err != nil || apiCalls != 0 || localCalls != 13 {
		t.Fatalf("desktop should remain preferred after cooldown: api=%d local=%d err=%v", apiCalls, localCalls, err)
	}
}

func TestDesktopPreferredBeforeAnyAPIRequest(t *testing.T) {
	for _, command := range []string{"status", "next"} {
		t.Run(command, func(t *testing.T) {
			apiCalls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				apiCalls++
				w.Header().Set("Retry-After", "120")
				w.WriteHeader(429)
			}))
			defer server.Close()
			localCalls := 0
			c := &spotifyClient{http: server.Client(), apiURL: server.URL, desktop: &desktopPlayer{run: func(context.Context, string) ([]byte, error) {
				localCalls++
				return []byte(`{"running":true,"state":"playing","name":"Local","duration":1000}`), nil
			}}}
			s := testStore(t, credentials{ClientID: "client", RefreshToken: "refresh", AccessToken: "access", Scope: "user-modify-playback-state", ExpiresAt: time.Now().Add(time.Hour)})
			if command == "status" {
				text, err := c.status(context.Background(), s, time.Now())
				if err != nil || !strings.Contains(text, "Local") {
					t.Fatalf("got %q %v", text, err)
				}
			} else if err := c.control(context.Background(), s, command); err != nil {
				t.Fatal(err)
			}
			var cached cache
			if err := s.load("status.json", &cached); err != nil || !cached.RateUntil.IsZero() {
				t.Fatalf("unexpected cooldown: %+v %v", cached, err)
			}
			wantLocal := 1
			if command != "status" {
				wantLocal = 2
			}
			if apiCalls != 0 || localCalls != wantLocal {
				t.Fatalf("api=%d local=%d", apiCalls, localCalls)
			}
		})
	}
}

func TestDesktopFailurePreservesCooldown(t *testing.T) {
	c := &spotifyClient{desktop: &desktopPlayer{run: func(context.Context, string) ([]byte, error) {
		return nil, errors.New("automation permission denied")
	}}}
	s := testStore(t, credentials{})
	deadline := time.Now().Add(time.Hour)
	if err := s.save("status.json", cache{RateUntil: deadline, NextPoll: deadline}); err != nil {
		t.Fatal(err)
	}
	got, err := c.status(context.Background(), s, time.Now())
	if err != nil || !strings.Contains(got, "rate limited") || !strings.HasSuffix(got, "🌐") {
		t.Fatalf("got %q %v", got, err)
	}
	if err := c.control(context.Background(), s, "toggle"); err == nil || !strings.Contains(err.Error(), "rate limited") {
		t.Fatalf("expected cooldown error, got %v", err)
	}
}

func TestDesktopUnavailableFallsBackToAPI(t *testing.T) {
	for _, unavailable := range []string{"closed", "permission denied"} {
		t.Run(unavailable, func(t *testing.T) {
			apiCalls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				apiCalls++
				if r.Method == "GET" {
					w.Write([]byte(`{"is_playing":true,"item":{"name":"Remote","duration_ms":1000}}`))
					return
				}
				if r.Method != "POST" || r.URL.Path != "/me/player/next" {
					t.Errorf("unexpected control: %s %s", r.Method, r.URL.Path)
				}
				w.WriteHeader(204)
			}))
			defer server.Close()
			desktop := fakeDesktop(`{"running":false}`)
			if unavailable != "closed" {
				desktop.run = func(context.Context, string) ([]byte, error) {
					return nil, errors.New("permission denied")
				}
			}
			c := &spotifyClient{http: server.Client(), apiURL: server.URL, desktop: desktop}
			s := testStore(t, credentials{ClientID: "client", RefreshToken: "refresh", AccessToken: "access", Scope: "user-modify-playback-state", ExpiresAt: time.Now().Add(time.Hour)})
			text, err := c.status(context.Background(), s, time.Now())
			if err != nil || !strings.Contains(text, "Remote") || !strings.HasSuffix(text, "🌐") {
				t.Fatalf("bad API fallback: %q %v", text, err)
			}
			if err := c.control(context.Background(), s, "next"); err != nil || apiCalls != 2 {
				t.Fatalf("bad API control fallback: calls=%d err=%v", apiCalls, err)
			}
		})
	}
}

func TestIdleDesktopDoesNotPollAPIOrRequireLogin(t *testing.T) {
	c := &spotifyClient{desktop: fakeDesktop(`{"running":true,"state":"stopped"}`)}
	s := testStore(t, credentials{})
	text, err := c.status(context.Background(), s, time.Now())
	if err != nil || text != "" {
		t.Fatalf("idle desktop should be authoritative: %q %v", text, err)
	}
	if err := c.control(context.Background(), s, "play"); err != nil {
		t.Fatalf("desktop should not need OAuth: %v", err)
	}
}

func TestFailedDesktopControlIsNotRepeatedThroughAPI(t *testing.T) {
	c := &spotifyClient{desktop: &desktopPlayer{run: func(_ context.Context, script string) ([]byte, error) {
		if strings.Contains(script, "playerState") {
			return []byte(`{"running":true,"state":"stopped"}`), nil
		}
		return nil, errors.New("automation command timed out")
	}}}
	s := testStore(t, credentials{})
	if err := c.control(context.Background(), s, "toggle"); err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("expected desktop command error: %v", err)
	}
}

func TestLoginResetPreservesRateLimit(t *testing.T) {
	s := testStore(t, credentials{})
	deadline := time.Now().Add(time.Hour)
	if err := s.save("status.json", cache{RateUntil: deadline, NextPoll: deadline, Failures: 3, Text: "old"}); err != nil {
		t.Fatal(err)
	}
	if err := s.resetPolling(); err != nil {
		t.Fatal(err)
	}
	var cached cache
	if err := s.load("status.json", &cached); err != nil || !cached.RateUntil.Equal(deadline) || !cached.NextPoll.Equal(deadline) || cached.Failures != 0 || cached.Text != "" {
		t.Fatalf("bad reset: %+v %v", cached, err)
	}
}
