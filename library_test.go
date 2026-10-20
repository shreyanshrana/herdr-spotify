package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

const testTrackURI = "spotify:track:7a3LWj5xSFhFRYmztS8wgK"

func libraryAuth() credentials {
	return credentials{ClientID: "client", AccessToken: "access", RefreshToken: "refresh", Scope: "user-read-playback-state user-library-read user-library-modify", ExpiresAt: time.Now().Add(time.Hour)}
}

func TestLikeToggleChecksFreshState(t *testing.T) {
	for _, desktop := range []bool{false, true} {
		t.Run(fmt.Sprintf("desktop=%t", desktop), func(t *testing.T) {
			liked, reads, writes, playback := false, 0, 0, 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/me/player":
					playback++
					fmt.Fprintf(w, `{"item":{"name":"Song","uri":%q}}`, testTrackURI)
				case "/me/library/contains":
					reads++
					if r.Method != "GET" || r.URL.Query().Get("uris") != testTrackURI {
						t.Errorf("unexpected lookup: %s %s", r.Method, r.URL)
					}
					fmt.Fprintf(w, "[%t]", liked)
				case "/me/library":
					writes++
					want := "PUT"
					if liked {
						want = "DELETE"
					}
					if r.Method != want || r.URL.Query().Get("uris") != testTrackURI {
						t.Errorf("unexpected mutation: %s %s; want %s", r.Method, r.URL, want)
					}
					liked = !liked
				default:
					t.Errorf("unexpected request: %s", r.URL)
				}
			}))
			defer server.Close()
			c := &spotifyClient{http: server.Client(), apiURL: server.URL}
			if desktop {
				c.desktop = fakeDesktop(fmt.Sprintf(`{"running":true,"state":"paused","name":"Song","uri":%q}`, testTrackURI))
			}
			s := testStore(t, libraryAuth())
			for i := 0; i < 2; i++ {
				if err := c.control(context.Background(), s, "like"); err != nil {
					t.Fatal(err)
				}
				var cached cache
				if err := s.load("status.json", &cached); err != nil || cached.Liked == nil || *cached.Liked != liked || !cached.NextPoll.IsZero() {
					t.Fatalf("bad toggle cache: %+v %v", cached, err)
				}
				// An external change must take precedence over the cached heart.
				if i == 0 {
					liked = false
				}
			}
			// Now exercise unlike with the song saved.
			if err := c.control(context.Background(), s, "like"); err != nil || liked {
				t.Fatalf("unlike failed: liked=%t err=%v", liked, err)
			}
			wantPlayback := 3
			if desktop {
				wantPlayback = 0
			}
			if reads != 3 || writes != 3 || playback != wantPlayback {
				t.Fatalf("reads=%d writes=%d playback=%d", reads, writes, playback)
			}
		})
	}
}

func TestLikeRejectsUnsupportedPlayback(t *testing.T) {
	for _, body := range []string{`{"item":null}`, `{"item":{"name":"Podcast","uri":"spotify:episode:7a3LWj5xSFhFRYmztS8wgK"}}`, `{"item":{"name":"Local","uri":"spotify:local:artist:album:song"}}`} {
		t.Run(body, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				w.Write([]byte(body))
			}))
			defer server.Close()
			c := &spotifyClient{http: server.Client(), apiURL: server.URL}
			if err := c.control(context.Background(), testStore(t, libraryAuth()), "like"); err == nil || calls != 1 {
				t.Fatalf("unsupported playback must not mutate library: calls=%d err=%v", calls, err)
			}
		})
	}
}

func TestLikeRequiresBothScopes(t *testing.T) {
	for _, scope := range []string{"user-library-read", "user-library-modify", "user-modify-playback-state"} {
		auth := libraryAuth()
		auth.Scope = scope
		c := &spotifyClient{desktop: fakeDesktop(`{"running":true,"state":"playing","name":"Song"}`)}
		if err := c.control(context.Background(), testStore(t, auth), "like"); err == nil || !strings.Contains(err.Error(), "login again") {
			t.Fatalf("expected re-login hint with scope %q: %v", scope, err)
		}
	}
}

func TestLikeLookupFailuresDoNotMutate(t *testing.T) {
	for _, tt := range []struct {
		status int
		body   string
	}{
		{200, `[]`}, {200, `null`}, {200, `[true,false]`}, {200, `invalid`}, {204, ``},
		{403, `{"error":{"message":"Insufficient client scope"}}`}, {429, ``},
	} {
		t.Run(fmt.Sprintf("%d/%s", tt.status, tt.body), func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				w.Header().Set("Retry-After", "120")
				w.WriteHeader(tt.status)
				w.Write([]byte(tt.body))
			}))
			defer server.Close()
			c := &spotifyClient{http: server.Client(), apiURL: server.URL, desktop: fakeDesktop(fmt.Sprintf(`{"running":true,"state":"playing","name":"Song","uri":%q}`, testTrackURI))}
			s := testStore(t, libraryAuth())
			if err := c.control(context.Background(), s, "like"); err == nil || calls != 1 {
				t.Fatalf("lookup failure must not mutate: calls=%d err=%v", calls, err)
			}
			if tt.status == 429 {
				if err := c.control(context.Background(), s, "like"); err == nil || !strings.Contains(err.Error(), "rate limited") || calls != 1 {
					t.Fatalf("cooldown not respected: calls=%d err=%v", calls, err)
				}
			}
		})
	}
}

func TestHeartStatusCachesAndRefreshes(t *testing.T) {
	for _, desktop := range []bool{false, true} {
		t.Run(fmt.Sprintf("desktop=%t", desktop), func(t *testing.T) {
			reads, liked := 0, false
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/me/player" {
					fmt.Fprintf(w, `{"item":{"name":"Song","uri":%q}}`, testTrackURI)
					return
				}
				reads++
				fmt.Fprintf(w, "[%t]", liked)
			}))
			defer server.Close()
			c := &spotifyClient{http: server.Client(), apiURL: server.URL}
			if desktop {
				c.desktop = fakeDesktop(fmt.Sprintf(`{"running":true,"state":"playing","name":"Song","uri":%q}`, testTrackURI))
			}
			s := testStore(t, libraryAuth())
			for i := 0; i < 2; i++ {
				text, err := c.status(context.Background(), s, time.Now())
				if err != nil || !strings.Contains(text, "♡ Alt+L") || reads != 1 {
					t.Fatalf("heart lookup not cached: %q reads=%d err=%v", text, reads, err)
				}
			}
			liked = true
			var cached cache
			if err := s.load("status.json", &cached); err != nil {
				t.Fatal(err)
			}
			cached.NextLikePoll, cached.NextPoll = time.Time{}, time.Time{}
			if err := s.save("status.json", cached); err != nil {
				t.Fatal(err)
			}
			text, err := c.status(context.Background(), s, time.Now())
			if err != nil || !strings.Contains(text, "♥ Alt+L") || reads != 2 {
				t.Fatalf("heart not refreshed: %q reads=%d err=%v", text, reads, err)
			}
		})
	}
}

func TestHeartFailureKeepsDesktopPlaybackAndCooldown(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Retry-After", "120")
		w.WriteHeader(429)
	}))
	defer server.Close()
	c := &spotifyClient{http: server.Client(), apiURL: server.URL, desktop: fakeDesktop(fmt.Sprintf(`{"running":true,"state":"playing","name":"Song","uri":%q}`, testTrackURI))}
	s := testStore(t, libraryAuth())
	for i := 0; i < 2; i++ {
		text, err := c.status(context.Background(), s, time.Now())
		if err != nil || !strings.Contains(text, "Song") || strings.Contains(text, "♡") || calls != 1 {
			t.Fatalf("library failure disrupted playback: %q calls=%d err=%v", text, calls, err)
		}
	}
}

func TestLikeMutationFailureAndTokenRotationPersist(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		switch r.URL.Path {
		case "/token":
			w.Write([]byte(`{"access_token":"rotated-access","refresh_token":"rotated-refresh","expires_in":3600}`))
		case "/me/library/contains":
			if r.Header.Get("Authorization") != "Bearer rotated-access" {
				t.Error("rotated access token not used")
			}
			w.Write([]byte(`[false]`))
		case "/me/library":
			w.Header().Set("Retry-After", "120")
			w.WriteHeader(429)
		default:
			t.Errorf("unexpected request: %s", r.URL)
		}
	}))
	defer server.Close()
	auth := libraryAuth()
	auth.ExpiresAt = time.Time{}
	c := &spotifyClient{http: server.Client(), apiURL: server.URL, tokenURL: server.URL + "/token", desktop: fakeDesktop(fmt.Sprintf(`{"running":true,"state":"playing","name":"Song","uri":%q}`, testTrackURI))}
	s := testStore(t, auth)
	for i := 0; i < 2; i++ {
		if err := c.control(context.Background(), s, "like"); err == nil || !strings.Contains(err.Error(), "rate limited") {
			t.Fatalf("expected mutation cooldown: %v", err)
		}
	}
	if err := s.load("auth.json", &auth); err != nil || auth.RefreshToken != "rotated-refresh" {
		t.Fatalf("rotated credentials not saved after failed mutation: %+v %v", auth, err)
	}
	var cached cache
	if err := s.load("status.json", &cached); err != nil || cached.Liked != nil || !cached.RateUntil.Equal(cached.NextPoll) || requests != 3 {
		t.Fatalf("bad mutation failure cache: %+v requests=%d err=%v", cached, requests, err)
	}
}

func TestHeartNeverReusesAnotherSongsState(t *testing.T) {
	newURI := "spotify:track:4aawyAB9vmqN3uQ7FjRGTy"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("uris") != newURI {
			t.Errorf("checked wrong song: %s", r.URL)
		}
		w.Write([]byte(`[false]`))
	}))
	defer server.Close()
	c := &spotifyClient{http: server.Client(), apiURL: server.URL, desktop: fakeDesktop(fmt.Sprintf(`{"running":true,"state":"playing","name":"New song","uri":%q}`, newURI))}
	s := testStore(t, libraryAuth())
	liked := true
	if err := s.save("status.json", cache{LikeURI: testTrackURI, Liked: &liked, NextLikePoll: time.Now().Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	text, err := c.status(context.Background(), s, time.Now())
	if err != nil || !strings.Contains(text, "♡ Alt+L") || strings.Contains(text, "♥") {
		t.Fatalf("old song's heart leaked: %q %v", text, err)
	}
	// A new song during cooldown must omit the previous song's heart.
	if err := s.save("status.json", cache{LikeURI: testTrackURI, Liked: &liked, RateUntil: time.Now().Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	text, err = c.status(context.Background(), s, time.Now())
	if err != nil || strings.Contains(text, "♥") || strings.Contains(text, "♡") || !strings.Contains(text, "New song") {
		t.Fatalf("cooldown leaked old heart: %q %v", text, err)
	}
}

func TestIdleDesktopLikeDoesNotTargetRemoteSong(t *testing.T) {
	apiCalls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		apiCalls++
	}))
	defer server.Close()
	c := &spotifyClient{http: server.Client(), apiURL: server.URL, desktop: fakeDesktop(`{"running":true,"state":"stopped"}`)}
	if err := c.control(context.Background(), testStore(t, libraryAuth()), "like"); err == nil || !strings.Contains(err.Error(), "no current song") || apiCalls != 0 {
		t.Fatalf("idle desktop should be authoritative: calls=%d err=%v", apiCalls, err)
	}
}
