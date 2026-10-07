package core

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/devehab/locly-dns/internal/config"
	"github.com/devehab/locly-dns/internal/hosts"
	"github.com/devehab/locly-dns/internal/hosts/hoststest"
)

type fixture struct {
	t     *testing.T
	hosts *hoststest.TemporaryHostsFile
	store *config.Store
	m     *Manager
}

func newFixture(t *testing.T, content string) *fixture {
	t.Helper()
	hf := hoststest.New(t, content)
	store := config.NewStore(filepath.Join(t.TempDir(), "localdns"))
	now := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	m := New(Options{Hosts: hf, Store: store, Now: func() time.Time { return now }, PermissionHint: "use sudo"})
	return &fixture{t: t, hosts: hf, store: store, m: m}
}

func (f *fixture) add(host, addr string) Result {
	f.t.Helper()
	res, err := f.m.Add(host, addr, AddOptions{})
	if err != nil {
		f.t.Fatalf("Add(%s, %s): %v", host, addr, err)
	}
	return res
}

func wantCode(t *testing.T, err error, code Code) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected error with code %s, got nil", code)
	}
	if got := ErrorCode(err); got != code {
		t.Fatalf("error code = %s, want %s (%v)", got, code, err)
	}
}

// outsideSection returns the hosts content with the LocalDNS section cut out.
func outsideSection(content string) string {
	start := strings.Index(content, hosts.BeginMarker)
	if start < 0 {
		return content
	}
	end := strings.Index(content, hosts.EndMarker)
	return content[:start] + content[end+len(hosts.EndMarker)+1:]
}

// ---------------------------------------------------------------------------
// The most important property of LocalDNS: it only ever changes its own
// section. Existing entries are preserved byte for byte through add, update
// and remove, and removing the last entry restores the original file.
// ---------------------------------------------------------------------------

func TestExistingEntriesArePreserved(t *testing.T) {
	f := newFixture(t, hoststest.DefaultContent)
	original := f.hosts.Content()

	// Before: only the user's entries exist.
	before := hosts.Parse([]byte(original))
	if before.HasSection() {
		t.Fatal("fixture should start without a LocalDNS section")
	}

	// After add: the new entries exist, old entries are untouched, format is correct.
	f.add("app.local", "127.0.0.1:3000")
	f.add("api.local", "127.0.0.1:3002")
	f.add("ha.local", "192.168.1.60")
	content := f.hosts.Content()
	if outsideSection(content) != original {
		t.Fatalf("content outside the LocalDNS section changed:\n--- before\n%s\n--- after\n%s", original, content)
	}
	wantSection := "# BEGIN LOCALDNS\n127.0.0.1 app.local\n127.0.0.1 api.local\n192.168.1.60 ha.local\n# END LOCALDNS\n"
	if !strings.HasSuffix(content, wantSection) {
		t.Fatalf("section format wrong:\n%s", content)
	}
	doc := hosts.Parse([]byte(content))
	for _, name := range []string{"localhost", "broadcasthost", "nas.home.arpa", "nas", "printer.lan"} {
		m, ok := doc.Lookup(name)
		if !ok || m.Managed {
			t.Errorf("user entry %s lost or taken over: %+v", name, m)
		}
	}

	// After remove: the entry is gone, old entries remain.
	if _, err := f.m.Remove("api.local"); err != nil {
		t.Fatal(err)
	}
	content = f.hosts.Content()
	if strings.Contains(content, "api.local") {
		t.Fatal("api.local still present after remove")
	}
	if outsideSection(content) != original {
		t.Fatal("content outside the section changed after remove")
	}

	// Removing everything restores the file exactly.
	for _, h := range []string{"app.local", "ha.local"} {
		if _, err := f.m.Remove(h); err != nil {
			t.Fatal(err)
		}
	}
	if got := f.hosts.Content(); got != original {
		t.Fatalf("file not restored exactly:\n--- want\n%q\n--- got\n%q", original, got)
	}
}

func TestPreservesWindowsLineEndings(t *testing.T) {
	original := "# Copyright (c) 1993-2009 Microsoft Corp.\r\n#\r\n# 127.0.0.1       localhost\r\n10.1.1.1 corp.lan\r\n"
	f := newFixture(t, original)
	f.add("app.local", "127.0.0.1:3000")
	content := f.hosts.Content()
	if !strings.HasPrefix(content, original) || !strings.Contains(content, "127.0.0.1 app.local\r\n") {
		t.Fatalf("CRLF not preserved: %q", content)
	}
	if _, err := f.m.Remove("app.local"); err != nil {
		t.Fatal(err)
	}
	if f.hosts.Content() != original {
		t.Fatal("CRLF file not restored")
	}
}

// Internet DNS behavior: names LocalDNS does not manage are never
// intercepted. The hosts file never mentions them, so the operating system
// resolves them through its normal DNS configuration.
func TestUnknownDomainsAreUntouched(t *testing.T) {
	f := newFixture(t, hoststest.DefaultContent)
	f.add("app.local", "127.0.0.1:3000")
	f.add("ha.local", "192.168.1.60:8123")

	internet := []string{"google.com", "example.com", "www.google.com", "github.com", "api.openai.com", "localhost.com"}
	content := f.hosts.Content()
	doc := hosts.Parse([]byte(content))
	for _, name := range internet {
		if _, ok := doc.Lookup(name); ok {
			t.Errorf("%s appears in the hosts file; it would be intercepted", name)
		}
		if strings.Contains(content, name) {
			t.Errorf("hosts file mentions %s", name)
		}
		if _, err := f.m.Get(name); ErrorCode(err) != CodeNotFound {
			t.Errorf("Get(%s) = %v, want not_found", name, err)
		}
	}

	// LocalDNS refuses to map internet domains at all, so it cannot be used
	// to hijack them, and a refused request leaves the file untouched.
	for _, name := range internet {
		_, err := f.m.Add(name, "127.0.0.1", AddOptions{Force: true})
		wantCode(t, err, CodeInvalidInput)
	}
	if f.hosts.Content() != content {
		t.Fatal("a refused add modified the hosts file")
	}
}

func TestAddAndList(t *testing.T) {
	f := newFixture(t, hoststest.DefaultContent)
	res := f.add("App.Local", "127.0.0.1:3000")
	if res.Action != ActionAdded || res.Entry.Hostname != "app.local" || res.Entry.URL != "http://app.local:3000" {
		t.Fatalf("unexpected result: %+v", res)
	}
	f.add("ha.local", "192.168.1.60")
	f.add("v6.local", "[::1]:8080")

	list, err := f.m.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 3 {
		t.Fatalf("got %d entries", len(list))
	}
	want := []struct {
		host, ip, addr, url string
		port                uint16
	}{
		{"app.local", "127.0.0.1", "127.0.0.1:3000", "http://app.local:3000", 3000},
		{"ha.local", "192.168.1.60", "192.168.1.60", "http://ha.local", 0},
		{"v6.local", "::1", "[::1]:8080", "http://v6.local:8080", 8080},
	}
	for i, w := range want {
		e := list[i]
		if e.Hostname != w.host || e.IP != w.ip || e.Address != w.addr || e.URL != w.url || e.Status != StatusActive {
			t.Errorf("entry %d = %+v, want %+v", i, e, w)
		}
		if (w.port == 0) != (e.Port == nil) || (e.Port != nil && *e.Port != w.port) {
			t.Errorf("entry %d port = %v, want %d", i, e.Port, w.port)
		}
	}

	cfg, err := f.store.Load()
	if err != nil || cfg.Find("app.local").Port != 3000 {
		t.Fatalf("port metadata not saved: %+v %v", cfg, err)
	}
	if !strings.Contains(f.hosts.Content(), "127.0.0.1 app.local\n") {
		t.Fatal("hosts entry must not contain the port")
	}
}

func TestListEmpty(t *testing.T) {
	f := newFixture(t, hoststest.DefaultContent)
	list, err := f.m.List()
	if err != nil || list == nil || len(list) != 0 {
		t.Fatalf("List = %v, %v; want empty non-nil", list, err)
	}
}

func TestDuplicateAddIsIdempotent(t *testing.T) {
	f := newFixture(t, hoststest.DefaultContent)
	f.add("app.local", "127.0.0.1:3000")
	content := f.hosts.Content()
	res := f.add("app.local", "127.0.0.1:3000")
	if res.Action != ActionUnchanged {
		t.Fatalf("action = %s, want unchanged", res.Action)
	}
	if f.hosts.Content() != content {
		t.Fatal("idempotent add rewrote the hosts file")
	}
	if strings.Count(f.hosts.Content(), "app.local") != 1 {
		t.Fatal("duplicate line written")
	}
}

func TestDuplicateWithDifferentAddressNeedsForce(t *testing.T) {
	f := newFixture(t, hoststest.DefaultContent)
	f.add("app.local", "127.0.0.1:3000")
	_, err := f.m.Add("app.local", "127.0.0.1:4000", AddOptions{})
	wantCode(t, err, CodeExists)
	if !strings.Contains(AsError(err).Hint, "--force") {
		t.Errorf("hint should mention --force: %q", AsError(err).Hint)
	}
}

func TestUpdateWithForce(t *testing.T) {
	f := newFixture(t, hoststest.DefaultContent)
	f.add("app.local", "127.0.0.1:3000")
	f.add("ha.local", "192.168.1.60")

	// Port-only change: the hosts file stays the same, the config changes.
	before := f.hosts.Content()
	res, err := f.m.Add("app.local", "127.0.0.1:4000", AddOptions{Force: true})
	if err != nil || res.Action != ActionUpdated || *res.Entry.Port != 4000 {
		t.Fatalf("update = %+v, %v", res, err)
	}
	if f.hosts.Content() != before {
		t.Fatal("port-only update should not rewrite the hosts file")
	}

	// IP change keeps the entry's position.
	if _, err := f.m.Add("app.local", "10.0.0.7", AddOptions{Force: true}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(f.hosts.Content(), "# BEGIN LOCALDNS\n10.0.0.7 app.local\n192.168.1.60 ha.local\n") {
		t.Fatalf("update did not happen in place:\n%s", f.hosts.Content())
	}
	e, _ := f.m.Get("app.local")
	if e.Port != nil {
		t.Fatalf("port should be cleared: %+v", e)
	}
}

func TestConflictWithUnmanagedEntry(t *testing.T) {
	f := newFixture(t, hoststest.DefaultContent)
	original := f.hosts.Content()
	// printer.lan is defined by the user outside the LocalDNS section.
	_, err := f.m.Add("printer.lan", "127.0.0.1", AddOptions{Force: true})
	wantCode(t, err, CodeConflict)
	if !strings.Contains(err.Error(), "line 13") {
		t.Errorf("error should point at the conflicting line: %v", err)
	}
	if f.hosts.Content() != original {
		t.Fatal("conflicting add modified the hosts file")
	}
	// And it cannot be removed through LocalDNS either.
	_, err = f.m.Remove("printer.lan")
	wantCode(t, err, CodeNotManaged)
	if f.hosts.Content() != original {
		t.Fatal("remove of unmanaged entry modified the hosts file")
	}
}

func TestShadowedEntryIsReportedAsConflict(t *testing.T) {
	f := newFixture(t, hoststest.DefaultContent)
	f.add("app.local", "127.0.0.1:3000")
	// The user later adds an overriding line above the section.
	f.hosts.SetContent("10.9.9.9 app.local\n" + f.hosts.Content())
	list, err := f.m.List()
	if err != nil {
		t.Fatal(err)
	}
	if list[0].Status != StatusConflict || !strings.Contains(list[0].Detail, "line 1") {
		t.Fatalf("expected conflict status, got %+v", list[0])
	}
}

func TestRemove(t *testing.T) {
	f := newFixture(t, hoststest.DefaultContent)
	f.add("app.local", "127.0.0.1:3000")
	f.add("api.local", "127.0.0.1:3002")
	res, err := f.m.Remove("APP.local")
	if err != nil || res.Action != ActionRemoved || res.Entry.Address != "127.0.0.1:3000" {
		t.Fatalf("Remove = %+v, %v", res, err)
	}
	cfg, _ := f.store.Load()
	if cfg.Find("app.local") != nil {
		t.Fatal("config entry not removed")
	}
	_, err = f.m.Remove("app.local")
	wantCode(t, err, CodeNotFound)
	_, err = f.m.Remove("bad host!")
	wantCode(t, err, CodeInvalidInput)
}

func TestMissingEntryCanBeRemovedOrRestored(t *testing.T) {
	f := newFixture(t, hoststest.DefaultContent)
	original := f.hosts.Content()
	f.add("app.local", "127.0.0.1:3000")
	// Someone deletes the section by hand; the config still remembers it.
	f.hosts.SetContent(original)
	list, _ := f.m.List()
	if len(list) != 1 || list[0].Status != StatusMissing {
		t.Fatalf("expected a missing entry, got %+v", list)
	}
	// Re-adding restores it.
	res := f.add("app.local", "127.0.0.1:3000")
	if res.Action != ActionAdded {
		t.Fatalf("restore action = %s", res.Action)
	}
	// Remove the section again and remove the stale entry: no hosts write needed.
	f.hosts.SetContent(original)
	if _, err := f.m.Remove("app.local"); err != nil {
		t.Fatal(err)
	}
	if f.hosts.Content() != original {
		t.Fatal("hosts file changed")
	}
}

func TestMalformedSectionBlocksWrites(t *testing.T) {
	broken := hoststest.DefaultContent + "# BEGIN LOCALDNS\n127.0.0.1 app.local\n"
	f := newFixture(t, broken)
	_, err := f.m.Add("x.local", "127.0.0.1", AddOptions{})
	wantCode(t, err, CodeHostsInvalid)
	_, err = f.m.Remove("app.local")
	wantCode(t, err, CodeHostsInvalid)
	_, err = f.m.List()
	wantCode(t, err, CodeHostsInvalid)
	if f.hosts.Content() != broken {
		t.Fatal("malformed file was modified")
	}
	st, err := f.m.Status()
	if err != nil || st.Section != SectionInvalid || st.Healthy {
		t.Fatalf("Status = %+v, %v", st, err)
	}
}

func TestMalformedEntryInSection(t *testing.T) {
	broken := "127.0.0.1 localhost\n# BEGIN LOCALDNS\n127.0.0.1 app.local\nnonsense here\n# END LOCALDNS\n"
	f := newFixture(t, broken)
	_, err := f.m.Add("x.local", "127.0.0.1", AddOptions{})
	wantCode(t, err, CodeHostsInvalid)
	if !strings.Contains(err.Error(), "line 4") {
		t.Errorf("error should name the line: %v", err)
	}
	if f.hosts.Content() != broken {
		t.Fatal("file with malformed entry was modified")
	}
}

func TestInvalidInput(t *testing.T) {
	f := newFixture(t, hoststest.DefaultContent)
	cases := [][2]string{
		{"google.com", "127.0.0.1"},
		{"app.local", "8.8.8.8"},
		{"app.local", "localhost:3000"},
		{"app.local", "127.0.0.1:0"},
		{"localhost", "127.0.0.1"},
		{"app.local:3000", "127.0.0.1"},
		{"", "127.0.0.1"},
	}
	for _, c := range cases {
		_, err := f.m.Add(c[0], c[1], AddOptions{})
		wantCode(t, err, CodeInvalidInput)
		if AsError(err).Hint == "" {
			t.Errorf("Add(%q, %q): error has no hint", c[0], c[1])
		}
	}
	if f.hosts.Content() != hoststest.DefaultContent {
		t.Fatal("invalid input modified the hosts file")
	}
}

func TestHostsFileMissing(t *testing.T) {
	f := newFixture(t, "")
	if err := os.Remove(f.hosts.Path()); err != nil {
		t.Fatal(err)
	}
	_, err := f.m.Add("app.local", "127.0.0.1", AddOptions{})
	wantCode(t, err, CodeHostsNotFound)
	if _, err := os.Stat(f.hosts.Path()); !os.IsNotExist(err) {
		t.Fatal("LocalDNS must not create a missing hosts file")
	}
}

func TestInvalidConfigIsReported(t *testing.T) {
	f := newFixture(t, hoststest.DefaultContent)
	if err := os.MkdirAll(f.store.Dir(), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(f.store.Path(), []byte("{nope"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := f.m.Add("app.local", "127.0.0.1", AddOptions{})
	wantCode(t, err, CodeConfigInvalid)
	if f.hosts.Content() != hoststest.DefaultContent {
		t.Fatal("hosts modified despite invalid config")
	}
}

func TestBackupIsWritten(t *testing.T) {
	f := newFixture(t, hoststest.DefaultContent)
	f.add("app.local", "127.0.0.1:3000")
	b, err := os.ReadFile(f.store.BackupPath())
	if err != nil || string(b) != hoststest.DefaultContent {
		t.Fatalf("backup = %q, %v", b, err)
	}
}

// readOnlyHosts wraps a hosts file and reports it as not writable.
type readOnlyHosts struct{ hosts.File }

func (readOnlyHosts) CheckWritable() error {
	return &fs.PathError{Op: "open", Path: "hosts", Err: fs.ErrPermission}
}

func TestPermissionDeniedChangesNothing(t *testing.T) {
	f := newFixture(t, hoststest.DefaultContent)
	m := New(Options{Hosts: readOnlyHosts{f.hosts}, Store: f.store, PermissionHint: "Run it with sudo"})
	_, err := m.Add("app.local", "127.0.0.1:3000", AddOptions{})
	wantCode(t, err, CodePermission)
	if AsError(err).Hint != "Run it with sudo" {
		t.Errorf("hint = %q", AsError(err).Hint)
	}
	if f.hosts.Content() != hoststest.DefaultContent || f.store.Exists() {
		t.Fatal("permission failure left partial changes")
	}
	// Reading still works without privileges.
	if _, err := m.List(); err != nil {
		t.Fatal(err)
	}
	if st, _ := m.Status(); st.Writable {
		t.Fatal("status should report read-only")
	}
}

// failingStore makes config saves fail to test rollback.
func TestConfigFailureRollsBackHosts(t *testing.T) {
	f := newFixture(t, hoststest.DefaultContent)
	// Make the config path a directory so saving the file fails.
	if err := os.MkdirAll(filepath.Join(f.store.Dir(), config.FileName), 0o755); err != nil {
		t.Fatal(err)
	}
	_, err := f.m.Add("app.local", "127.0.0.1", AddOptions{})
	if err == nil {
		t.Fatal("expected an error")
	}
	if f.hosts.Content() != hoststest.DefaultContent {
		t.Fatalf("hosts file not rolled back:\n%s", f.hosts.Content())
	}
}

func TestConcurrentAdds(t *testing.T) {
	f := newFixture(t, hoststest.DefaultContent)
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if _, err := f.m.Add(fmt.Sprintf("svc%d.local", i), fmt.Sprintf("127.0.0.1:%d", 3000+i), AddOptions{}); err != nil {
				t.Error(err)
			}
		}(i)
	}
	wg.Wait()
	list, err := f.m.List()
	if err != nil || len(list) != 20 {
		t.Fatalf("List = %d entries, %v", len(list), err)
	}
	if outsideSection(f.hosts.Content()) != hoststest.DefaultContent {
		t.Fatal("concurrent adds damaged user entries")
	}
}

func TestPurge(t *testing.T) {
	f := newFixture(t, hoststest.DefaultContent)
	f.add("app.local", "127.0.0.1:3000")
	f.add("ha.local", "192.168.1.60")
	plan, err := f.m.PlanPurge()
	if err != nil || plan.Entries != 2 || !plan.SectionExists || !plan.ConfigExists {
		t.Fatalf("plan = %+v, %v", plan, err)
	}
	res, err := f.m.Purge()
	if err != nil || res.EntriesRemoved != 2 || !res.ConfigRemoved {
		t.Fatalf("Purge = %+v, %v", res, err)
	}
	if f.hosts.Content() != hoststest.DefaultContent {
		t.Fatalf("purge did not restore the hosts file:\n%s", f.hosts.Content())
	}
	if _, err := os.Stat(f.store.Dir()); !os.IsNotExist(err) {
		t.Fatal("config dir not removed")
	}
	// Purging again is harmless.
	if res, err := f.m.Purge(); err != nil || res.EntriesRemoved != 0 {
		t.Fatalf("second purge = %+v, %v", res, err)
	}
}

func TestPurgeRemovesSectionWithBadContentButNotBrokenMarkers(t *testing.T) {
	f := newFixture(t, "127.0.0.1 localhost\n# BEGIN LOCALDNS\ngarbage\n# END LOCALDNS\n")
	if _, err := f.m.Purge(); err != nil {
		t.Fatal(err)
	}
	if f.hosts.Content() != "127.0.0.1 localhost\n" {
		t.Fatalf("got %q", f.hosts.Content())
	}

	broken := "127.0.0.1 localhost\n# BEGIN LOCALDNS\n127.0.0.1 a.local\n"
	f2 := newFixture(t, broken)
	_, err := f2.m.Purge()
	wantCode(t, err, CodeHostsInvalid)
	if f2.hosts.Content() != broken {
		t.Fatal("purge modified a file with broken markers")
	}
}

func TestStatus(t *testing.T) {
	f := newFixture(t, hoststest.DefaultContent)
	st, err := f.m.Status()
	if err != nil || st.Section != SectionAbsent || !st.Healthy || !st.Writable || st.Counts.Total != 0 {
		t.Fatalf("Status = %+v, %v", st, err)
	}
	f.add("app.local", "127.0.0.1:3000")
	st, _ = f.m.Status()
	if st.Section != SectionPresent || st.Counts.Active != 1 || st.HostsFile != f.hosts.Path() {
		t.Fatalf("Status = %+v", st)
	}
}

func TestDoctor(t *testing.T) {
	f := newFixture(t, hoststest.DefaultContent)
	extra := func() Check { return Check{ID: "ui_available", Name: "Local UI available", Status: CheckPass} }
	r := f.m.Doctor(extra)
	if !r.OK || len(r.Checks) != 7 {
		t.Fatalf("doctor = %+v", r)
	}
	names := []string{
		"Operating system supported", "Hosts file found", "Hosts file readable", "Hosts file writable",
		"LocalDNS section valid", "Configuration valid", "Local UI available",
	}
	for i, n := range names {
		if r.Checks[i].Name != n || r.Checks[i].Status != CheckPass {
			t.Errorf("check %d = %+v, want pass %q", i, r.Checks[i], n)
		}
	}

	// A broken section fails with a fix.
	f.hosts.SetContent(hoststest.DefaultContent + "# END LOCALDNS\n")
	r = f.m.Doctor()
	if r.OK {
		t.Fatal("doctor should fail on a broken section")
	}
	for _, c := range r.Checks {
		if c.ID == "section_valid" && (c.Status != CheckFail || c.Fix == "") {
			t.Fatalf("section check = %+v", c)
		}
	}

	// A missing hosts file fails.
	_ = os.Remove(f.hosts.Path())
	if r := f.m.Doctor(); r.OK {
		t.Fatal("doctor should fail without a hosts file")
	}
}

func TestDoctorPermissionWarning(t *testing.T) {
	f := newFixture(t, hoststest.DefaultContent)
	m := New(Options{Hosts: readOnlyHosts{f.hosts}, Store: f.store, PermissionHint: "use sudo"})
	r := m.Doctor()
	if !r.OK {
		t.Fatal("read-only hosts file is a warning, not a failure")
	}
	if c := r.Checks[3]; c.ID != "hosts_writable" || c.Status != CheckWarn || c.Fix != "use sudo" {
		t.Fatalf("writable check = %+v", c)
	}
	m = New(Options{Hosts: readOnlyHosts{f.hosts}, Store: f.store, CanElevate: true})
	if c := m.Doctor().Checks[3]; c.Status != CheckPass {
		t.Fatalf("with sudo available the check should pass: %+v", c)
	}
}

func TestErrorHelpers(t *testing.T) {
	if ErrorCode(errors.New("x")) != CodeInternal || AsError(errors.New("x")).Code != CodeInternal {
		t.Fatal("plain errors are internal")
	}
	e := &Error{Code: CodeNotFound, Message: "m"}
	if ErrorCode(fmt.Errorf("wrap: %w", e)) != CodeNotFound {
		t.Fatal("wrapped code lost")
	}
}
