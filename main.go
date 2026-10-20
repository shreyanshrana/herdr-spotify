package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "herdr-spotify:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: herdr-spotify login [--client-id ID] [--port 6061] | status | prev | next | toggle | play | pause | like")
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		return err
	}
	// Match Herdr's config location on macOS and Linux.
	if home, err := os.UserHomeDir(); err == nil {
		dir = home + "/.config"
	}
	store := &store{dir: dir + "/herdr-spotify"}
	client := newSpotifyClient()
	switch args[0] {
	case "login":
		flags := flag.NewFlagSet("login", flag.ContinueOnError)
		id := flags.String("client-id", "", "Spotify developer app Client ID")
		port := flags.Int("port", 6061, "OAuth callback port")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		if flags.NArg() != 0 || *port < 1 || *port > 65535 {
			return fmt.Errorf("expected --client-id ID and a port between 1 and 65535")
		}
		if *id == "" {
			fmt.Print("Spotify Client ID: ")
			line, err := bufio.NewReader(os.Stdin).ReadString('\n')
			if err != nil {
				return err
			}
			*id = strings.TrimSpace(line)
		}
		if strings.TrimSpace(*id) == "" {
			return fmt.Errorf("Client ID is required")
		}
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		auth, err := client.login(ctx, strings.TrimSpace(*id), *port)
		if err != nil {
			return err
		}
		unlock, err := store.lock()
		if err != nil {
			return err
		}
		defer unlock()
		if err := store.save("auth.json", auth); err != nil {
			return err
		}
		if err := store.resetPolling(); err != nil {
			return err
		}
		fmt.Println("Spotify connected. Herdr's status bar will update shortly.")
		return nil
	case "status":
		if len(args) != 1 {
			return fmt.Errorf("status accepts no arguments")
		}
		ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
		defer cancel()
		text, err := client.status(ctx, store, time.Now())
		if err != nil {
			return err
		}
		if text != "" {
			fmt.Println(text)
		}
		return nil
	case "prev", "next", "toggle", "play", "pause", "like":
		if len(args) != 1 {
			return fmt.Errorf("%s accepts no arguments", args[0])
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return client.control(ctx, store, args[0])
	default:
		return fmt.Errorf("unknown command %q; use login, status, prev, next, toggle, play, pause, or like", args[0])
	}
}
