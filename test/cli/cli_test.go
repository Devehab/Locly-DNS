// Package clitest runs the real localdns binary as a subprocess, the way
// people and AI agents use it. Every run points at a temporary hosts file.
package clitest

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/devehab/locly-dns/internal/hosts/hoststest"
)

var binary string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "localdns-clitest-*")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	binary = filepath.Join(dir, "localdns")
	if runtime.GOOS == "windows" {
		binary += ".exe"
	}
	build := exec.Command("go", "build", "-o", binary, "../../cmd/localdns")
	build.Stdout, build.Stderr = os.Stdout, os.Stderr
	if err := build.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "build failed:", err)
		os.Exit(1)
	}
	code := m.Run()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}

type sandbox struct {
	t         *testing.T
	hosts     *hoststest.TemporaryHostsFile
	configDir string
	bin       string
}

func newSandbox(t *testing.T) *sandbox {
	return &sandbox{
		t:         t,
		hosts:     hoststest.New(t, hoststest.DefaultContent),
		configDir: filepath.Join(t.TempDir(), "cfg"),
		bin:       binary,
	}
}

type out struct {
	code           int
	stdout, stderr string
}

// run executes the binary with no terminal attached (stdin is empty), exactly
// like an AI agent or CI job would. A timeout guards against any prompt.
func (s *sandbox) run(args ...string) out {
	s.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, s.bin, args...)
	cmd.Env = append(os.Environ(),
		"LOCALDNS_HOSTS_FILE="+s.hosts.Path(),
		"LOCALDNS_CONFIG_DIR="+s.configDir,
		"LOCALDNS_NO_ELEVATE=1",
		"NO_COLOR=1",
	)
	cmd.Stdin = strings.NewReader("")
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	if ctx.Err() != nil {
		s.t.Fatalf("%v timed out (waiting for input?)", args)
	}
	code := 0
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		code = ee.ExitCode()
	} else if err != nil {
		s.t.Fatalf("%v: %v", args, err)
	}
	return out{code, stdout.String(), stderr.String()}
}

func (o out) json(t *testing.T, v any) {
	t.Helper()
	if err := json.Unmarshal([]byte(o.stdout), v); err != nil {
		t.Fatalf("invalid JSON (%v):\n%s", err, o.stdout)
	}
}

// The workflow from the spec: an agent discovers the CLI, adds an entry,
// verifies it, removes it and uninstalls — with no prompts.
func TestAgentWorkflow(t *testing.T) {
	s := newSandbox(t)

	r := s.run("info")
	if r.code != 0 || !strings.Contains(r.stdout, "COMMANDS") {
		t.Fatalf("info: %+v", r)
	}

	r = s.run("list", "--json")
	var list struct {
		Entries []map[string]any `json:"entries"`
	}
	r.json(t, &list)
	if len(list.Entries) != 0 {
		t.Fatalf("expected empty list: %s", r.stdout)
	}

	r = s.run("add", "test.local", "127.0.0.1:3000")
	if r.code != 0 || !strings.Contains(r.stdout, "✓ Added successfully") || !strings.Contains(r.stdout, "http://test.local:3000") {
		t.Fatalf("add: %+v", r)
	}

	r = s.run("list", "--json")
	r.json(t, &list)
	if len(list.Entries) != 1 || list.Entries[0]["hostname"] != "test.local" || list.Entries[0]["status"] != "active" ||
		list.Entries[0]["port"] != float64(3000) || list.Entries[0]["url"] != "http://test.local:3000" {
		t.Fatalf("list --json after add: %s", r.stdout)
	}
	if !strings.Contains(s.hosts.Content(), "# BEGIN LOCALDNS\n127.0.0.1 test.local\n# END LOCALDNS\n") {
		t.Fatalf("hosts file:\n%s", s.hosts.Content())
	}

	// Without --yes and without a terminal: fail fast, never hang.
	r = s.run("remove", "test.local")
	if r.code != 7 || !strings.Contains(r.stderr, "--yes") {
		t.Fatalf("remove without --yes: %+v", r)
	}

	r = s.run("remove", "test.local", "--yes")
	if r.code != 0 {
		t.Fatalf("remove --yes: %+v", r)
	}
	if s.hosts.Content() != hoststest.DefaultContent {
		t.Fatalf("hosts file not restored:\n%s", s.hosts.Content())
	}

	r = s.run("status", "--json")
	var st map[string]any
	r.json(t, &st)
	if r.code != 0 || st["healthy"] != true {
		t.Fatalf("status: %s", r.stdout)
	}
}

func TestEveryCommandHasHelp(t *testing.T) {
	s := newSandbox(t)
	for _, c := range []string{"add", "list", "remove", "info", "status", "doctor", "ui", "uninstall", "version"} {
		r := s.run(c, "--help")
		if r.code != 0 || !strings.Contains(r.stdout, "USAGE") || !strings.Contains(r.stdout, "localdns "+c) {
			t.Errorf("%s --help: %+v", c, r)
		}
	}
}

func TestDoctor(t *testing.T) {
	s := newSandbox(t)
	r := s.run("doctor", "--port", "0")
	if r.code != 0 || !strings.Contains(r.stdout, "Everything looks good.") {
		t.Fatalf("doctor: %+v", r)
	}
	var report struct {
		OK     bool `json:"ok"`
		Checks []struct {
			Name   string `json:"name"`
			Status string `json:"status"`
		} `json:"checks"`
	}
	s.run("doctor", "--json", "--port", "0").json(t, &report)
	if !report.OK || len(report.Checks) != 7 {
		t.Fatalf("doctor --json: %+v", report)
	}
}

func TestErrorsAreMachineReadable(t *testing.T) {
	s := newSandbox(t)
	r := s.run("add", "google.com", "127.0.0.1", "--json")
	var e struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
			Hint    string `json:"hint"`
		} `json:"error"`
	}
	r.json(t, &e)
	if r.code != 2 || e.Error.Code != "invalid_input" || e.Error.Hint == "" {
		t.Fatalf("error JSON: %+v (%d)", e, r.code)
	}
	if s.hosts.Content() != hoststest.DefaultContent {
		t.Fatal("refused add modified the hosts file")
	}
}

func TestUninstallRemovesEverything(t *testing.T) {
	s := newSandbox(t)
	// Uninstall deletes the binary, so use a private copy.
	s.bin = filepath.Join(t.TempDir(), filepath.Base(binary))
	data, err := os.ReadFile(binary)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(s.bin, data, 0o755); err != nil {
		t.Fatal(err)
	}

	s.run("add", "app.local", "127.0.0.1:3000")
	s.run("add", "ha.local", "192.168.1.60")

	r := s.run("uninstall")
	if r.code != 7 {
		t.Fatalf("uninstall without --yes should refuse: %+v", r)
	}
	r = s.run("uninstall", "--yes")
	for _, want := range []string{"✓ LocalDNS entries removed", "✓ Configuration removed", "✓ LocalDNS uninstalled"} {
		if !strings.Contains(r.stdout, want) {
			t.Errorf("missing %q in:\n%s", want, r.stdout)
		}
	}
	if r.code != 0 {
		t.Fatalf("uninstall: %+v", r)
	}
	if s.hosts.Content() != hoststest.DefaultContent {
		t.Fatalf("other hosts entries were modified:\n%s", s.hosts.Content())
	}
	if _, err := os.Stat(s.configDir); !os.IsNotExist(err) {
		t.Fatal("config dir still exists")
	}
	if runtime.GOOS != "windows" { // Windows deletes the running exe asynchronously
		if _, err := os.Stat(s.bin); !os.IsNotExist(err) {
			t.Fatal("binary still exists")
		}
	}
}

func TestUIStartsOnLoopback(t *testing.T) {
	s := newSandbox(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cmd := exec.CommandContext(ctx, s.bin, "ui", "--port", "0", "--json")
	cmd.Env = append(os.Environ(), "LOCALDNS_HOSTS_FILE="+s.hosts.Path(), "LOCALDNS_CONFIG_DIR="+s.configDir)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()
	var started struct {
		URL string `json:"url"`
	}
	if err := json.NewDecoder(stdout).Decode(&started); err != nil {
		t.Fatal(err)
	}
	go func() { _, _ = io.Copy(io.Discard, stdout) }()
	if !strings.HasPrefix(started.URL, "http://127.0.0.1:") {
		t.Fatalf("UI URL %q is not loopback", started.URL)
	}
}
