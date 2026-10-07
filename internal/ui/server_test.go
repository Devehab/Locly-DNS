package ui

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/devehab/locly-dns/internal/config"
	"github.com/devehab/locly-dns/internal/core"
	"github.com/devehab/locly-dns/internal/hosts/hoststest"
)

const testToken = "test-token-123"

type uiFixture struct {
	t     *testing.T
	srv   *httptest.Server
	hosts *hoststest.TemporaryHostsFile
	m     *core.Manager
}

func newUIFixture(t *testing.T) *uiFixture {
	t.Helper()
	return newUIFixtureWith(t, nil, nil)
}

// newUIFixtureWith lets a test prepare the hosts file (knowing the port the
// server will use) and adjust the options.
func newUIFixtureWith(t *testing.T, setup func(m *core.Manager, port int), adjust func(*Options)) *uiFixture {
	t.Helper()
	hf := hoststest.New(t, hoststest.DefaultContent)
	m := core.New(core.Options{Hosts: hf, Store: config.NewStore(filepath.Join(t.TempDir(), "cfg"))})
	ln, err := Listen(0)
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	if setup != nil {
		setup(m, port)
	}
	opts := Options{Port: port, Version: "test", Token: testToken, ReadOnlyHint: "sudo localdns ui"}
	if adjust != nil {
		adjust(&opts)
	}
	h, err := NewHandler(m, opts)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewUnstartedServer(h)
	_ = srv.Listener.Close()
	srv.Listener = ln
	srv.Start()
	t.Cleanup(srv.Close)
	return &uiFixture{t: t, srv: srv, hosts: hf, m: m}
}

// response is what tests need from an HTTP reply (the body is already read
// and closed).
type response struct {
	StatusCode int
	Header     http.Header
}

func (f *uiFixture) do(method, path, body string, hdr map[string]string) (response, map[string]any) {
	f.t.Helper()
	req, err := http.NewRequest(method, f.srv.URL+path, strings.NewReader(body))
	if err != nil {
		f.t.Fatal(err)
	}
	for k, v := range hdr {
		if k == "Host" {
			req.Host = v
			continue
		}
		req.Header.Set(k, v)
	}
	resp, err := f.srv.Client().Do(req)
	if err != nil {
		f.t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	data, _ := io.ReadAll(resp.Body)
	var out map[string]any
	_ = json.Unmarshal(data, &out)
	return response{StatusCode: resp.StatusCode, Header: resp.Header}, out
}

func authed(extra ...string) map[string]string {
	h := map[string]string{TokenHeader: testToken}
	for i := 0; i+1 < len(extra); i += 2 {
		h[extra[i]] = extra[i+1]
	}
	return h
}

func TestListenIsLoopbackOnly(t *testing.T) {
	ln, err := Listen(0)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ln.Close() }()
	addr := ln.Addr().(*net.TCPAddr)
	if !addr.IP.IsLoopback() || addr.IP.String() != LoopbackAddr {
		t.Fatalf("UI must listen on 127.0.0.1 only, got %s", addr)
	}
}

func TestIndexEmbedsTokenAndSecurityHeaders(t *testing.T) {
	f := newUIFixture(t)
	resp, err := f.srv.Client().Get(f.srv.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(body), testToken) || !strings.Contains(string(body), "+ Add Host") {
		t.Fatalf("index: %d %s", resp.StatusCode, body)
	}
	for _, h := range []string{"Content-Security-Policy", "X-Frame-Options", "X-Content-Type-Options", "Referrer-Policy"} {
		if resp.Header.Get(h) == "" {
			t.Errorf("missing header %s", h)
		}
	}
	if strings.Contains(resp.Header.Get("Content-Security-Policy"), "unsafe-inline") {
		t.Error("CSP must not allow inline scripts")
	}
	if resp.Header.Get("Access-Control-Allow-Origin") != "" {
		t.Error("CORS must not be enabled")
	}
}

func TestAssets(t *testing.T) {
	f := newUIFixture(t)
	for name, ctype := range assetTypes {
		resp, _ := f.do("GET", "/assets/"+name, "", nil)
		if resp.StatusCode != http.StatusOK || resp.Header.Get("Content-Type") != ctype {
			t.Errorf("%s: %d %s", name, resp.StatusCode, resp.Header.Get("Content-Type"))
		}
	}
	if resp, _ := f.do("GET", "/assets/index.html", "", nil); resp.StatusCode != http.StatusNotFound {
		t.Errorf("unknown asset should 404, got %d", resp.StatusCode)
	}
	if resp, _ := f.do("GET", "/assets/../server.go", "", nil); resp.StatusCode == http.StatusOK {
		t.Error("path traversal served a file")
	}
}

func TestDNSRebindingIsBlocked(t *testing.T) {
	f := newUIFixture(t)
	for _, host := range []string{"evil.example.com", "evil.example.com:" + port(f), "192.168.1.5:" + port(f), "127.0.0.1:1"} {
		resp, _ := f.do("GET", "/api/entries", "", authed("Host", host))
		if resp.StatusCode != http.StatusForbidden {
			t.Errorf("Host %q: status %d, want 403", host, resp.StatusCode)
		}
	}
	if resp, _ := f.do("GET", "/", "", map[string]string{"Host": "localhost:" + port(f)}); resp.StatusCode != http.StatusOK {
		t.Errorf("localhost Host should be allowed, got %d", resp.StatusCode)
	}
}

func port(f *uiFixture) string {
	u, _ := url.Parse(f.srv.URL)
	return u.Port()
}

func TestAPIRequiresToken(t *testing.T) {
	f := newUIFixture(t)
	resp, _ := f.do("GET", "/api/entries", "", nil)
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("no token: %d", resp.StatusCode)
	}
	resp, _ = f.do("POST", "/api/entries", `{"hostname":"a.local","address":"127.0.0.1"}`,
		map[string]string{TokenHeader: "wrong", "Content-Type": "application/json"})
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("wrong token: %d", resp.StatusCode)
	}
	if f.hosts.Content() != hoststest.DefaultContent {
		t.Fatal("unauthenticated request modified the hosts file")
	}
}

func TestCrossOriginIsBlocked(t *testing.T) {
	f := newUIFixture(t)
	for _, hdr := range []map[string]string{
		authed("Origin", "http://evil.example.com", "Content-Type", "application/json"),
		authed("Origin", "null", "Content-Type", "application/json"),
		authed("Sec-Fetch-Site", "cross-site", "Content-Type", "application/json"),
	} {
		resp, _ := f.do("POST", "/api/entries", `{"hostname":"a.local","address":"127.0.0.1"}`, hdr)
		if resp.StatusCode != http.StatusForbidden {
			t.Errorf("%v: status %d", hdr, resp.StatusCode)
		}
	}
	resp, _ := f.do("POST", "/api/entries", `{"hostname":"a.local","address":"127.0.0.1"}`, authed())
	if resp.StatusCode != http.StatusUnsupportedMediaType {
		t.Errorf("form-encoded body should be refused, got %d", resp.StatusCode)
	}
	if f.hosts.Content() != hoststest.DefaultContent {
		t.Fatal("blocked request modified the hosts file")
	}
}

func TestAddListDeleteThroughAPI(t *testing.T) {
	f := newUIFixture(t)
	same := authed("Content-Type", "application/json", "Origin", "http://127.0.0.1:"+port(f), "Sec-Fetch-Site", "same-origin")

	resp, body := f.do("POST", "/api/entries", `{"hostname":"app.local","address":"127.0.0.1:3000"}`, same)
	if resp.StatusCode != http.StatusCreated || body["action"] != "added" {
		t.Fatalf("add: %d %v", resp.StatusCode, body)
	}
	resp, body = f.do("POST", "/api/entries", `{"hostname":"app.local","address":"127.0.0.1:3000"}`, same)
	if resp.StatusCode != http.StatusOK || body["action"] != "unchanged" {
		t.Fatalf("re-add: %d %v", resp.StatusCode, body)
	}

	resp, body = f.do("GET", "/api/entries", "", same)
	entries, _ := body["entries"].([]any)
	if resp.StatusCode != http.StatusOK || len(entries) != 1 {
		t.Fatalf("list: %d %v", resp.StatusCode, body)
	}
	e := entries[0].(map[string]any)
	if e["hostname"] != "app.local" || e["url"] != "http://app.local:3000" || e["port"] != float64(3000) || e["status"] != "active" {
		t.Fatalf("entry = %v", e)
	}
	if !strings.Contains(f.hosts.Content(), "127.0.0.1 app.local") {
		t.Fatal("hosts file not updated")
	}

	resp, body = f.do("DELETE", "/api/entries/app.local", "", same)
	if resp.StatusCode != http.StatusOK || body["action"] != "removed" {
		t.Fatalf("delete: %d %v", resp.StatusCode, body)
	}
	if f.hosts.Content() != hoststest.DefaultContent {
		t.Fatal("hosts file not restored after delete")
	}
	resp, body = f.do("DELETE", "/api/entries/app.local", "", same)
	if resp.StatusCode != http.StatusNotFound || errCode(body) != "not_found" {
		t.Fatalf("second delete: %d %v", resp.StatusCode, body)
	}
}

func errCode(body map[string]any) string {
	e, _ := body["error"].(map[string]any)
	s, _ := e["code"].(string)
	return s
}

func TestAPIErrors(t *testing.T) {
	f := newUIFixture(t)
	h := authed("Content-Type", "application/json")
	cases := []struct {
		body   string
		status int
		code   string
	}{
		{`{"hostname":"google.com","address":"127.0.0.1"}`, 400, "invalid_input"},
		{`{"hostname":"app.local","address":"8.8.8.8"}`, 400, "invalid_input"},
		{`{"hostname":"printer.lan","address":"127.0.0.1"}`, 409, "conflict"},
		{`{"hostname":"a.local"`, 400, "invalid_input"},
		{`{"hostname":"a.local","address":"127.0.0.1","extra":1}`, 400, "invalid_input"},
	}
	for _, c := range cases {
		resp, body := f.do("POST", "/api/entries", c.body, h)
		if resp.StatusCode != c.status || errCode(body) != c.code {
			t.Errorf("%s: %d %v; want %d %s", c.body, resp.StatusCode, body, c.status, c.code)
		}
	}
	if f.hosts.Content() != hoststest.DefaultContent {
		t.Fatal("failed requests modified the hosts file")
	}
}

func TestStatusEndpoint(t *testing.T) {
	f := newUIFixture(t)
	resp, body := f.do("GET", "/api/status", "", authed())
	if resp.StatusCode != http.StatusOK || body["writable"] != true || body["version"] != "test" || body["hosts_file"] != f.hosts.Path() {
		t.Fatalf("status: %d %v", resp.StatusCode, body)
	}
	if _, ok := body["read_only_hint"]; ok {
		t.Error("writable UI should not show a read-only hint")
	}
}

func TestHealthAndProbe(t *testing.T) {
	f := newUIFixture(t)
	p, _ := strconv.Atoi(port(f))
	if !Probe(p) {
		t.Fatal("Probe should detect the running UI")
	}
	c := AvailabilityCheck(p)
	if c.Status != core.CheckPass || !strings.Contains(c.Message, "already running") {
		t.Fatalf("check = %+v", c)
	}
}

func TestAvailabilityCheckBusyPort(t *testing.T) {
	ln, err := Listen(0)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ln.Close() }()
	p := ln.Addr().(*net.TCPAddr).Port
	c := AvailabilityCheck(p)
	if c.Status != core.CheckWarn || c.Fix == "" {
		t.Fatalf("busy port check = %+v", c)
	}
}

func TestServeStopsOnCancel(t *testing.T) {
	hf := hoststest.New(t, hoststest.DefaultContent)
	m := core.New(core.Options{Hosts: hf, Store: config.NewStore(t.TempDir())})
	ln, err := Listen(0)
	if err != nil {
		t.Fatal(err)
	}
	h, _ := NewHandler(m, Options{Port: ln.Addr().(*net.TCPAddr).Port})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- Serve(ctx, ln, h) }()
	time.Sleep(50 * time.Millisecond)
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Serve did not stop")
	}
}

func TestDashboardNameNeedsHostsEntry(t *testing.T) {
	viaName := authed("Content-Type", "application/json", "Origin", "http://"+core.DashboardHostname)

	// Not mapped: the name is refused, like any other.
	f := newUIFixture(t)
	if resp, _ := f.do("GET", "/", "", map[string]string{"Host": core.DashboardHostname}); resp.StatusCode != http.StatusForbidden {
		t.Fatalf("unmapped dashboard name: %d", resp.StatusCode)
	}

	// Mapped to this server: allowed through the router (port 80) or directly.
	f = newUIFixtureWith(t, func(m *core.Manager, port int) {
		if _, err := m.Add(core.DashboardHostname, "127.0.0.1:"+strconv.Itoa(port), core.AddOptions{}); err != nil {
			t.Fatal(err)
		}
	}, nil)
	for _, host := range []string{core.DashboardHostname, core.DashboardHostname + ":" + port(f)} {
		if resp, _ := f.do("GET", "/", "", map[string]string{"Host": host}); resp.StatusCode != http.StatusOK {
			t.Errorf("Host %s: %d", host, resp.StatusCode)
		}
	}
	viaName["Host"] = core.DashboardHostname
	if resp, _ := f.do("POST", "/api/entries", `{"hostname":"a.local","address":"127.0.0.1"}`, viaName); resp.StatusCode != http.StatusCreated {
		t.Errorf("API call through the dashboard name: %d", resp.StatusCode)
	}

	// Mapped somewhere else: refused.
	f = newUIFixtureWith(t, func(m *core.Manager, port int) {
		if _, err := m.Add(core.DashboardHostname, "127.0.0.1:"+strconv.Itoa(port+1), core.AddOptions{}); err != nil {
			t.Fatal(err)
		}
	}, nil)
	if resp, _ := f.do("GET", "/", "", map[string]string{"Host": core.DashboardHostname}); resp.StatusCode != http.StatusForbidden {
		t.Fatalf("dashboard name mapped elsewhere: %d", resp.StatusCode)
	}
}

func TestEditThroughAPI(t *testing.T) {
	f := newUIFixture(t)
	same := authed("Content-Type", "application/json", "Origin", "http://127.0.0.1:"+port(f))
	f.do("POST", "/api/entries", `{"hostname":"app.local","address":"127.0.0.1:3000"}`, same)
	resp, body := f.do("PUT", "/api/entries/app.local", `{"hostname":"web.local","address":"http://127.0.0.1:4000/"}`, same)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("edit: %d %v", resp.StatusCode, body)
	}
	entry := body["entry"].(map[string]any)
	if entry["hostname"] != "web.local" || entry["address"] != "127.0.0.1:4000" {
		t.Fatalf("edit result = %v", entry)
	}
	resp, body = f.do("PUT", "/api/entries/nope.local", `{"hostname":"","address":"127.0.0.1"}`, same)
	if resp.StatusCode != http.StatusNotFound || errCode(body) != "not_found" {
		t.Fatalf("edit unknown: %d %v", resp.StatusCode, body)
	}
	resp, _ = f.do("PUT", "/api/entries/web.local", `{"hostname":"x.local"}`, authed("Origin", "http://127.0.0.1:"+port(f)))
	if resp.StatusCode != http.StatusUnsupportedMediaType {
		t.Fatalf("edit without JSON content type: %d", resp.StatusCode)
	}
}

type fakeSwitch struct {
	mu                sync.Mutex
	status            RouterStatus
	enabled, disabled int
}

func (f *fakeSwitch) RouterStatus() RouterStatus {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.status
}

func (f *fakeSwitch) EnableRouter() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.enabled++
	f.status.Running, f.status.Installed = true, true
	return nil
}

func (f *fakeSwitch) DisableRouter() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.disabled++
	f.status.Running, f.status.Installed = false, false
	return nil
}

func TestRouterSwitch(t *testing.T) {
	routerOffDelay = 10 * time.Millisecond
	sw := &fakeSwitch{status: RouterStatus{Running: true, Installed: true, CanChange: true}}
	f := newUIFixtureWith(t, func(m *core.Manager, port int) {
		_, _ = m.Add(core.DashboardHostname, "127.0.0.1:"+strconv.Itoa(port), core.AddOptions{})
	}, func(o *Options) { o.Router = sw })
	same := authed("Content-Type", "application/json", "Origin", "http://127.0.0.1:"+port(f))

	_, body := f.do("GET", "/api/router", "", same)
	if body["supported"] != true || body["running"] != true || body["dashboard_url"] != "http://localdns.local" ||
		body["direct_url"] != "http://127.0.0.1:"+port(f) {
		t.Fatalf("router status = %v", body)
	}

	// Off: the reply comes first, then the router stops.
	resp, body := f.do("POST", "/api/router", `{"enabled":false}`, same)
	if resp.StatusCode != http.StatusAccepted || body["running"] != false || body["direct_url"] == nil {
		t.Fatalf("switch off: %d %v", resp.StatusCode, body)
	}
	deadline := time.Now().Add(2 * time.Second)
	for sw.RouterStatus().Running && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if sw.disabled != 1 {
		t.Fatal("router was not switched off")
	}

	resp, body = f.do("POST", "/api/router", `{"enabled":true}`, same)
	if resp.StatusCode != http.StatusOK || body["running"] != true || sw.enabled != 1 {
		t.Fatalf("switch on: %d %v", resp.StatusCode, body)
	}

	if resp, _ := f.do("POST", "/api/router", `{"on":true}`, same); resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("bad body: %d", resp.StatusCode)
	}

	// Without administrator rights the switch is refused and explains why.
	sw.mu.Lock()
	sw.status.CanChange = false
	sw.mu.Unlock()
	resp, body = f.do("POST", "/api/router", `{"enabled":false}`, same)
	if resp.StatusCode != http.StatusForbidden || !strings.Contains(body["error"].(map[string]any)["hint"].(string), "sudo localdns ui") {
		t.Fatalf("no rights: %d %v", resp.StatusCode, body)
	}

	// No router control at all: the switch is hidden.
	f = newUIFixture(t)
	if _, body := f.do("GET", "/api/router", "", authed()); body["supported"] != false {
		t.Fatalf("router without control = %v", body)
	}
}

func TestGuideIsServed(t *testing.T) {
	f := newUIFixture(t)
	resp, _ := f.do("GET", "/guide", "", nil)
	if resp.StatusCode != http.StatusOK || !strings.HasPrefix(resp.Header.Get("Content-Type"), "text/html") {
		t.Fatalf("guide: %d %v", resp.StatusCode, resp.Header)
	}
	for _, a := range []string{"guide.js", "guide.css"} {
		if resp, _ := f.do("GET", "/assets/"+a, "", nil); resp.StatusCode != http.StatusOK {
			t.Errorf("%s: %d", a, resp.StatusCode)
		}
	}
}
