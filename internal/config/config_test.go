package config

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadMissingIsEmpty(t *testing.T) {
	s := NewStore(filepath.Join(t.TempDir(), "nested", "localdns"))
	c, err := s.Load()
	if err != nil {
		t.Fatal(err)
	}
	if c.Version != SchemaVersion || len(c.Entries) != 0 {
		t.Fatalf("unexpected config: %+v", c)
	}
	if s.Exists() {
		t.Fatal("Load must not create the file")
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	s := NewStore(filepath.Join(t.TempDir(), "localdns"))
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	c := &Config{}
	c.Put(Entry{Hostname: "app.local", IP: "127.0.0.1", Port: 3000, CreatedAt: now, UpdatedAt: now})
	c.Put(Entry{Hostname: "ha.local", IP: "192.168.1.60", CreatedAt: now, UpdatedAt: now})
	if err := s.Save(c); err != nil {
		t.Fatal(err)
	}
	got, err := s.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Entries) != 2 || got.Find("app.local").Port != 3000 || got.Find("ha.local").Port != 0 {
		t.Fatalf("round trip mismatch: %+v", got)
	}
	got.Put(Entry{Hostname: "app.local", IP: "127.0.0.1", Port: 4000})
	if len(got.Entries) != 2 || got.Find("app.local").Port != 4000 {
		t.Fatal("Put should replace")
	}
	if !got.Delete("app.local") || got.Delete("app.local") || got.Find("app.local") != nil {
		t.Fatal("Delete misbehaved")
	}
}

func TestInvalidConfig(t *testing.T) {
	for name, content := range map[string]string{
		"not json":       "{",
		"wrong version":  `{"version": 99, "entries": []}`,
		"bad hostname":   `{"version": 1, "entries": [{"hostname": "Bad Name", "ip": "127.0.0.1"}]}`,
		"bad ip":         `{"version": 1, "entries": [{"hostname": "a.local", "ip": "nope"}]}`,
		"duplicate host": `{"version": 1, "entries": [{"hostname": "a.local", "ip": "127.0.0.1"}, {"hostname": "a.local", "ip": "127.0.0.1"}]}`,
	} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, FileName), []byte(content), 0o644); err != nil {
				t.Fatal(err)
			}
			_, err := NewStore(dir).Load()
			var inv *InvalidError
			if !errors.As(err, &inv) {
				t.Fatalf("expected InvalidError, got %v", err)
			}
		})
	}
}

func TestRemoveOnlyDeletesOwnFiles(t *testing.T) {
	dir := t.TempDir()
	s := NewStore(dir)
	if err := s.Save(&Config{}); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveBackup([]byte("hosts")); err != nil {
		t.Fatal(err)
	}
	userFile := filepath.Join(dir, "not-ours.txt")
	if err := os.WriteFile(userFile, []byte("keep me"), 0o644); err != nil {
		t.Fatal(err)
	}
	removed, err := s.Remove()
	if err != nil || !removed {
		t.Fatalf("Remove = %v, %v", removed, err)
	}
	if _, err := os.Stat(userFile); err != nil {
		t.Fatal("Remove deleted a file LocalDNS did not create")
	}
	if s.Exists() {
		t.Fatal("config file still exists")
	}
}

func TestRemoveDeletesEmptyDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "localdns")
	s := NewStore(dir)
	if err := s.Save(&Config{}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Remove(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatal("empty config dir should be removed")
	}
	if removed, err := s.Remove(); err != nil || removed {
		t.Fatalf("second Remove = %v, %v", removed, err)
	}
}
