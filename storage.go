package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

type store struct{ dir string }

func (s *store) resetPolling() error {
	var cached cache
	if err := s.load("status.json", &cached); err != nil {
		return err
	}
	cached.Text = ""
	cached.Failures = 0
	cached.NextPoll = time.Time{}
	cached.LikeURI = ""
	cached.Liked = nil
	cached.NextLikePoll = time.Time{}
	if time.Now().Before(cached.RateUntil) {
		cached.NextPoll = cached.RateUntil
	}
	return s.save("status.json", cached)
}

func (s *store) lock() (func(), error) {
	if err := os.MkdirAll(s.dir, 0700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(filepath.Join(s.dir, ".lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		return nil, err
	}
	return func() { f.Close() }, nil
}

func (s *store) load(name string, value any) error {
	data, err := os.ReadFile(filepath.Join(s.dir, name))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	return json.Unmarshal(data, value)
}

func (s *store) save(name string, value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(s.dir, ".tmp-")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), filepath.Join(s.dir, name))
}
