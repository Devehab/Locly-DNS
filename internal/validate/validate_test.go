package validate

import (
	"errors"
	"net/netip"
	"strings"
	"testing"
)

func TestHostname(t *testing.T) {
	valid := map[string]string{
		"app.local":       "app.local",
		"App.Local":       "app.local",
		"app.local.":      "app.local",
		" my-app.test ":   "my-app.test",
		"a.b.c.internal":  "a.b.c.internal",
		"x1.localhost":    "x1.localhost",
		"nas":             "nas",
		"printer.lan":     "printer.lan",
		"svc.home.arpa":   "svc.home.arpa",
		"123.local":       "123.local",
		"a-b-c.localhost": "a-b-c.localhost",
		// Pasted links are accepted.
		"http://app.local":   "app.local",
		"https://App.local/": "app.local",
	}
	for in, want := range valid {
		got, err := Hostname(in)
		if err != nil || got != want {
			t.Errorf("Hostname(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	invalid := []string{
		"", "   ", "http://app.local/page", "app.local/path", "app.local:3000", "-app.local",
		"app-.local", "app..local", "app_x.local", "ap p.local", "émoji.local",
		strings.Repeat("a", 64) + ".local", strings.Repeat("a.", 127) + "local",
	}
	for _, in := range invalid {
		if got, err := Hostname(in); err == nil {
			t.Errorf("Hostname(%q) = %q, want error", in, got)
		}
	}
}

func TestLocalHostnamePolicy(t *testing.T) {
	allowed := []string{
		"app.local", "api.test", "ha.local", "db.internal", "x.localhost", "nas.lan",
		"printer.home.arpa", "box.localdomain", "demo.example", "deep.sub.app.local",
	}
	for _, h := range allowed {
		if _, err := LocalHostname(h); err != nil {
			t.Errorf("LocalHostname(%q) unexpected error: %v", h, err)
		}
	}
	// Public internet domains must never be accepted: LocalDNS does not
	// override real DNS names.
	refused := []string{
		"google.com", "example.com", "github.io", "app.dev", "my.app", "localhost.com",
		"local", "test", "home.arpa", "localhost", "broadcasthost", "localhost.localdomain",
		"ip6-localhost", "nas", "local.com", "app.local.evil.com",
	}
	for _, h := range refused {
		if _, err := LocalHostname(h); err == nil {
			t.Errorf("LocalHostname(%q) should be refused", h)
		}
	}
}

func TestLocalHostnameHint(t *testing.T) {
	_, err := LocalHostname("google.com")
	var ve *Error
	if !errors.As(err, &ve) {
		t.Fatalf("expected *Error, got %T", err)
	}
	if !strings.Contains(ve.Hint, ".local") || !strings.Contains(ve.Hint, "normal DNS") {
		t.Errorf("hint should explain the policy, got %q", ve.Hint)
	}
}

func TestAddress(t *testing.T) {
	tests := []struct {
		in   string
		ip   string
		port uint16
	}{
		{"127.0.0.1:3000", "127.0.0.1", 3000},
		{"127.0.0.1", "127.0.0.1", 0},
		{"192.168.1.60", "192.168.1.60", 0},
		{"192.168.1.60:8123", "192.168.1.60", 8123},
		{" 10.0.0.2:65535 ", "10.0.0.2", 65535},
		{"[::1]:8080", "::1", 8080},
		{"::1", "::1", 0},
		{"fd00::5", "fd00::5", 0},
		{"::ffff:127.0.0.1", "127.0.0.1", 0},
		// Pasted links and localhost.
		{"http://127.0.0.1:7861", "127.0.0.1", 7861},
		{"https://127.0.0.1:7861/", "127.0.0.1", 7861},
		{" HTTP://192.168.1.60:8123// ", "192.168.1.60", 8123},
		{"http://[::1]:8080/", "::1", 8080},
		{"localhost:3000", "127.0.0.1", 3000},
		{"http://LocalHost:5173/", "127.0.0.1", 5173},
		{"localhost", "127.0.0.1", 0},
	}
	for _, tt := range tests {
		ip, port, err := Address(tt.in)
		if err != nil {
			t.Errorf("Address(%q) error: %v", tt.in, err)
			continue
		}
		if ip.String() != tt.ip || port != tt.port {
			t.Errorf("Address(%q) = %s, %d; want %s, %d", tt.in, ip, port, tt.ip, tt.port)
		}
	}
	bad := map[string]string{
		"":                          "empty",
		"app.local":                 "not a hostname",
		"127.0.0.1:0":               "port 0",
		"127.0.0.1:70000":           "bad port",
		"127.0.0.1:abc":             "bad port",
		"http://127.0.0.1:3000/app": "path",
		"127.0.0.1:3000/?x=1":       "path",
		"ftp://127.0.0.1":           "path",
		"999.1.1.1":                 "invalid",
		"127.0.0.1:3000:1":          "invalid",
	}
	for in, want := range bad {
		_, _, err := Address(in)
		if err == nil {
			t.Errorf("Address(%q) should fail", in)
			continue
		}
		var ve *Error
		if !errors.As(err, &ve) {
			t.Errorf("Address(%q): expected *Error, got %T", in, err)
			continue
		}
		msg := ve.Message + " " + ve.Hint
		if !strings.Contains(strings.ToLower(msg), want) {
			t.Errorf("Address(%q) error %q should mention %q", in, msg, want)
		}
	}
}

func TestLocalIP(t *testing.T) {
	ok := []string{
		"127.0.0.1", "127.8.9.10", "::1", "10.1.2.3", "172.16.0.1", "172.31.255.255",
		"192.168.1.60", "169.254.1.1", "100.64.0.1", "100.127.255.254", "fd12:3456::1", "fe80::1",
	}
	for _, s := range ok {
		if err := LocalIP(netip.MustParseAddr(s)); err != nil {
			t.Errorf("LocalIP(%s) unexpected error: %v", s, err)
		}
	}
	refused := []string{
		"8.8.8.8", "1.1.1.1", "142.250.80.46", "172.32.0.1", "100.128.0.1", "0.0.0.0",
		"::", "224.0.0.1", "255.255.255.255", "2001:4860:4860::8888",
	}
	for _, s := range refused {
		if err := LocalIP(netip.MustParseAddr(s)); err == nil {
			t.Errorf("LocalIP(%s) should be refused", s)
		}
	}
}

func TestFormatAddress(t *testing.T) {
	if got := FormatAddress(netip.MustParseAddr("127.0.0.1"), 3000); got != "127.0.0.1:3000" {
		t.Errorf("got %q", got)
	}
	if got := FormatAddress(netip.MustParseAddr("192.168.1.60"), 0); got != "192.168.1.60" {
		t.Errorf("got %q", got)
	}
	if got := FormatAddress(netip.MustParseAddr("::1"), 8080); got != "[::1]:8080" {
		t.Errorf("got %q", got)
	}
}
