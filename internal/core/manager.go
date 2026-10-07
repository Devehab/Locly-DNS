// Package core is the LocalDNS business logic shared by the CLI and the web
// UI. It maps local hostnames to local IP addresses (with optional port
// metadata) by editing only the LocalDNS section of the hosts file.
//
// The package deliberately has no networking code: it cannot open sockets,
// serve DNS or make outbound requests. An architecture test enforces this.
package core

import (
	"bytes"
	"errors"
	"io/fs"
	"net/netip"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/devehab/locly-dns/internal/config"
	"github.com/devehab/locly-dns/internal/fsutil"
	"github.com/devehab/locly-dns/internal/hosts"
	"github.com/devehab/locly-dns/internal/validate"
)

// Status of a managed entry.
type Status string

// Entry statuses.
const (
	StatusActive   Status = "active"   // in the hosts file and effective
	StatusMissing  Status = "missing"  // known to LocalDNS but missing from the hosts file
	StatusConflict Status = "conflict" // overridden by an earlier line outside the LocalDNS section
	StatusPaused   Status = "paused"   // switched off by the user: kept by LocalDNS, not in the hosts file
)

// Entry is a managed hostname as presented to users and agents.
type Entry struct {
	Hostname string  `json:"hostname"`
	IP       string  `json:"ip"`
	Port     *uint16 `json:"port"`
	Address  string  `json:"address"`
	URL      string  `json:"url"`
	Status   Status  `json:"status"`
	Detail   string  `json:"detail,omitempty"`
	// ShortURL is the port-free address (http://app.local) when the
	// LocalDNS router is running and serves this name. Filled in by the
	// CLI and web UI, which can check the router; empty otherwise.
	ShortURL string `json:"short_url,omitempty"`
}

// Routable reports whether the port-free router can serve this entry: it
// has a port and its name resolves to 127.0.0.1, where the router listens.
func (e Entry) Routable() bool {
	if e.Port == nil || e.Status == StatusMissing || e.Status == StatusPaused {
		return false
	}
	ip, err := netip.ParseAddr(e.IP)
	return err == nil && ip.Unmap() == routerAddr
}

var routerAddr = netip.MustParseAddr("127.0.0.1")

// Action describes what a mutating call did.
type Action string

// Actions.
const (
	ActionAdded     Action = "added"
	ActionUpdated   Action = "updated"
	ActionUnchanged Action = "unchanged"
	ActionRemoved   Action = "removed"
	ActionPaused    Action = "paused"
	ActionResumed   Action = "resumed"
)

// Result is returned by Add and Remove.
type Result struct {
	Action Action `json:"action"`
	Entry  Entry  `json:"entry"`
}

// Options configures a Manager.
type Options struct {
	Hosts hosts.File
	Store *config.Store
	// Now returns the current time; defaults to time.Now.
	Now func() time.Time
	// PermissionHint is shown when the hosts file or config is not writable.
	PermissionHint string
	// CanElevate tells doctor that the caller can obtain administrator rights
	// on demand (e.g. via sudo), so a read-only hosts file is expected.
	CanElevate bool
}

// Manager performs LocalDNS operations. It is safe for concurrent use.
type Manager struct {
	opts Options
	mu   sync.Mutex
}

// New returns a Manager.
func New(opts Options) *Manager {
	if opts.Now == nil {
		opts.Now = time.Now
	}
	return &Manager{opts: opts}
}

// HostsPath is the hosts file being managed.
func (m *Manager) HostsPath() string { return m.opts.Hosts.Path() }

// ConfigPath is the LocalDNS config file.
func (m *Manager) ConfigPath() string { return m.opts.Store.Path() }

// ConfigDir is the LocalDNS config directory.
func (m *Manager) ConfigDir() string { return m.opts.Store.Dir() }

type state struct {
	raw []byte
	doc *hosts.Document
	cfg *config.Config
}

func (m *Manager) readHosts() ([]byte, error) {
	raw, err := m.opts.Hosts.Read()
	switch {
	case err == nil:
		return raw, nil
	case errors.Is(err, fs.ErrNotExist):
		return nil, newError(CodeHostsNotFound, "Run `localdns doctor` for details.", err,
			"hosts file not found at %s", m.HostsPath())
	case fsutil.IsPermission(err):
		return nil, newError(CodePermission, m.opts.PermissionHint, err,
			"cannot read hosts file %s: permission denied", m.HostsPath())
	}
	return nil, newError(CodeInternal, "", err, "cannot read hosts file %s: %v", m.HostsPath(), err)
}

func (m *Manager) loadConfig() (*config.Config, error) {
	cfg, err := m.opts.Store.Load()
	if err == nil {
		return cfg, nil
	}
	var inv *config.InvalidError
	if errors.As(err, &inv) {
		return nil, newError(CodeConfigInvalid,
			"Delete "+m.ConfigPath()+" and LocalDNS will recreate it (saved ports are forgotten; hosts entries are kept).",
			err, "%v", err)
	}
	return nil, newError(CodeInternal, "", err, "cannot read %s: %v", m.ConfigPath(), err)
}

// load reads the hosts file and config, and requires a valid section.
func (m *Manager) load() (*state, error) {
	raw, err := m.readHosts()
	if err != nil {
		return nil, err
	}
	doc := hosts.Parse(raw)
	if serr := doc.Err(); serr != nil {
		return nil, sectionError(m.HostsPath(), serr)
	}
	cfg, err := m.loadConfig()
	if err != nil {
		return nil, err
	}
	return &state{raw: raw, doc: doc, cfg: cfg}, nil
}

func sectionError(path string, serr *hosts.SectionError) error {
	hint := "Open " + path + " and fix that line, or delete everything between '" +
		hosts.BeginMarker + "' and '" + hosts.EndMarker + "'. Run `localdns doctor` for details."
	if serr.Markers {
		hint = "Open " + path + " and make sure there is exactly one '" + hosts.BeginMarker +
			"' line followed by one '" + hosts.EndMarker + "' line. Run `localdns doctor` for details."
	}
	return newError(CodeHostsInvalid, hint, serr, "%s: %v", path, serr)
}

// entries builds the user-facing entry list: managed hosts entries in file
// order, followed by entries known only to the config (missing from hosts).
func entries(st *state) []Entry {
	var out []Entry
	for _, he := range st.doc.Entries() {
		var port uint16
		if ce := st.cfg.Find(he.Hostname); ce != nil && ce.IP == he.IP.String() {
			port = ce.Port
		}
		e := makeEntry(he.Hostname, he.IP, port, StatusActive)
		if eff, ok := st.doc.Lookup(he.Hostname); ok && !eff.Managed && eff.IP != he.IP {
			e.Status = StatusConflict
			e.Detail = "overridden by line " + strconv.Itoa(eff.Line) + " of the hosts file (" +
				eff.IP.String() + "), which LocalDNS does not manage"
		}
		out = append(out, e)
	}
	for _, ce := range st.cfg.Entries {
		if _, ok := st.doc.Entry(ce.Hostname); ok {
			continue
		}
		ip, _ := netip.ParseAddr(ce.IP)
		if ce.Paused {
			e := makeEntry(ce.Hostname, ip, ce.Port, StatusPaused)
			e.Detail = "paused: left out of the hosts file until you resume it (localdns resume " + ce.Hostname + ")"
			out = append(out, e)
			continue
		}
		e := makeEntry(ce.Hostname, ip, ce.Port, StatusMissing)
		e.Detail = "not present in the hosts file; run `localdns resume " + ce.Hostname +
			"` to restore it or `localdns remove " + ce.Hostname + "` to forget it"
		out = append(out, e)
	}
	return out
}

func makeEntry(hostname string, ip netip.Addr, port uint16, status Status) Entry {
	e := Entry{
		Hostname: hostname,
		IP:       ip.String(),
		Address:  validate.FormatAddress(ip, port),
		URL:      "http://" + hostname,
		Status:   status,
	}
	if port != 0 {
		p := port
		e.Port = &p
		e.URL += ":" + strconv.Itoa(int(port))
	}
	return e
}

// List returns all managed entries.
func (m *Manager) List() ([]Entry, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	st, err := m.load()
	if err != nil {
		return nil, err
	}
	list := entries(st)
	if list == nil {
		list = []Entry{}
	}
	return list, nil
}

// Get returns the managed entry for hostname.
func (m *Manager) Get(hostname string) (Entry, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	name, err := validate.Hostname(hostname)
	if err != nil {
		return Entry{}, invalidInput(err)
	}
	st, err := m.load()
	if err != nil {
		return Entry{}, err
	}
	return m.find(st, name)
}

func (m *Manager) find(st *state, name string) (Entry, error) {
	for _, e := range entries(st) {
		if e.Hostname == name {
			return e, nil
		}
	}
	if un := st.doc.Unmanaged(name); len(un) > 0 {
		return Entry{}, newError(CodeNotManaged,
			"LocalDNS only changes entries between '"+hosts.BeginMarker+"' and '"+hosts.EndMarker+
				"'. Edit "+m.HostsPath()+" yourself to change this one.",
			nil, "%s is defined on line %d of %s, but it is not managed by LocalDNS",
			name, un[0].Line, m.HostsPath())
	}
	return Entry{}, newError(CodeNotFound, "Run `localdns list` to see managed hostnames.", nil,
		"%s is not managed by LocalDNS", name)
}

func invalidInput(err error) error {
	var ve *validate.Error
	if errors.As(err, &ve) {
		return newError(CodeInvalidInput, ve.Hint, err, "%s", ve.Message)
	}
	return newError(CodeInvalidInput, "", err, "%v", err)
}

// AddOptions modifies Add.
type AddOptions struct {
	// Force replaces an existing entry that has a different address.
	Force bool
}

// Add maps hostname to address ("IP" or "IP:PORT"). Adding an identical entry
// again is a no-op (ActionUnchanged), which makes Add idempotent for scripts.
func (m *Manager) Add(hostname, address string, opts AddOptions) (Result, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	name, err := validate.LocalHostname(hostname)
	if err != nil {
		return Result{}, invalidInput(err)
	}
	ip, port, err := validate.Address(address)
	if err != nil {
		return Result{}, invalidInput(err)
	}
	if err := validate.LocalIP(ip); err != nil {
		return Result{}, invalidInput(err)
	}
	if err := validate.Mapping(name, ip); err != nil {
		return Result{}, invalidInput(err)
	}

	st, err := m.load()
	if err != nil {
		return Result{}, err
	}
	if un := st.doc.Unmanaged(name); len(un) > 0 {
		return Result{}, newError(CodeConflict,
			"LocalDNS never edits entries it does not own. Remove that line from "+m.HostsPath()+
				" yourself, or choose a different hostname.",
			nil, "%s is already defined on line %d of %s (%s), outside the LocalDNS section",
			name, un[0].Line, m.HostsPath(), un[0].IP)
	}

	newAddr := validate.FormatAddress(ip, port)
	action := ActionAdded
	list := st.doc.Entries()
	idx := -1
	for i, e := range list {
		if e.Hostname == name {
			idx = i
		}
	}
	ce := st.cfg.Find(name)
	if idx >= 0 {
		cur, _ := m.find(st, name)
		if cur.Address == newAddr {
			return Result{Action: ActionUnchanged, Entry: cur}, nil
		}
		if !opts.Force {
			return Result{}, newError(CodeExists,
				"Use --force to replace it: localdns add "+name+" "+newAddr+" --force",
				nil, "%s already points to %s", name, cur.Address)
		}
		action = ActionUpdated
		list[idx].IP = ip
	} else {
		if ce != nil && (ce.IP != ip.String() || ce.Port != port) {
			// The entry vanished from the hosts file but LocalDNS remembers a
			// different address; restoring it with a new one is a replacement.
			action = ActionUpdated
		}
		list = append(list, hosts.Entry{IP: ip, Hostname: name})
	}

	now := m.opts.Now().UTC().Truncate(time.Second)
	created := now
	if ce != nil {
		created = ce.CreatedAt
	}
	st.cfg.Put(config.Entry{Hostname: name, IP: ip.String(), Port: port, CreatedAt: created, UpdatedAt: now})
	if err := st.doc.SetEntries(list); err != nil {
		return Result{}, newError(CodeInternal, "", err, "%v", err)
	}
	if err := m.commit(st); err != nil {
		return Result{}, err
	}
	return Result{Action: action, Entry: makeEntry(name, ip, port, StatusActive)}, nil
}

// Edit changes a managed entry in one write: its address, its hostname, or
// both. An empty newHostname keeps the name and an empty address keeps the
// address. The entry keeps its place in the hosts file and its creation
// time.
func (m *Manager) Edit(hostname, newHostname, address string) (Result, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	old, err := validate.Hostname(hostname)
	if err != nil {
		return Result{}, invalidInput(err)
	}
	if strings.TrimSpace(newHostname) == "" && strings.TrimSpace(address) == "" {
		return Result{}, newError(CodeInvalidInput,
			"Example: localdns edit "+old+" 127.0.0.1:4000, or localdns edit "+old+" --name web.local",
			nil, "nothing to change: give a new address, a new name, or both")
	}
	st, err := m.load()
	if err != nil {
		return Result{}, err
	}
	cur, err := m.find(st, old)
	if err != nil {
		return Result{}, err
	}

	name := old
	if strings.TrimSpace(newHostname) != "" {
		if name, err = validate.LocalHostname(newHostname); err != nil {
			return Result{}, invalidInput(err)
		}
	}
	ip, err := netip.ParseAddr(cur.IP)
	if err != nil {
		return Result{}, newError(CodeInternal, "", err, "invalid stored address %q", cur.IP)
	}
	var port uint16
	if cur.Port != nil {
		port = *cur.Port
	}
	if strings.TrimSpace(address) != "" {
		if ip, port, err = validate.Address(address); err != nil {
			return Result{}, invalidInput(err)
		}
		if err := validate.LocalIP(ip); err != nil {
			return Result{}, invalidInput(err)
		}
	}
	if err := validate.Mapping(name, ip); err != nil {
		return Result{}, invalidInput(err)
	}
	paused := cur.Status == StatusPaused
	if name == old && validate.FormatAddress(ip, port) == cur.Address && (cur.Status == StatusActive || paused) {
		return Result{Action: ActionUnchanged, Entry: cur}, nil
	}

	if name != old {
		if un := st.doc.Unmanaged(name); len(un) > 0 {
			return Result{}, newError(CodeConflict,
				"LocalDNS never edits entries it does not own. Choose a different hostname.",
				nil, "%s is already defined on line %d of %s (%s), outside the LocalDNS section",
				name, un[0].Line, m.HostsPath(), un[0].IP)
		}
		if _, ok := st.doc.Entry(name); ok || st.cfg.Find(name) != nil {
			return Result{}, newError(CodeExists,
				"Remove it first (localdns remove "+name+") or choose a different hostname.",
				nil, "%s is already managed by LocalDNS", name)
		}
	}

	list := st.doc.Entries()
	replaced := false
	for i := range list {
		if list[i].Hostname == old {
			list[i] = hosts.Entry{IP: ip, Hostname: name}
			replaced = true
		}
	}
	if !replaced && !paused {
		// The line was deleted by hand (status "missing"): restore it. A
		// paused entry stays out of the hosts file.
		list = append(list, hosts.Entry{IP: ip, Hostname: name})
	}
	now := m.opts.Now().UTC().Truncate(time.Second)
	created := now
	if ce := st.cfg.Find(old); ce != nil {
		created = ce.CreatedAt
	}
	st.cfg.Delete(old)
	st.cfg.Put(config.Entry{Hostname: name, IP: ip.String(), Port: port, Paused: paused, CreatedAt: created, UpdatedAt: now})
	if err := st.doc.SetEntries(list); err != nil {
		return Result{}, newError(CodeInternal, "", err, "%v", err)
	}
	if err := m.commit(st); err != nil {
		return Result{}, err
	}
	status := StatusActive
	if paused {
		status = StatusPaused
	}
	return Result{Action: ActionUpdated, Entry: makeEntry(name, ip, port, status)}, nil
}

// Pause switches a managed entry off without forgetting it: its line leaves
// the hosts file, so the name stops working, and Resume puts it back.
func (m *Manager) Pause(hostname string) (Result, error) { return m.setPaused(hostname, true) }

// Resume switches a paused entry back on. It also restores an entry whose
// line was deleted from the hosts file by hand.
func (m *Manager) Resume(hostname string) (Result, error) { return m.setPaused(hostname, false) }

func (m *Manager) setPaused(hostname string, paused bool) (Result, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	name, err := validate.Hostname(hostname)
	if err != nil {
		return Result{}, invalidInput(err)
	}
	st, err := m.load()
	if err != nil {
		return Result{}, err
	}
	cur, err := m.find(st, name)
	if err != nil {
		return Result{}, err
	}
	ip, err := netip.ParseAddr(cur.IP)
	if err != nil {
		return Result{}, newError(CodeInternal, "", err, "invalid stored address %q", cur.IP)
	}
	var port uint16
	if cur.Port != nil {
		port = *cur.Port
	}

	list := st.doc.Entries()
	action := ActionPaused
	status := StatusPaused
	if paused {
		if cur.Status == StatusPaused {
			return Result{Action: ActionUnchanged, Entry: cur}, nil
		}
		var keep []hosts.Entry
		for _, e := range list {
			if e.Hostname != name {
				keep = append(keep, e)
			}
		}
		list = keep
	} else {
		action, status = ActionResumed, StatusActive
		if _, inHosts := st.doc.Entry(name); inHosts {
			return Result{Action: ActionUnchanged, Entry: cur}, nil
		}
		if un := st.doc.Unmanaged(name); len(un) > 0 {
			return Result{}, newError(CodeConflict,
				"LocalDNS never edits entries it does not own. Remove that line from "+m.HostsPath()+
					" yourself, or rename this entry: localdns edit "+name+" --name <new-name>",
				nil, "%s is now defined on line %d of %s (%s), outside the LocalDNS section",
				name, un[0].Line, m.HostsPath(), un[0].IP)
		}
		list = append(list, hosts.Entry{IP: ip, Hostname: name})
	}

	now := m.opts.Now().UTC().Truncate(time.Second)
	ce := config.Entry{Hostname: name, IP: ip.String(), Port: port, CreatedAt: now}
	if old := st.cfg.Find(name); old != nil {
		ce = *old
	}
	ce.Paused, ce.UpdatedAt = paused, now
	st.cfg.Put(ce)
	if err := st.doc.SetEntries(list); err != nil {
		return Result{}, newError(CodeInternal, "", err, "%v", err)
	}
	if err := m.commit(st); err != nil {
		return Result{}, err
	}
	return Result{Action: action, Entry: makeEntry(name, ip, port, status)}, nil
}

// Remove deletes the managed entry for hostname. Entries outside the
// LocalDNS section are never touched.
func (m *Manager) Remove(hostname string) (Result, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	name, err := validate.Hostname(hostname)
	if err != nil {
		return Result{}, invalidInput(err)
	}
	st, err := m.load()
	if err != nil {
		return Result{}, err
	}
	entry, err := m.find(st, name)
	if err != nil {
		return Result{}, err
	}

	if _, ok := st.doc.Entry(name); ok {
		var keep []hosts.Entry
		for _, e := range st.doc.Entries() {
			if e.Hostname != name {
				keep = append(keep, e)
			}
		}
		if err := st.doc.SetEntries(keep); err != nil {
			return Result{}, newError(CodeInternal, "", err, "%v", err)
		}
	}
	st.cfg.Delete(name)
	if err := m.commit(st); err != nil {
		return Result{}, err
	}
	return Result{Action: ActionRemoved, Entry: entry}, nil
}

// commit writes the hosts file (if its content changed) and the config. It
// checks permissions for both before writing either, and rolls the hosts file
// back if the config cannot be saved, so a failure never leaves them
// half-updated.
func (m *Manager) commit(st *state) error {
	next := st.doc.Bytes()
	hostsChanged := !bytes.Equal(next, st.raw)
	if err := m.checkWritable(hostsChanged, true); err != nil {
		return err
	}
	if hostsChanged {
		if err := m.opts.Store.SaveBackup(st.raw); err != nil {
			return m.writeError(m.ConfigDir(), err)
		}
		if err := m.opts.Hosts.Replace(st.raw, next); err != nil {
			return m.writeError(m.HostsPath(), err)
		}
	}
	if err := m.opts.Store.Save(st.cfg); err != nil {
		if hostsChanged {
			_ = m.opts.Hosts.Replace(next, st.raw)
		}
		return m.writeError(m.ConfigPath(), err)
	}
	return nil
}

func (m *Manager) writeError(path string, err error) error {
	if fsutil.IsPermission(err) {
		return m.permissionError(path, err)
	}
	if errors.Is(err, hosts.ErrChanged) {
		return newError(CodeInternal, "Run the command again.", err, "%v", err)
	}
	return newError(CodeInternal, "", err, "cannot write %s: %v", path, err)
}

func (m *Manager) permissionError(path string, err error) error {
	return newError(CodePermission, m.opts.PermissionHint, err,
		"LocalDNS needs administrator rights to modify %s", path)
}

// checkWritable verifies the files a mutation will touch can be written.
func (m *Manager) checkWritable(hostsFile, configDir bool) error {
	if hostsFile {
		if err := m.opts.Hosts.CheckWritable(); err != nil {
			return m.writeError(m.HostsPath(), err)
		}
	}
	if configDir {
		if err := m.opts.Store.CheckWritable(); err != nil {
			return m.writeError(m.ConfigDir(), err)
		}
	}
	return nil
}

// CheckWritable reports whether LocalDNS can currently modify the hosts file
// and its config. A nil error means add and remove will not need elevation.
func (m *Manager) CheckWritable() error {
	return m.checkWritable(true, true)
}

// PurgePlan describes what Purge would remove.
type PurgePlan struct {
	Entries       int  `json:"entries"`
	SectionExists bool `json:"section_exists"`
	ConfigExists  bool `json:"config_exists"`
}

// PlanPurge inspects what Purge would do and checks it can do it.
func (m *Manager) PlanPurge() (PurgePlan, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	plan := PurgePlan{ConfigExists: m.opts.Store.Exists()}
	raw, err := m.readHosts()
	if err != nil && ErrorCode(err) != CodeHostsNotFound {
		return plan, err
	}
	if raw != nil {
		doc := hosts.Parse(raw)
		if serr := doc.Err(); serr != nil && serr.Markers {
			return plan, sectionError(m.HostsPath(), serr)
		}
		plan.SectionExists = doc.HasSection()
		plan.Entries = len(doc.Entries())
	}
	if err := m.checkWritable(plan.SectionExists, plan.ConfigExists); err != nil {
		return plan, err
	}
	return plan, nil
}

// PurgeResult reports what Purge removed.
type PurgeResult struct {
	EntriesRemoved int  `json:"entries_removed"`
	ConfigRemoved  bool `json:"config_removed"`
}

// Purge removes the LocalDNS section from the hosts file and deletes the
// LocalDNS config. Every line outside the section is left untouched.
func (m *Manager) Purge() (PurgeResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var res PurgeResult

	raw, err := m.readHosts()
	if err != nil && ErrorCode(err) != CodeHostsNotFound {
		return res, err
	}
	if raw != nil {
		doc := hosts.Parse(raw)
		if doc.HasSection() || doc.Err() != nil {
			res.EntriesRemoved = len(doc.Entries())
			if err := doc.RemoveSection(); err != nil {
				var serr *hosts.SectionError
				errors.As(err, &serr)
				return res, sectionError(m.HostsPath(), serr)
			}
			if err := m.checkWritable(true, false); err != nil {
				return res, err
			}
			if err := m.opts.Hosts.Replace(raw, doc.Bytes()); err != nil {
				return res, m.writeError(m.HostsPath(), err)
			}
		}
	}
	removed, err := m.opts.Store.Remove()
	if err != nil {
		return res, m.writeError(m.ConfigDir(), err)
	}
	res.ConfigRemoved = removed
	return res, nil
}
