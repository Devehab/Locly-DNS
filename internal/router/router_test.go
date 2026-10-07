package router

import (
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/devehab/locly-dns/internal/core"
)

func entry(host, ip string, port int) core.Entry {
	e := core.Entry{Hostname: host, IP: ip, Status: core.StatusActive}
	if port != 0 {
		p := uint16(port)
		e.Port = &p
	}
	return e
}

// upstream starts an app on 127.0.0.1 that echoes what it received.
func upstream(t *testing.T) (*httptest.Server, int) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Seen-Host", r.Host)
		w.Header().Set("X-Seen-Forwarded-Host", r.Header.Get("X-Forwarded-Host"))
		_, _ = io.WriteString(w, "hello from "+r.URL.RequestURI())
	}))
	t.Cleanup(srv.Close)
	ap, err := netip.ParseAddrPort(srv.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	return srv, int(ap.Port())
}

func get(t *testing.T, h http.Handler, host, path string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "http://"+host+path, nil)
	req.Host = host
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestForwardsManagedNameToItsPort(t *testing.T) {
	_, port := upstream(t)
	r := New(func() ([]core.Entry, error) {
		return []core.Entry{entry("app.local", "127.0.0.1", port)}, nil
	}, "test")

	rec := get(t, r, "app.local", "/dashboard?x=1")
	if rec.Code != http.StatusOK || rec.Body.String() != "hello from /dashboard?x=1" {
		t.Fatalf("got %d %q", rec.Code, rec.Body.String())
	}
	// The app sees the name it was opened with, like a direct visit.
	if got := rec.Header().Get("X-Seen-Host"); got != "app.local" {
		t.Errorf("upstream Host = %q, want app.local", got)
	}
	if got := rec.Header().Get("X-Seen-Forwarded-Host"); got != "app.local" {
		t.Errorf("X-Forwarded-Host = %q", got)
	}
	// Case and an explicit :80 don't matter.
	if rec := get(t, r, "APP.local:80", "/"); rec.Code != http.StatusOK {
		t.Errorf("APP.local:80 -> %d", rec.Code)
	}
}

func TestRefusesNamesItDoesNotManage(t *testing.T) {
	var calls atomic.Int32
	r := New(func() ([]core.Entry, error) {
		calls.Add(1)
		return []core.Entry{entry("app.local", "127.0.0.1", 3000), entry("bare.local", "127.0.0.1", 0)}, nil
	}, "test")

	for _, host := range []string{"google.com", "evil.local", "127.0.0.2"} {
		rec := get(t, r, host, "/")
		if rec.Code != http.StatusNotFound || !strings.Contains(rec.Body.String(), "isn't managed by LocalDNS") {
			t.Errorf("%s -> %d %q", host, rec.Code, rec.Body.String())
		}
	}
	rec := get(t, r, "bare.local", "/")
	if rec.Code != http.StatusNotFound || !strings.Contains(rec.Body.String(), "has no port") {
		t.Errorf("bare.local -> %d %q", rec.Code, rec.Body.String())
	}
	// Entries are cached briefly instead of re-read on every request.
	if n := calls.Load(); n != 1 {
		t.Errorf("list called %d times, want 1", n)
	}
}

func TestEscapesHostInPages(t *testing.T) {
	r := New(func() ([]core.Entry, error) { return nil, nil }, "test")
	rec := get(t, r, "<script>x</script>.local", "/")
	if strings.Contains(rec.Body.String(), "<script>") {
		t.Fatalf("host not escaped: %s", rec.Body.String())
	}
}

func TestAppNotRunning(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close() // nothing listens there now
	r := New(func() ([]core.Entry, error) { return []core.Entry{entry("app.local", "127.0.0.1", port)}, nil }, "test")
	rec := get(t, r, "app.local", "/")
	if rec.Code != http.StatusBadGateway || !strings.Contains(rec.Body.String(), "Nothing is running on") {
		t.Fatalf("got %d %q", rec.Code, rec.Body.String())
	}
}

func TestListError(t *testing.T) {
	r := New(func() ([]core.Entry, error) { return nil, errors.New("config is broken") }, "test")
	if rec := get(t, r, "app.local", "/"); rec.Code != http.StatusInternalServerError {
		t.Fatalf("got %d", rec.Code)
	}
}

func TestHealthAndProbe(t *testing.T) {
	r := New(func() ([]core.Entry, error) { return nil, nil }, "1.2.3")
	rec := get(t, r, "127.0.0.1", HealthPath)
	if rec.Code != http.StatusOK || rec.Header().Get("X-LocalDNS-Router") != "1.2.3" {
		t.Fatalf("health: %d %v", rec.Code, rec.Header())
	}
	// The health path is only answered for 127.0.0.1/localhost, not for a
	// managed or unknown name.
	if rec := get(t, r, "app.local", HealthPath); rec.Header().Get("X-LocalDNS-Router") != "" {
		t.Fatal("health answered for app.local")
	}

	ln, err := Listen(0)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(ln.Addr().String(), "127.0.0.1:") {
		t.Fatalf("listening on %s, want 127.0.0.1 only", ln.Addr())
	}
	port := ln.Addr().(*net.TCPAddr).Port
	srv := &http.Server{Handler: r}
	go func() { _ = srv.Serve(ln) }()
	defer func() { _ = srv.Close() }()
	if ok, v := Probe(port); !ok || v != "1.2.3" {
		t.Fatalf("Probe = %v %q", ok, v)
	}
	_, other := upstream(t)
	if ok, _ := Probe(other); ok {
		t.Fatal("Probe accepted a server that is not the LocalDNS router")
	}
}

func TestShortURL(t *testing.T) {
	cases := []struct {
		e    core.Entry
		port int
		want string
	}{
		{entry("app.local", "127.0.0.1", 3000), 80, "http://app.local"},
		{entry("app.local", "127.0.0.1", 3000), 8080, "http://app.local:8080"},
		{entry("nas.local", "192.168.1.20", 5000), 80, ""},
		{entry("bare.local", "127.0.0.1", 0), 80, ""},
	}
	for _, c := range cases {
		if got := ShortURL(c.e, c.port); got != c.want {
			t.Errorf("ShortURL(%s %s %v, %d) = %q, want %q", c.e.Hostname, c.e.IP, c.e.Port, c.port, got, c.want)
		}
	}
}

func TestServiceDefinitions(t *testing.T) {
	plist := launchdPlist("/Library/Application Support/LocalDNS/local&dns", map[string]string{"LOCALDNS_HOSTS_FILE": "/tmp/a<b"})
	for _, want := range []string{
		"<string>dev.locly.router</string>",
		"<string>/Library/Application Support/LocalDNS/local&amp;dns</string>",
		"<string>router</string>", "<string>run</string>",
		// Never root: launchd opens the socket and runs the router as nobody.
		"<key>UserName</key>\n  <string>nobody</string>",
		"<key>SockNodeName</key>\n      <string>127.0.0.1</string>",
		"<key>SockServiceName</key>\n      <string>80</string>",
		"<key>inetdCompatibility</key>\n  <dict>\n    <key>Wait</key>\n    <true/>",
		"<key>LOCALDNS_ROUTER_FD</key>\n    <string>0</string>",
		"<key>LOCALDNS_HOSTS_FILE</key>\n    <string>/tmp/a&lt;b</string>",
	} {
		if !strings.Contains(plist, want) {
			t.Errorf("plist missing %q\n%s", want, plist)
		}
	}
	if strings.Contains(plist, "root") {
		t.Error("plist mentions root")
	}

	unit := systemdUnit("/usr/local/bin/localdns", "me", map[string]string{"LOCALDNS_CONFIG_DIR": `/tmp/50% "x"`})
	for _, want := range []string{`ExecStart="/usr/local/bin/localdns" router run`, "User=me",
		`Environment="LOCALDNS_CONFIG_DIR=/tmp/50%% \"x\""`,
		"AmbientCapabilities=CAP_NET_BIND_SERVICE", "NoNewPrivileges=yes"} {
		if !strings.Contains(unit, want) {
			t.Errorf("unit missing %q\n%s", want, unit)
		}
	}
	if !strings.Contains(systemdUnit("/x", "", nil), "DynamicUser=yes") {
		t.Error("unit without a user should use DynamicUser")
	}
}

func TestInheritedListener(t *testing.T) {
	ln, err := Listen(0)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ln.Close() }()
	f, err := ln.(*net.TCPListener).File()
	if err != nil {
		t.Skipf("no socket files here: %v", err)
	}
	defer func() { _ = f.Close() }()

	got, err := InheritedListener(strconv.Itoa(int(f.Fd())))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = got.Close() }()
	if got.Addr().String() != ln.Addr().String() {
		t.Fatalf("inherited %s, want %s", got.Addr(), ln.Addr())
	}
	for _, bad := range []string{"", "x", "-1"} {
		if _, err := InheritedListener(bad); err == nil {
			t.Errorf("InheritedListener(%q) should fail", bad)
		}
	}
}

func TestOnlyLoopbackAddressesAreServed(t *testing.T) {
	for addr, want := range map[string]bool{
		"127.0.0.1:80": true, "[::1]:80": true, "0.0.0.0:80": false, "192.168.1.5:80": false, "[::]:80": false,
	} {
		ap := netip.MustParseAddrPort(addr)
		if got := isLoopback(net.TCPAddrFromAddrPort(ap)); got != want {
			t.Errorf("isLoopback(%s) = %v, want %v", addr, got, want)
		}
	}
}

func TestDashboardNotRunning(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()
	r := New(func() ([]core.Entry, error) {
		return []core.Entry{entry(core.DashboardHostname, "127.0.0.1", port)}, nil
	}, "test")
	rec := get(t, r, core.DashboardHostname, "/")
	if rec.Code != http.StatusBadGateway || !strings.Contains(rec.Body.String(), "localdns ui") {
		t.Fatalf("got %d %q", rec.Code, rec.Body.String())
	}
}

func TestPausedNameIsNotForwarded(t *testing.T) {
	_, port := upstream(t)
	e := entry("app.local", "127.0.0.1", port)
	e.Status = core.StatusPaused
	r := New(func() ([]core.Entry, error) { return []core.Entry{e}, nil }, "test")
	rec := get(t, r, "app.local", "/")
	if rec.Code != http.StatusNotFound || !strings.Contains(rec.Body.String(), "localdns resume app.local") {
		t.Fatalf("got %d %q", rec.Code, rec.Body.String())
	}
}
