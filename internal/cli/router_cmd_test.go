package cli

import (
	"strings"
	"testing"

	"github.com/devehab/locly-dns/internal/router"
	"github.com/devehab/locly-dns/internal/version"
)

type fakeRouter struct {
	installed, outdated, running bool
	version                      string // of the running router; default: this version
	enableErr                    error
	enabled, disabled            int
	exe                          string
	env                          map[string]string
}

func (f *fakeRouter) State() router.ServiceState {
	return router.ServiceState{Installed: f.installed, Outdated: f.outdated, Kind: "launchd", Path: "/tmp/dev.locly.router.plist"}
}

func (f *fakeRouter) Enable(exe string, env map[string]string) error {
	if f.enableErr != nil {
		return f.enableErr
	}
	f.enabled++
	f.exe, f.env = exe, env
	f.installed, f.outdated, f.running = true, false, true
	return nil
}

func (f *fakeRouter) Disable() error {
	f.disabled++
	f.installed, f.outdated, f.running = false, false, false
	return nil
}

func (f *fakeRouter) Running() (bool, string) {
	if !f.running {
		return false, ""
	}
	if f.version != "" {
		return true, f.version
	}
	return true, version.Version
}

func withRouter(r RouterControl) runOpt { return func(e *Env) { e.Router = r } }

func TestPortFreeURLs(t *testing.T) {
	h := newHarness(t)
	fr := &fakeRouter{}

	// Off: the port is shown, with a tip on how to drop it.
	r := h.run([]string{"add", "app.local", "127.0.0.1:3000"}, withRouter(fr))
	if r.code != 0 || !strings.Contains(r.stdout, "http://app.local:3000") ||
		!strings.Contains(r.stdout, "localdns router enable") {
		t.Fatalf("add with router off: %+v", r)
	}

	r = h.run([]string{"router", "enable"}, withRouter(fr))
	if r.code != 0 || fr.enabled != 1 || fr.exe != h.exe || !strings.Contains(r.stdout, "Port-free URLs are on") {
		t.Fatalf("router enable: %+v (enabled %d, exe %q)", r, fr.enabled, fr.exe)
	}
	// The service serves the same hosts file and config as this command.
	if fr.env["LOCALDNS_HOSTS_FILE"] != h.hosts.Path() || fr.env["LOCALDNS_CONFIG_DIR"] != h.configDir {
		t.Fatalf("router enable passed env %v", fr.env)
	}

	// On: names on 127.0.0.1 with a port get a port-free URL.
	h.run([]string{"add", "nas.local", "192.168.1.20:5000"}, withRouter(fr))
	h.run([]string{"add", "bare.local", "127.0.0.1"}, withRouter(fr))
	v := h.run([]string{"list", "--json"}, withRouter(fr)).json(t)
	got := map[string]any{}
	for _, e := range v["entries"].([]any) {
		m := e.(map[string]any)
		got[m["hostname"].(string)] = m["short_url"]
	}
	if got["app.local"] != "http://app.local" || got["nas.local"] != nil || got["bare.local"] != nil {
		t.Fatalf("short_url = %v", got)
	}
	r = h.run([]string{"list"}, withRouter(fr))
	if !strings.Contains(r.stdout, "http://app.local\n") && !strings.Contains(r.stdout, "http://app.local ") {
		t.Fatalf("list should show the port-free URL:\n%s", r.stdout)
	}
	r = h.run([]string{"add", "app.local", "127.0.0.1:3000"}, withRouter(fr))
	if !strings.Contains(r.stdout, "http://app.local   (also http://app.local:3000)") {
		t.Fatalf("add with router on:\n%s", r.stdout)
	}
	if r := h.run([]string{"status"}, withRouter(fr)); !strings.Contains(r.stdout, "Port-free") {
		t.Fatalf("status:\n%s", r.stdout)
	}
	v = h.run([]string{"doctor", "--json", "--port", "0"}, withRouter(fr)).json(t)
	checks := v["checks"].([]any)
	last := checks[len(checks)-1].(map[string]any)
	if last["id"] != "router" || last["status"] != "pass" {
		t.Fatalf("router check = %v", last)
	}

	// Installed but not answering is a warning, not a failure.
	fr.running = false
	v = h.run([]string{"doctor", "--json", "--port", "0"}, withRouter(fr)).json(t)
	checks = v["checks"].([]any)
	if last := checks[len(checks)-1].(map[string]any); last["status"] != "warn" || v["ok"] != true {
		t.Fatalf("router check when down = %v", v)
	}
	fr.running = true

	// Uninstall removes the router too.
	r = h.run([]string{"uninstall", "--yes", "--json"}, withRouter(fr))
	if v := r.json(t); r.code != 0 || v["router_removed"] != true || fr.disabled != 1 {
		t.Fatalf("uninstall: %+v", r)
	}
}

func TestRouterStatusExplainsProblems(t *testing.T) {
	h := newHarness(t)

	r := h.run([]string{"router"}, withRouter(&fakeRouter{installed: true, outdated: true}))
	if r.code != 0 || !strings.Contains(r.stdout, "sudo localdns router enable") || !strings.Contains(r.stdout, "0.2.0") {
		t.Fatalf("outdated router:\n%s", r.stdout)
	}
	v := h.run([]string{"doctor", "--json", "--port", "0"}, withRouter(&fakeRouter{installed: true, outdated: true})).json(t)
	checks := v["checks"].([]any)
	if last := checks[len(checks)-1].(map[string]any); last["status"] != "warn" || last["fix"] != "sudo localdns router enable" {
		t.Fatalf("doctor with outdated router: %v", last)
	}

	r = h.run([]string{"router"}, withRouter(&fakeRouter{installed: true, running: true, version: "0.0.1"}))
	if !strings.Contains(r.stdout, "0.0.1 (this localdns is") || !strings.Contains(r.stdout, "Update the router") {
		t.Fatalf("old running router:\n%s", r.stdout)
	}

	r = h.run([]string{"router", "--json"}, withRouter(&fakeRouter{installed: true, running: true}))
	if v := r.json(t); v["running"] != true || v["version"] != version.Version || v["installed"] != true {
		t.Fatalf("router --json = %v", v)
	}
}

func TestRouterEnableNeedsAdmin(t *testing.T) {
	h := newHarness(t)
	h.elevator.available = true
	fr := &fakeRouter{enableErr: router.ErrNeedsAdmin}
	r := h.run([]string{"router", "enable"}, withRouter(fr), interactive(""))
	if len(h.elevator.calls) != 1 || r.code != 0 {
		t.Fatalf("router enable should re-run with sudo: %+v, calls %v", r, h.elevator.calls)
	}
	call := strings.Join(h.elevator.calls[0], " ")
	if !strings.Contains(call, "router enable") {
		t.Fatalf("elevated call = %s", call)
	}
}

func TestRouterUnknownAction(t *testing.T) {
	h := newHarness(t)
	if r := h.run([]string{"router", "explode"}); r.code != ExitUsage {
		t.Fatalf("router explode: %+v", r)
	}
}
