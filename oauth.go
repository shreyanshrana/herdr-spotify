package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os/exec"
	"runtime"
)

func randomToken() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

func callbackHandler(state string, result chan<- string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/callback" {
			http.NotFound(w, r)
			return
		}
		if subtle.ConstantTimeCompare([]byte(r.URL.Query().Get("state")), []byte(state)) != 1 {
			http.Error(w, "Invalid OAuth state", http.StatusBadRequest)
			return
		}
		code := r.URL.Query().Get("code")
		if code == "" && r.URL.Query().Get("error") == "" {
			http.Error(w, "Missing authorization code", http.StatusBadRequest)
			return
		}
		select {
		case result <- code:
			if code == "" {
				http.Error(w, "Spotify sign-in was denied. Retry herdr-spotify login.", http.StatusBadRequest)
			} else {
				fmt.Fprintln(w, "Spotify connected. You can close this tab.")
			}
		default:
			http.Error(w, "Callback already received", http.StatusConflict)
		}
	})
}

func (c *spotifyClient) login(ctx context.Context, clientID string, port int) (credentials, error) {
	auth := credentials{ClientID: clientID}
	listener, err := net.Listen("tcp4", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		return auth, fmt.Errorf("start OAuth callback: %w", err)
	}
	state, verifier := randomToken(), randomToken()
	challenge := sha256.Sum256([]byte(verifier))
	redirect := fmt.Sprintf("http://127.0.0.1:%d/callback", port)
	result := make(chan string, 1)
	server := &http.Server{Handler: callbackHandler(state, result), ReadHeaderTimeout: c.http.Timeout}
	defer server.Close()
	serverErr := make(chan error, 1)
	go func() { serverErr <- server.Serve(listener) }()
	values := url.Values{
		"client_id": {clientID}, "response_type": {"code"}, "redirect_uri": {redirect},
		"scope": {"user-read-currently-playing user-read-playback-state user-modify-playback-state user-library-read user-library-modify"},
		"state": {state}, "code_challenge_method": {"S256"},
		"code_challenge": {base64.RawURLEncoding.EncodeToString(challenge[:])},
	}
	authorize := "https://accounts.spotify.com/authorize?" + values.Encode()
	fmt.Printf("Your Spotify app's Redirect URI must be %s\nOpen this URL to sign in:\n%s\n", redirect, authorize)
	opener := "xdg-open"
	if runtime.GOOS == "darwin" {
		opener = "open"
	}
	if err := exec.CommandContext(ctx, opener, authorize).Run(); err != nil {
		fmt.Println("Could not open the browser automatically; use the URL above.")
	}
	select {
	case code := <-result:
		if code == "" {
			return auth, fmt.Errorf("Spotify sign-in was denied")
		}
		err := c.token(ctx, url.Values{
			"grant_type": {"authorization_code"}, "code": {code}, "client_id": {clientID},
			"redirect_uri": {redirect}, "code_verifier": {verifier},
		}, &auth)
		if err == nil && auth.RefreshToken == "" {
			err = fmt.Errorf("Spotify did not return a refresh token; retry login")
		}
		return auth, err
	case err := <-serverErr:
		return auth, err
	case <-ctx.Done():
		return auth, fmt.Errorf("sign-in timed out: %w", ctx.Err())
	}
}
