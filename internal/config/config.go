// Package config stores LocalDNS's own metadata: the port associated with
// each hostname (the hosts file cannot hold ports) and when it was added.
//
// The hosts file stays the source of truth for hostname → IP. The config is
// only metadata layered on top of it.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/netip"
	"os"
	"path/filepath"
	"time"

	"github.com/devehab/locly-dns/internal/fsutil"
	"github.com/devehab/locly-dns/internal/validate"
)

// Files LocalDNS creates inside its config directory. Removal only ever
// touches these names, never anything else a user may keep there.
const (
	FileName   = "config.json"
	BackupName = "hosts.backup"
)

// SchemaVersion is the current config file format version.
const SchemaVersion = 1

// Entry is the metadata for one hostname.
type Entry struct {
	Hostname  string    `json:"hostname"`
	IP        string    `json:"ip"`
	Port      uint16    `json:"port,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Config is the content of config.json.
type Config struct {
	Version int     `json:"version"`
	Entries []Entry `json:"entries"`
}

// Find returns the entry for hostname, or nil.
func (c *Config) Find(hostname string) *Entry {
	for i := range c.Entries {
		if c.Entries[i].Hostname == hostname {
			return &c.Entries[i]
		}
	}
	return nil
}

// Put inserts or replaces the entry for e.Hostname.
func (c *Config) Put(e Entry) {
	if old := c.Find(e.Hostname); old != nil {
		*old = e
		return
	}
	c.Entries = append(c.Entries, e)
}

// Delete removes the entry for hostname and reports whether it existed.
func (c *Config) Delete(hostname string) bool {
	for i := range c.Entries {
		if c.Entries[i].Hostname == hostname {
			c.Entries = append(c.Entries[:i], c.Entries[i+1:]...)
			return true
		}
	}
	return false
}

// InvalidError means config.json exists but cannot be used.
type InvalidError struct {
	Path string
	Err  error
}

func (e *InvalidError) Error() string {
	return fmt.Sprintf("configuration file %s is invalid: %v", e.Path, e.Err)
}

func (e *InvalidError) Unwrap() error { return e.Err }

// Store reads and writes the config directory.
type Store struct {
	dir string
}

// NewStore returns a Store for dir.
func NewStore(dir string) *Store { return &Store{dir: dir} }

// Dir is the config directory.
func (s *Store) Dir() string { return s.dir }

// Path is the config file path.
func (s *Store) Path() string { return filepath.Join(s.dir, FileName) }

// Exists reports whether the config file exists.
func (s *Store) Exists() bool {
	_, err := os.Stat(s.Path())
	return err == nil
}

// Load reads the config. A missing file yields an empty config.
func (s *Store) Load() (*Config, error) {
	data, err := os.ReadFile(s.Path())
	if errors.Is(err, fs.ErrNotExist) {
		return &Config{Version: SchemaVersion}, nil
	}
	if err != nil {
		return nil, err
	}
	var c Config
	if err := json.Unmarshal(data, &c); err != nil {
		return nil, &InvalidError{Path: s.Path(), Err: err}
	}
	if err := c.validate(); err != nil {
		return nil, &InvalidError{Path: s.Path(), Err: err}
	}
	return &c, nil
}

func (c *Config) validate() error {
	if c.Version != SchemaVersion {
		return fmt.Errorf("unsupported version %d (expected %d)", c.Version, SchemaVersion)
	}
	seen := map[string]bool{}
	for _, e := range c.Entries {
		h, err := validate.Hostname(e.Hostname)
		if err != nil || h != e.Hostname {
			return fmt.Errorf("invalid hostname %q", e.Hostname)
		}
		if seen[h] {
			return fmt.Errorf("hostname %s is listed more than once", h)
		}
		seen[h] = true
		if _, err := netip.ParseAddr(e.IP); err != nil {
			return fmt.Errorf("invalid IP %q for %s", e.IP, h)
		}
	}
	return nil
}

// Save writes the config atomically (write to a temporary file, then rename).
func (s *Store) Save(c *Config) error {
	c.Version = SchemaVersion
	if c.Entries == nil {
		c.Entries = []Entry{}
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return s.writeFile(FileName, append(data, '\n'))
}

// SaveBackup stores a copy of the hosts file as it was before LocalDNS last
// changed it, so a user can always recover it by hand.
func (s *Store) SaveBackup(hostsContent []byte) error {
	return s.writeFile(BackupName, hostsContent)
}

// BackupPath is where SaveBackup writes.
func (s *Store) BackupPath() string { return filepath.Join(s.dir, BackupName) }

func (s *Store) writeFile(name string, data []byte) error {
	// World-readable so every user can list entries; only admins can write.
	if err := os.MkdirAll(s.dir, 0o755); err != nil { //nolint:gosec // G301: see above
		return err
	}
	tmp, err := os.CreateTemp(s.dir, "."+name+".tmp-*")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp.Name(), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), filepath.Join(s.dir, name))
}

// CheckWritable reports whether Save could succeed, without writing.
func (s *Store) CheckWritable() error { return fsutil.CheckWritable(s.dir) }

// Remove deletes the files LocalDNS created and then the directory itself if
// it is empty. It never deletes files it did not create, so pointing
// --config-dir at a shared directory is safe. It reports whether anything was
// removed.
func (s *Store) Remove() (bool, error) {
	removed := false
	for _, name := range []string{FileName, BackupName} {
		err := os.Remove(filepath.Join(s.dir, name))
		switch {
		case err == nil:
			removed = true
		case !errors.Is(err, fs.ErrNotExist):
			return removed, err
		}
	}
	if entries, err := os.ReadDir(s.dir); err == nil && len(entries) == 0 {
		if err := os.Remove(s.dir); err == nil {
			removed = true
		}
	}
	return removed, nil
}
