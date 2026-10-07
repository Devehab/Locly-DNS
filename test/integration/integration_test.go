// Package integration tests LocalDNS components working together on a
// temporary hosts file. Nothing here touches the real hosts file.
package integration

import (
	"bytes"
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"io/fs"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/devehab/locly-dns/internal/cli"
	"github.com/devehab/locly-dns/internal/config"
	"github.com/devehab/locly-dns/internal/core"
	"github.com/devehab/locly-dns/internal/hosts"
	"github.com/devehab/locly-dns/internal/hosts/hoststest"
	"github.com/devehab/locly-dns/internal/ui"
)

type env struct {
	t         *testing.T
	hosts     *hoststest.TemporaryHostsFile
	configDir string
}

func newEnv(t *testing.T) *env {
	return &env{t: t, hosts: hoststest.New(t, hoststest.DefaultContent), configDir: filepath.Join(t.TempDir(), "cfg")}
}

func (e *env) cli(args ...string) (int, string) {
	e.t.Helper()
	var out, errOut bytes.Buffer
	code := cli.Run(cli.Env{
		Args:   args,
		Stdout: &out,
		Stderr: &errOut,
		Getenv: func(k string) string {
			return map[string]string{"LOCALDNS_HOSTS_FILE": e.hosts.Path(), "LOCALDNS_CONFIG_DIR": e.configDir}[k]
		},
	})
	return code, out.String() + errOut.String()
}

func (e *env) manager() *core.Manager {
	return core.New(core.Options{Hosts: hosts.NewDiskFile(e.hosts.Path()), Store: config.NewStore(e.configDir)})
}

// startUI serves the real UI handler on a loopback port.
func (e *env) startUI() (baseURL, token string) {
	e.t.Helper()
	ln, err := ui.Listen(0)
	if err != nil {
		e.t.Fatal(err)
	}
	token = "integration-token"
	h, err := ui.NewHandler(e.manager(), ui.Options{Port: ln.Addr().(*net.TCPAddr).Port, Token: token})
	if err != nil {
		e.t.Fatal(err)
	}
	srv := httptest.NewUnstartedServer(h)
	_ = srv.Listener.Close()
	srv.Listener = ln
	srv.Start()
	e.t.Cleanup(srv.Close)
	return srv.URL, token
}

func api(t *testing.T, method, url, token, body string) (int, map[string]any) {
	t.Helper()
	req, _ := http.NewRequest(method, url, strings.NewReader(body))
	req.Header.Set(ui.TokenHeader, token)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	data, _ := io.ReadAll(resp.Body)
	var v map[string]any
	_ = json.Unmarshal(data, &v)
	return resp.StatusCode, v
}

// The CLI and the web UI are two front ends to the same core: changes made
// through one are immediately visible through the other.
func TestCLIAndUIShareTheSameCore(t *testing.T) {
	e := newEnv(t)
	base, token := e.startUI()

	if code, out := e.cli("add", "app.local", "127.0.0.1:3000"); code != 0 {
		t.Fatalf("cli add: %d %s", code, out)
	}
	status, body := api(t, "GET", base+"/api/entries", token, "")
	entries, _ := body["entries"].([]any)
	if status != 200 || len(entries) != 1 || entries[0].(map[string]any)["url"] != "http://app.local:3000" {
		t.Fatalf("UI does not see CLI change: %d %v", status, body)
	}

	status, _ = api(t, "POST", base+"/api/entries", token, `{"hostname":"api.local","address":"127.0.0.1:3002"}`)
	if status != http.StatusCreated {
		t.Fatalf("ui add: %d", status)
	}
	code, out := e.cli("list", "--json")
	if code != 0 || !strings.Contains(out, `"hostname": "api.local"`) || !strings.Contains(out, `"port": 3002`) {
		t.Fatalf("CLI does not see UI change: %s", out)
	}

	status, _ = api(t, "DELETE", base+"/api/entries/app.local", token, "")
	if status != 200 {
		t.Fatalf("ui delete: %d", status)
	}
	if code, _ := e.cli("remove", "api.local", "--yes"); code != 0 {
		t.Fatal("cli remove failed")
	}
	if e.hosts.Content() != hoststest.DefaultContent {
		t.Fatalf("hosts file not restored:\n%s", e.hosts.Content())
	}
}

// Full lifecycle on a realistic hosts file: every user entry survives and the
// file is byte-for-byte identical after uninstall.
func TestLifecyclePreservesUserEntries(t *testing.T) {
	e := newEnv(t)
	original := e.hosts.Content()
	steps := [][]string{
		{"add", "app.local", "127.0.0.1:3000"},
		{"add", "api.local", "127.0.0.1:3002"},
		{"add", "ha.local", "192.168.1.60:8123"},
		{"add", "app.local", "127.0.0.1:3000"},            // idempotent
		{"add", "api.local", "127.0.0.1:4000", "--force"}, // update
		{"remove", "ha.local", "--yes"},
	}
	for _, s := range steps {
		if code, out := e.cli(s...); code != 0 {
			t.Fatalf("%v: %d %s", s, code, out)
		}
		doc := hosts.Parse([]byte(e.hosts.Content()))
		if doc.Err() != nil {
			t.Fatalf("%v produced an invalid file: %v", s, doc.Err())
		}
		for _, name := range []string{"localhost", "broadcasthost", "nas", "nas.home.arpa", "printer.lan"} {
			if m, ok := doc.Lookup(name); !ok || m.Managed {
				t.Fatalf("after %v user entry %s was lost", s, name)
			}
		}
	}
	code, out := e.cli("list", "--json")
	var list struct{ Entries []core.Entry }
	if err := json.Unmarshal([]byte(out), &list); err != nil || code != 0 || len(list.Entries) != 2 {
		t.Fatalf("list: %d %s", code, out)
	}
	if code, out := e.cli("uninstall", "--yes", "--keep-binary"); code != 0 {
		t.Fatalf("uninstall: %d %s", code, out)
	}
	if e.hosts.Content() != original {
		t.Fatalf("hosts file differs after uninstall:\n%s", e.hosts.Content())
	}
}

// Names LocalDNS does not manage are never written to the hosts file, so the
// operating system keeps resolving them with its normal DNS.
func TestInternetDomainsAreNeverIntercepted(t *testing.T) {
	e := newEnv(t)
	for _, args := range [][]string{
		{"add", "app.local", "127.0.0.1:3000"},
		{"add", "google.com", "127.0.0.1"},
		{"add", "example.com", "127.0.0.1", "--force"},
		{"add", "www.github.com", "192.168.1.1"},
	} {
		e.cli(args...)
	}
	doc := hosts.Parse([]byte(e.hosts.Content()))
	for _, name := range []string{"google.com", "example.com", "www.github.com"} {
		if _, ok := doc.Lookup(name); ok {
			t.Errorf("%s would be intercepted by the hosts file", name)
		}
	}
	if _, ok := doc.Lookup("app.local"); !ok {
		t.Fatal("app.local should resolve through the hosts file")
	}
}

// LocalDNS must not contain a DNS server or any network listener other than
// the loopback-only web UI and the optional loopback-only router. This audits
// the source code itself.
func TestOnlyLoopbackUIListens(t *testing.T) {
	root := filepath.Join("..", "..")
	listenFuncs := map[string]bool{
		"Listen": true, "ListenPacket": true, "ListenUDP": true, "ListenTCP": true, "ListenIP": true,
		"ListenUnix": true, "ListenMulticastUDP": true, "ListenAndServe": true, "ListenAndServeTLS": true,
		"FileListener": true, "FilePacketConn": true,
		"Dial": true, "DialTimeout": true, "DialUDP": true, "DialTCP": true, "LookupHost": true, "LookupIP": true,
	}
	var found []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		// test/ and tools/ (release packaging, CI file server) are not part
		// of the localdns binary.
		if d.IsDir() && (d.Name() == ".git" || d.Name() == "test" || d.Name() == "tools") {
			return filepath.SkipDir
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		f, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			return err
		}
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			if pkg, ok := sel.X.(*ast.Ident); ok && (pkg.Name == "net" || pkg.Name == "http") && listenFuncs[sel.Sel.Name] {
				rel, _ := filepath.Rel(root, path)
				found = append(found, filepath.ToSlash(rel)+": "+pkg.Name+"."+sel.Sel.Name)
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"internal/router/router.go: net.Listen",
		"internal/router/router.go: net.FileListener", // the socket launchd opens on macOS
		"internal/ui/server.go: net.Listen",
	}
	if strings.Join(found, "\n") != strings.Join(want, "\n") {
		t.Fatalf("unexpected network calls:\n%s\nwant only:\n%s", strings.Join(found, "\n"), strings.Join(want, "\n"))
	}

	// And both listeners are bound to the loopback interface.
	for _, l := range []struct{ file, call, constant string }{
		{"internal/ui/server.go", `net.Listen("tcp", net.JoinHostPort(LoopbackAddr,`, `LoopbackAddr = "127.0.0.1"`},
		{"internal/router/router.go", `net.Listen("tcp", net.JoinHostPort(ListenAddr,`, `const ListenAddr = "127.0.0.1"`},
		// An inherited socket is refused unless it is bound to loopback.
		{"internal/router/router.go", `if !isLoopback(ln.Addr()) {`, `ap.Addr().Unmap().IsLoopback()`},
	} {
		src, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(l.file)))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(src), l.call) || !strings.Contains(string(src), l.constant) {
			t.Fatalf("%s: listener is not pinned to 127.0.0.1", l.file)
		}
	}
}

// The core never creates or rewrites anything except the hosts file section
// and its own config directory.
func TestFilesystemFootprint(t *testing.T) {
	e := newEnv(t)
	hostsDir := filepath.Dir(e.hosts.Path())
	e.cli("add", "app.local", "127.0.0.1:3000")
	e.cli("remove", "app.local", "--yes")

	entries, _ := os.ReadDir(hostsDir)
	if len(entries) != 1 || entries[0].Name() != "hosts" {
		var names []string
		for _, en := range entries {
			names = append(names, en.Name())
		}
		t.Fatalf("unexpected files next to the hosts file: %v", names)
	}
	cfgEntries, _ := os.ReadDir(e.configDir)
	for _, en := range cfgEntries {
		if en.Name() != config.FileName && en.Name() != config.BackupName {
			t.Fatalf("unexpected file in config dir: %s", en.Name())
		}
	}
}
