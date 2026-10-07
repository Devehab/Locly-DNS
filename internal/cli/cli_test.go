package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/devehab/locly-dns/internal/hosts"
	"github.com/devehab/locly-dns/internal/hosts/hoststest"
)

type harness struct {
	t         *testing.T
	hosts     *hoststest.TemporaryHostsFile
	configDir string
	elevator  *fakeElevator
	exe       string
	removed   []string
	readOnly  bool
}

func newHarness(t *testing.T) *harness {
	return &harness{
		t:         t,
		hosts:     hoststest.New(t, hoststest.DefaultContent),
		configDir: filepath.Join(t.TempDir(), "cfg"),
		elevator:  &fakeElevator{},
		exe:       filepath.Join(t.TempDir(), "bin", "localdns"),
	}
}

type result struct {
	code   int
	stdout string
	stderr string
}

func (r result) json(t *testing.T) map[string]any {
	t.Helper()
	var v map[string]any
	if err := json.Unmarshal([]byte(r.stdout), &v); err != nil {
		t.Fatalf("stdout is not JSON: %v\n%s", err, r.stdout)
	}
	return v
}

type runOpt func(*Env)

func interactive(stdin string) runOpt {
	return func(e *Env) {
		e.Interactive = true
		e.Stdin = strings.NewReader(stdin)
	}
}

func (h *harness) run(args []string, opts ...runOpt) result {
	h.t.Helper()
	var stdout, stderr bytes.Buffer
	env := Env{
		Args:   args,
		Stdout: &stdout,
		Stderr: &stderr,
		Getenv: func(k string) string {
			switch k {
			case "LOCALDNS_HOSTS_FILE":
				return h.hosts.Path()
			case "LOCALDNS_CONFIG_DIR":
				return h.configDir
			}
			return ""
		},
		Elevator:     h.elevator,
		Executable:   h.exe,
		RemoveBinary: func(p string) error { h.removed = append(h.removed, p); return nil },
		HostsFile: func(p string) hosts.File {
			if h.readOnly {
				return readOnly{hosts.NewDiskFile(p)}
			}
			return hosts.NewDiskFile(p)
		},
	}
	for _, o := range opts {
		o(&env)
	}
	code := Run(env)
	return result{code: code, stdout: stdout.String(), stderr: stderr.String()}
}

type readOnly struct{ hosts.File }

func (readOnly) CheckWritable() error {
	return &fs.PathError{Op: "open", Path: "hosts", Err: fs.ErrPermission}
}

type fakeElevator struct {
	available bool
	calls     [][]string
	exitCode  int
}

func (f *fakeElevator) Available(bool) bool { return f.available }
func (f *fakeElevator) Describe() string    { return "sudo" }
func (f *fakeElevator) Run(exe string, args []string, _ bool, _ io.Reader, _, _ io.Writer) (int, error) {
	f.calls = append(f.calls, append([]string{exe}, args...))
	return f.exitCode, nil
}

func TestInfo(t *testing.T) {
	h := newHarness(t)
	for _, args := range [][]string{{"info"}, {}, {"--help"}, {"help"}} {
		r := h.run(args)
		if r.code != 0 {
			t.Fatalf("%v: exit %d", args, r.code)
		}
		for _, want := range []string{"LocalDNS", "Local hostname manager for developers.", "VERSION", "COMMANDS",
			"add          Add a hostname", "uninstall    Remove LocalDNS", "EXAMPLES",
			"localdns add app.local 127.0.0.1:3000", "EXIT CODES"} {
			if !strings.Contains(r.stdout, want) {
				t.Errorf("%v: info output missing %q", args, want)
			}
		}
	}
	r := h.run([]string{"info", "--json"})
	v := r.json(t)
	cmds, _ := v["commands"].([]any)
	if r.code != 0 || v["name"] != "localdns" || len(cmds) < 8 {
		t.Fatalf("info --json = %v", v)
	}
}

func TestHelpForEveryCommand(t *testing.T) {
	h := newHarness(t)
	for _, cmd := range commands {
		for _, args := range [][]string{{cmd.name, "--help"}, {cmd.name, "-h"}, {"help", cmd.name}} {
			r := h.run(args)
			if r.code != 0 || !strings.Contains(r.stdout, "USAGE") || !strings.Contains(r.stdout, cmd.usage) {
				t.Errorf("%v: exit %d\n%s", args, r.code, r.stdout)
			}
		}
	}
}

func TestVersion(t *testing.T) {
	h := newHarness(t)
	for _, args := range [][]string{{"version"}, {"--version"}} {
		if r := h.run(args); r.code != 0 || !strings.HasPrefix(r.stdout, "localdns ") {
			t.Errorf("%v: %+v", args, r)
		}
	}
}

func TestAddListRemoveHuman(t *testing.T) {
	h := newHarness(t)
	r := h.run([]string{"add", "app.local", "127.0.0.1:3000"})
	want := "✓ Added successfully\n\napp.local → 127.0.0.1:3000\n\nURL:\nhttp://app.local:3000\n"
	if r.code != 0 || r.stdout != want {
		t.Fatalf("add output:\n%q\nwant:\n%q", r.stdout, want)
	}
	r = h.run([]string{"add", "ha.local", "192.168.1.60"})
	if r.code != 0 || r.stdout != "✓ Added successfully\n\nha.local → 192.168.1.60\n" {
		t.Fatalf("add without port:\n%q", r.stdout)
	}
	r = h.run([]string{"list"})
	for _, want := range []string{"HOSTNAME", "ADDRESS", "STATUS", "app.local", "127.0.0.1:3000", "ha.local", "✓", "2 entries"} {
		if !strings.Contains(r.stdout, want) {
			t.Errorf("list missing %q:\n%s", want, r.stdout)
		}
	}
	r = h.run([]string{"remove", "app.local", "--yes"})
	if r.code != 0 || !strings.Contains(r.stdout, "Removed") {
		t.Fatalf("remove: %+v", r)
	}
	if strings.Contains(h.hosts.Content(), "app.local") {
		t.Fatal("app.local still in hosts file")
	}
}

func TestJSONOutput(t *testing.T) {
	h := newHarness(t)
	r := h.run([]string{"add", "app.local", "127.0.0.1:3000", "--json"})
	v := r.json(t)
	entry := v["entry"].(map[string]any)
	if r.code != 0 || v["action"] != "added" || entry["url"] != "http://app.local:3000" {
		t.Fatalf("add --json = %v", v)
	}
	h.run([]string{"add", "ha.local", "192.168.1.60:8123"})

	r = h.run([]string{"list", "--json"})
	v = r.json(t)
	entries := v["entries"].([]any)
	if len(entries) != 2 {
		t.Fatalf("list --json = %v", v)
	}
	ha := entries[1].(map[string]any)
	for k, want := range map[string]any{
		"hostname": "ha.local", "ip": "192.168.1.60", "port": float64(8123),
		"url": "http://ha.local:8123", "status": "active",
	} {
		if ha[k] != want {
			t.Errorf("entry[%s] = %v, want %v", k, ha[k], want)
		}
	}

	r = h.run([]string{"status", "--json"})
	v = r.json(t)
	if r.code != 0 || v["healthy"] != true || v["section"] != "present" {
		t.Fatalf("status --json = %v", v)
	}
	r = h.run([]string{"doctor", "--json", "--port", "0"})
	v = r.json(t)
	if r.code != 0 || v["ok"] != true || len(v["checks"].([]any)) != 8 {
		t.Fatalf("doctor --json = %v", v)
	}
	r = h.run([]string{"remove", "ha.local", "--yes", "--json"})
	if v := r.json(t); r.code != 0 || v["action"] != "removed" {
		t.Fatalf("remove --json = %v", v)
	}
}

func TestEmptyListJSON(t *testing.T) {
	h := newHarness(t)
	r := h.run([]string{"list", "--json"})
	if r.code != 0 || strings.TrimSpace(r.stdout) != `{
  "entries": []
}` {
		t.Fatalf("empty list --json = %q", r.stdout)
	}
}

func TestExitCodesAndJSONErrors(t *testing.T) {
	h := newHarness(t)
	h.run([]string{"add", "app.local", "127.0.0.1:3000"})
	cases := []struct {
		args []string
		code int
		err  string
	}{
		{[]string{"bogus"}, ExitUsage, "usage"},
		{[]string{"add", "a.local"}, ExitUsage, "usage"},
		{[]string{"add", "a.local", "127.0.0.1", "extra"}, ExitUsage, "usage"},
		{[]string{"list", "--nope"}, ExitUsage, "usage"},
		{[]string{"add", "google.com", "127.0.0.1"}, ExitUsage, "invalid_input"},
		{[]string{"add", "x.local", "8.8.8.8"}, ExitUsage, "invalid_input"},
		{[]string{"remove", "nope.local", "--yes"}, ExitNotFound, "not_found"},
		{[]string{"add", "app.local", "127.0.0.1:4000"}, ExitConflict, "already_exists"},
		{[]string{"add", "printer.lan", "127.0.0.1"}, ExitConflict, "conflict"},
		{[]string{"remove", "printer.lan", "--yes"}, ExitConflict, "not_managed"},
		{[]string{"remove", "app.local"}, ExitNotConfirmed, "confirmation_required"},
	}
	for _, c := range cases {
		r := h.run(append(c.args, "--json"))
		if r.code != c.code {
			t.Errorf("%v: exit %d, want %d (%s)", c.args, r.code, c.code, r.stdout)
			continue
		}
		v := r.json(t)
		e, _ := v["error"].(map[string]any)
		if e["code"] != c.err || e["message"] == "" {
			t.Errorf("%v: error = %v, want code %s", c.args, v, c.err)
		}
		// Human mode: same exit code, message on stderr, nothing on stdout.
		r = h.run(c.args)
		if r.code != c.code || r.stdout != "" || !strings.HasPrefix(r.stderr, "✗ ") {
			t.Errorf("%v human: %+v", c.args, r)
		}
	}
}

func TestMalformedHostsExitCode(t *testing.T) {
	h := newHarness(t)
	h.hosts.SetContent("# BEGIN LOCALDNS\n127.0.0.1 a.local\n")
	r := h.run([]string{"add", "b.local", "127.0.0.1", "--json"})
	if r.code != ExitInvalidState {
		t.Fatalf("exit %d: %s", r.code, r.stdout)
	}
	r = h.run([]string{"doctor", "--port", "0"})
	if r.code != ExitError || !strings.Contains(r.stdout, "✗ LocalDNS section valid") || !strings.Contains(r.stdout, "Fix:") {
		t.Fatalf("doctor on malformed file: %d\n%s", r.code, r.stdout)
	}
}

func TestRemoveConfirmation(t *testing.T) {
	h := newHarness(t)
	h.run([]string{"add", "app.local", "127.0.0.1:3000"})
	content := h.hosts.Content()

	// Declined: nothing changes.
	for _, answer := range []string{"n\n", "\n", "", "nope\n"} {
		r := h.run([]string{"remove", "app.local"}, interactive(answer))
		if r.code != ExitNotConfirmed || !strings.Contains(r.stdout, "Remove app.local → 127.0.0.1:3000?\n\n[y/N] ") {
			t.Fatalf("answer %q: %+v", answer, r)
		}
		if h.hosts.Content() != content {
			t.Fatal("declined remove changed the hosts file")
		}
	}

	// Without a terminal it never prompts.
	r := h.run([]string{"remove", "app.local"})
	if r.code != ExitNotConfirmed || strings.Contains(r.stdout, "[y/N]") || !strings.Contains(r.stderr, "--yes") {
		t.Fatalf("non-interactive: %+v", r)
	}

	// Accepted.
	r = h.run([]string{"remove", "app.local"}, interactive("y\n"))
	if r.code != 0 || strings.Contains(h.hosts.Content(), "app.local") {
		t.Fatalf("confirmed remove: %+v", r)
	}
}

func TestFlagsAnywhere(t *testing.T) {
	h := newHarness(t)
	if r := h.run([]string{"--json", "add", "app.local", "127.0.0.1:3000"}); r.code != 0 || !strings.Contains(r.stdout, `"action"`) {
		t.Fatalf("flag before command: %+v", r)
	}
	if r := h.run([]string{"remove", "--yes", "app.local"}); r.code != 0 {
		t.Fatalf("flag before argument: %+v", r)
	}
}

func TestElevation(t *testing.T) {
	h := newHarness(t)
	h.run([]string{"add", "app.local", "127.0.0.1:3000"})
	h.readOnly = true

	// No elevation available: permission error with a sudo hint.
	r := h.run([]string{"add", "api.local", "127.0.0.1:3002"})
	if r.code != ExitPermission || !strings.Contains(r.stderr, "administrator rights") {
		t.Fatalf("no elevator: %+v", r)
	}

	// Elevation available: the command is re-run with explicit paths, and
	// remove passes --yes so the user is not asked twice.
	h.elevator.available = true
	h.elevator.exitCode = 0
	r = h.run([]string{"remove", "app.local"}, interactive("y\n"))
	if r.code != 0 || len(h.elevator.calls) != 1 {
		t.Fatalf("elevated remove: %+v calls=%v", r, h.elevator.calls)
	}
	call := strings.Join(h.elevator.calls[0], " ")
	for _, want := range []string{h.exe + " remove app.local", "--yes",
		"--hosts-file=" + h.hosts.Path(), "--config-dir=" + h.configDir, "--no-elevate"} {
		if !strings.Contains(call, want) {
			t.Errorf("elevated call %q missing %q", call, want)
		}
	}
	if !strings.Contains(r.stderr, "administrator rights") {
		t.Errorf("interactive elevation should explain itself: %q", r.stderr)
	}

	// The elevated command's exit code is propagated.
	h.elevator.exitCode = 4
	if r := h.run([]string{"add", "x.local", "127.0.0.1"}); r.code != 4 {
		t.Fatalf("exit code not propagated: %d", r.code)
	}

	// --no-elevate and LOCALDNS_NO_ELEVATE opt out.
	calls := len(h.elevator.calls)
	if r := h.run([]string{"add", "x.local", "127.0.0.1", "--no-elevate"}); r.code != ExitPermission {
		t.Fatalf("--no-elevate: %+v", r)
	}
	r = h.run([]string{"add", "x.local", "127.0.0.1"}, func(e *Env) {
		get := e.Getenv
		e.Getenv = func(k string) string {
			if k == EnvNoElevate {
				return "1"
			}
			return get(k)
		}
	})
	if r.code != ExitPermission || len(h.elevator.calls) != calls {
		t.Fatalf("LOCALDNS_NO_ELEVATE: %+v", r)
	}

	// Reads never need elevation.
	if r := h.run([]string{"list", "--json"}); r.code != 0 || len(h.elevator.calls) != calls {
		t.Fatalf("list should not elevate: %+v", r)
	}
}

func TestUninstall(t *testing.T) {
	h := newHarness(t)
	h.run([]string{"add", "app.local", "127.0.0.1:3000"})
	h.run([]string{"add", "ha.local", "192.168.1.60"})

	// Declined.
	r := h.run([]string{"uninstall"}, interactive("n\n"))
	if r.code != ExitNotConfirmed || !strings.Contains(r.stdout, "Your other hosts entries will not be modified.") {
		t.Fatalf("declined uninstall: %+v", r)
	}
	if len(h.removed) != 0 || !strings.Contains(h.hosts.Content(), "app.local") {
		t.Fatal("declined uninstall changed something")
	}

	r = h.run([]string{"uninstall"}, interactive("y\n"))
	for _, want := range []string{"Uninstall LocalDNS?", "• Remove LocalDNS-managed host entries",
		"• Remove LocalDNS configuration", "• Remove the LocalDNS application", "Continue?",
		"✓ LocalDNS entries removed", "✓ Configuration removed", "✓ LocalDNS uninstalled"} {
		if !strings.Contains(r.stdout, want) {
			t.Errorf("uninstall output missing %q:\n%s", want, r.stdout)
		}
	}
	if r.code != 0 {
		t.Fatalf("uninstall exit %d: %s", r.code, r.stderr)
	}
	if h.hosts.Content() != hoststest.DefaultContent {
		t.Fatalf("uninstall did not restore the hosts file:\n%s", h.hosts.Content())
	}
	if _, err := os.Stat(h.configDir); !os.IsNotExist(err) {
		t.Fatal("config dir not removed")
	}
	if len(h.removed) != 1 || h.removed[0] != h.exe {
		t.Fatalf("binary removal = %v", h.removed)
	}
}

func TestUninstallJSONKeepBinary(t *testing.T) {
	h := newHarness(t)
	h.run([]string{"add", "app.local", "127.0.0.1:3000"})
	r := h.run([]string{"uninstall", "--yes", "--json", "--keep-binary"})
	v := r.json(t)
	if r.code != 0 || v["entries_removed"] != float64(1) || v["config_removed"] != true || v["binary_removed"] != false {
		t.Fatalf("uninstall --json = %v", v)
	}
	if len(h.removed) != 0 {
		t.Fatal("--keep-binary removed the binary")
	}
	if r := h.run([]string{"uninstall"}); r.code != ExitNotConfirmed {
		t.Fatalf("uninstall without --yes in non-interactive mode: %+v", r)
	}
}

func TestUI(t *testing.T) {
	h := newHarness(t)
	h.run([]string{"add", "app.local", "127.0.0.1:3000"})
	pr, pw := io.Pipe()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan int, 1)
	go func() {
		var stderr bytes.Buffer
		code := Run(Env{
			Args:    []string{"ui", "--port", "0", "--json"},
			Stdout:  pw,
			Stderr:  &stderr,
			Context: ctx,
			Getenv: func(k string) string {
				return map[string]string{"LOCALDNS_HOSTS_FILE": h.hosts.Path(), "LOCALDNS_CONFIG_DIR": h.configDir}[k]
			},
		})
		_ = pw.Close()
		done <- code
	}()

	var started struct {
		URL      string `json:"url"`
		Writable bool   `json:"writable"`
	}
	if err := json.NewDecoder(pr).Decode(&started); err != nil {
		t.Fatal(err)
	}
	go func() { _, _ = io.Copy(io.Discard, pr) }()
	if !strings.HasPrefix(started.URL, "http://127.0.0.1:") || !started.Writable {
		t.Fatalf("ui start = %+v", started)
	}
	resp, err := http.Get(started.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(body), "LocalDNS") {
		t.Fatalf("GET / = %d", resp.StatusCode)
	}
	cancel()
	select {
	case code := <-done:
		if code != 0 {
			t.Fatalf("ui exit %d", code)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("ui did not stop")
	}
}

func TestQuoteArgs(t *testing.T) {
	if got := quoteArgs([]string{"add", "a.local", "it's here"}); got != `add a.local 'it'\''s here'` {
		t.Fatalf("got %s", got)
	}
}

func TestUIOpensBrowserForPeopleOnly(t *testing.T) {
	h := newHarness(t)
	cases := []struct {
		name        string
		args        []string
		interactive bool
		wantOpen    bool
	}{
		{"terminal", []string{"ui", "--port", "0"}, true, true},
		{"terminal --no-open", []string{"ui", "--port", "0", "--no-open"}, true, false},
		{"script", []string{"ui", "--port", "0"}, false, false},
		{"script --open", []string{"ui", "--port", "0", "--open"}, false, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var opened []string
			var mu sync.Mutex
			pr, pw := io.Pipe()
			ctx, cancel := context.WithCancel(context.Background())
			done := make(chan int, 1)
			go func() {
				done <- Run(Env{
					Args:        tc.args,
					Stdout:      pw,
					Stderr:      io.Discard,
					Context:     ctx,
					Interactive: tc.interactive,
					Getenv: func(k string) string {
						return map[string]string{"LOCALDNS_HOSTS_FILE": h.hosts.Path(), "LOCALDNS_CONFIG_DIR": h.configDir}[k]
					},
					OpenBrowser: func(u string) error { mu.Lock(); opened = append(opened, u); mu.Unlock(); return nil },
				})
				_ = pw.Close()
			}()
			// Wait until the server reports that it is running.
			buf := make([]byte, 4096)
			var got strings.Builder
			for !strings.Contains(got.String(), "Press Ctrl+C") {
				n, err := pr.Read(buf)
				got.Write(buf[:n])
				if err != nil {
					t.Fatalf("ui ended early: %v\n%s", err, got.String())
				}
			}
			go func() { _, _ = io.Copy(io.Discard, pr) }()
			cancel()
			if code := <-done; code != 0 {
				t.Fatalf("exit %d", code)
			}
			mu.Lock()
			defer mu.Unlock()
			if tc.wantOpen != (len(opened) == 1) {
				t.Fatalf("opened = %v, want open=%v", opened, tc.wantOpen)
			}
			if tc.wantOpen && !strings.HasPrefix(opened[0], "http://127.0.0.1:") {
				t.Fatalf("opened %q", opened[0])
			}
		})
	}
}

func TestUIAsksForAdminRightsWhenReadOnly(t *testing.T) {
	h := newHarness(t)
	h.readOnly = true
	h.elevator.available = true
	r := h.run([]string{"ui", "--port", "7399"}, interactive(""))
	if r.code != 0 || len(h.elevator.calls) != 1 {
		t.Fatalf("ui should re-run elevated: %+v calls=%v", r, h.elevator.calls)
	}
	call := strings.Join(h.elevator.calls[0], " ")
	for _, want := range []string{" ui --port 7399", "--no-open", "--no-elevate", "--hosts-file=" + h.hosts.Path()} {
		if !strings.Contains(call, want) {
			t.Errorf("elevated call %q missing %q", call, want)
		}
	}

	// --no-elevate and non-interactive sessions start read-only instead.
	for _, opts := range [][]string{{"ui", "--port", "0", "--no-elevate", "--json"}} {
		calls := len(h.elevator.calls)
		pr, pw := io.Pipe()
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan int, 1)
		go func() {
			done <- Run(Env{Args: opts, Stdout: pw, Stderr: io.Discard, Context: ctx, Interactive: true,
				Elevator: h.elevator,
				Getenv: func(k string) string {
					return map[string]string{"LOCALDNS_HOSTS_FILE": h.hosts.Path(), "LOCALDNS_CONFIG_DIR": h.configDir}[k]
				},
				HostsFile: func(p string) hosts.File { return readOnly{hosts.NewDiskFile(p)} },
			})
			_ = pw.Close()
		}()
		var started struct {
			Writable bool `json:"writable"`
		}
		if err := json.NewDecoder(pr).Decode(&started); err != nil {
			t.Fatal(err)
		}
		go func() { _, _ = io.Copy(io.Discard, pr) }()
		cancel()
		<-done
		if started.Writable || len(h.elevator.calls) != calls {
			t.Fatalf("%v: expected a read-only UI without elevation", opts)
		}
	}
}

func TestEdit(t *testing.T) {
	h := newHarness(t)
	h.run([]string{"add", "app.local", "127.0.0.1:3000"})

	r := h.run([]string{"edit", "app.local", "http://127.0.0.1:4000/"})
	if r.code != 0 || !strings.Contains(r.stdout, "Updated successfully") || !strings.Contains(r.stdout, "app.local → 127.0.0.1:4000") {
		t.Fatalf("edit address: %+v", r)
	}
	r = h.run([]string{"edit", "app.local", "--name", "web.local", "--json"})
	v := r.json(t)
	if r.code != 0 || v["action"] != "updated" || v["entry"].(map[string]any)["hostname"] != "web.local" {
		t.Fatalf("edit --name: %+v", r)
	}
	if r := h.run([]string{"edit", "web.local", "127.0.0.1:4000"}); !strings.Contains(r.stdout, "Nothing to change") {
		t.Fatalf("edit no-op: %+v", r)
	}
	if r := h.run([]string{"edit", "web.local"}); r.code != ExitUsage {
		t.Fatalf("edit with nothing to change: %+v", r)
	}
	if r := h.run([]string{"edit", "nope.local", "127.0.0.1"}); r.code != ExitNotFound {
		t.Fatalf("edit unknown: %+v", r)
	}
	if r := h.run([]string{"add", "api.local", "localhost:3002", "--json"}); r.json(t)["entry"].(map[string]any)["address"] != "127.0.0.1:3002" {
		t.Fatalf("add localhost: %+v", r)
	}
}
