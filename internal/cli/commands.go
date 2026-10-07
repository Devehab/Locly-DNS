package cli

import (
	"fmt"
	"net"
	"net/netip"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/devehab/locly-dns/internal/core"
	"github.com/devehab/locly-dns/internal/fsutil"
	"github.com/devehab/locly-dns/internal/ui"
	"github.com/devehab/locly-dns/internal/validate"
	"github.com/devehab/locly-dns/internal/version"
)

// Command-specific flags.
const (
	flagYes = iota
	flagForce
	flagNoElevate
	flagPort
	flagOpen
	flagNoOpen
	flagRouterPort
	flagKeepBinary
	flagName
)

type flagDoc struct {
	name string
	desc string
}

var flagDocs = map[int]flagDoc{
	flagYes:        {"-y, --yes", "Do not ask for confirmation (for scripts and AI agents)"},
	flagForce:      {"--force", "Replace an existing entry that points somewhere else"},
	flagNoElevate:  {"--no-elevate", "Never request administrator rights automatically (sudo)"},
	flagPort:       {"--port <port>", "Port for the web UI (default 7357)"},
	flagOpen:       {"--open", "Open the browser even without a terminal (scripts)"},
	flagNoOpen:     {"--no-open", "Don't open your browser automatically"},
	flagRouterPort: {"--port <port>", "Port for `router run` (default 80, which is what makes URLs port-free)"},
	flagKeepBinary: {"--keep-binary", "Remove entries and configuration but keep the localdns binary"},
	flagName:       {"--name <hostname>", "Rename the entry, e.g. --name web.local"},
}

var commonFlagDocs = []flagDoc{
	{"--json", "Print machine-readable JSON (errors too)"},
	{"--hosts-file <path>", "Use a different hosts file (default: the system hosts file; env LOCALDNS_HOSTS_FILE)"},
	{"--config-dir <path>", "Use a different config directory (env LOCALDNS_CONFIG_DIR)"},
	{"--no-color", "Disable colors (also honors NO_COLOR)"},
	{"-h, --help", "Show help for the command"},
}

type command struct {
	name     string
	summary  string
	usage    string
	long     string
	args     [][2]string
	flags    []int
	examples []string
	minArgs  int
	maxArgs  int
	listed   bool // shown in `localdns info`
	run      func(*runCtx) int
}

var commands []*command

func init() {
	commands = []*command{
		{
			name: "add", summary: "Add a hostname", listed: true,
			usage: "localdns add <hostname> <ip[:port]>",
			long: "Map a local hostname to an IP address on this machine or your local network.\n" +
				"The port is optional; LocalDNS remembers it to show the full URL (the hosts\n" +
				"file itself cannot store ports). Adding the same entry twice is a no-op.",
			args: [][2]string{
				{"hostname", "Local hostname, e.g. app.local (allowed suffixes: " + validate.SuffixList() + ")"},
				{"ip[:port]", "Local IP with optional port, e.g. 127.0.0.1:3000, 192.168.1.60, [::1]:8080"},
			},
			flags: []int{flagForce, flagNoElevate},
			examples: []string{
				"localdns add app.local 127.0.0.1:3000",
				"localdns add api.local 127.0.0.1:3002",
				"localdns add ha.local 192.168.1.60",
				"localdns add app.local 127.0.0.1:4000 --force",
				"localdns add app.local 127.0.0.1:3000 --json",
			},
			minArgs: 2, maxArgs: 2, run: runAdd,
		},
		{
			name: "edit", summary: "Change a hostname or its address", listed: true,
			usage: "localdns edit <hostname> [<ip[:port]>] [--name <new-hostname>]",
			long: "Change where a hostname points, rename it, or both, in one step. The entry\n" +
				"keeps its place; nothing else in the hosts file changes. A pasted link such\n" +
				"as http://127.0.0.1:4000/ is accepted as the address.",
			args: [][2]string{
				{"hostname", "The hostname to change, e.g. app.local"},
				{"ip[:port]", "Optional new address, e.g. 127.0.0.1:4000"},
			},
			flags: []int{flagName, flagNoElevate},
			examples: []string{
				"localdns edit app.local 127.0.0.1:4000",
				"localdns edit app.local --name web.local",
				"localdns edit app.local 127.0.0.1:5173 --name web.local",
				"localdns edit app.local 127.0.0.1:4000 --json",
			},
			minArgs: 1, maxArgs: 2, run: runEdit,
		},
		{
			name: "list", summary: "List hostnames", listed: true,
			usage: "localdns list",
			long:  "List the hostnames managed by LocalDNS with their address and status.",
			examples: []string{
				"localdns list",
				"localdns list --json",
			},
			run: runList,
		},
		{
			name: "remove", summary: "Remove a hostname", listed: true,
			usage: "localdns remove <hostname>",
			long: "Remove a hostname managed by LocalDNS. Asks for confirmation unless --yes is\n" +
				"given. Entries LocalDNS did not create are never touched.",
			args:  [][2]string{{"hostname", "The hostname to remove, e.g. app.local"}},
			flags: []int{flagYes, flagNoElevate},
			examples: []string{
				"localdns remove app.local",
				"localdns remove app.local --yes",
				"localdns remove app.local --yes --json",
			},
			minArgs: 1, maxArgs: 1, run: runRemove,
		},
		{
			name: "pause", summary: "Switch a hostname off, keep it", listed: true,
			usage: "localdns pause <hostname>",
			long: "Switch a hostname off without removing it: its line leaves the hosts file,\n" +
				"so the name stops working, but LocalDNS keeps it (address and port) and\n" +
				"`localdns resume` turns it back on.",
			args:  [][2]string{{"hostname", "The hostname to switch off, e.g. app.local"}},
			flags: []int{flagNoElevate},
			examples: []string{
				"localdns pause app.local",
				"localdns pause app.local --json",
			},
			minArgs: 1, maxArgs: 1, run: runPause,
		},
		{
			name: "resume", summary: "Switch a paused hostname back on", listed: true,
			usage: "localdns resume <hostname>",
			long: "Switch a paused hostname back on: its line returns to the hosts file. Also\n" +
				"restores an entry whose line was deleted from the hosts file by hand.",
			args:  [][2]string{{"hostname", "The hostname to switch on, e.g. app.local"}},
			flags: []int{flagNoElevate},
			examples: []string{
				"localdns resume app.local",
				"localdns resume app.local --json",
			},
			minArgs: 1, maxArgs: 1, run: runResume,
		},
		{
			name: "status", summary: "Check entries", listed: true,
			usage: "localdns status",
			long: "Show where LocalDNS stores its data, whether it can write the hosts file, and\n" +
				"whether each entry is active, missing from the hosts file, or overridden.",
			examples: []string{"localdns status", "localdns status --json"},
			run:      runStatus,
		},
		{
			name: "doctor", summary: "Diagnose problems", listed: true,
			usage: "localdns doctor",
			long: "Check that LocalDNS can work on this machine and explain how to fix anything\n" +
				"that is wrong. Exits with code 1 if a check fails.",
			flags:    []int{flagPort},
			examples: []string{"localdns doctor", "localdns doctor --json"},
			run:      runDoctor,
		},
		{
			name: "ui", summary: "Open the web interface", listed: true,
			usage: "localdns ui",
			long: "Start the LocalDNS web interface on http://127.0.0.1:7357 and open it in your\n" +
				"browser. It listens on localhost only and stops when you press Ctrl+C.\n" +
				"On macOS and Linux it asks for your password (sudo) so the UI can add and\n" +
				"delete hosts; without a terminal it starts read-only.",
			flags:    []int{flagPort, flagNoOpen, flagOpen, flagNoElevate},
			examples: []string{"localdns ui", "localdns ui --port 8080", "localdns ui --no-open", "localdns ui --no-elevate"},
			run:      runUI,
		},
		{
			name: "router", summary: "Port-free URLs (http://app.local)", listed: true,
			usage: "localdns router [status|enable|disable|run]",
			long: "The hosts file maps names to addresses but cannot store ports, so\n" +
				"http://app.local would go to port 80. The LocalDNS router listens on\n" +
				"127.0.0.1:80 (this machine only) and forwards each of your names to its\n" +
				"port, so http://app.local opens 127.0.0.1:3000. It serves only names you\n" +
				"added; other traffic, DNS and proxy settings are never touched.\n\n" +
				"enable installs it as a background service that starts on its own and\n" +
				"never runs as root (macOS: launchd opens the port and runs the router as\n" +
				"\"nobody\"; Linux: a systemd service as you; Windows: a login item). It asks\n" +
				"for your password once on macOS and Linux. disable removes it, run keeps it\n" +
				"in the foreground instead.",
			args:     [][2]string{{"action", "status (default), enable, disable or run"}},
			flags:    []int{flagRouterPort, flagNoElevate},
			examples: []string{"localdns router enable", "localdns router", "localdns router disable", "localdns router run"},
			maxArgs:  1, run: runRouter,
		},
		{
			name: "uninstall", summary: "Remove LocalDNS", listed: true,
			usage: "localdns uninstall",
			long: "Remove LocalDNS-managed host entries, the LocalDNS configuration and the\n" +
				"localdns binary. Other hosts entries are not modified.",
			flags:    []int{flagYes, flagKeepBinary, flagNoElevate},
			examples: []string{"localdns uninstall", "localdns uninstall --yes"},
			run:      runUninstall,
		},
		{
			name: "info", summary: "Show commands, usage and examples", listed: true,
			usage: "localdns info",
			long: "Print an overview of LocalDNS: commands, examples, flags, exit codes and file\n" +
				"locations. Start here (or with `localdns info --json`) to learn the CLI.",
			examples: []string{"localdns info", "localdns info --json"},
			run:      runInfo,
		},
		{
			name: "version", summary: "Print the version",
			usage:    "localdns version",
			examples: []string{"localdns version", "localdns --version"},
			run:      runVersion,
		},
	}
}

func lookup(name string) *command {
	for _, c := range commands {
		if c.name == name {
			return c
		}
	}
	return nil
}

func (cmd *command) helpText() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s\n\nUSAGE\n  %s [flags]\n", cmd.long, cmd.usage)
	if cmd.long == "" {
		b.Reset()
		fmt.Fprintf(&b, "%s\n\nUSAGE\n  %s [flags]\n", cmd.summary, cmd.usage)
	}
	if len(cmd.args) > 0 {
		b.WriteString("\nARGUMENTS\n")
		for _, a := range cmd.args {
			fmt.Fprintf(&b, "  %-12s %s\n", a[0], a[1])
		}
	}
	b.WriteString("\nFLAGS\n")
	var docs []flagDoc
	for _, f := range cmd.flags {
		docs = append(docs, flagDocs[f])
	}
	docs = append(docs, commonFlagDocs...)
	for _, d := range docs {
		fmt.Fprintf(&b, "  %-20s %s\n", d.name, d.desc)
	}
	if len(cmd.examples) > 0 {
		b.WriteString("\nEXAMPLES\n")
		for _, e := range cmd.examples {
			fmt.Fprintf(&b, "  %s\n", e)
		}
	}
	return b.String()
}

// ---- add ----

func runAdd(c *runCtx) int {
	res, err := c.manager().Add(c.args[0], c.args[1], core.AddOptions{Force: c.o.force})
	if err != nil {
		if code, ok := c.tryElevate(err); ok {
			return code
		}
		return c.fail(err)
	}
	res.Entry = c.withShortURLs([]core.Entry{res.Entry})[0]
	if c.o.json {
		writeJSON(c.env.Stdout, res)
		return ExitOK
	}
	switch res.Action {
	case core.ActionAdded:
		c.out.println(c.out.ok() + " Added successfully")
	case core.ActionUpdated:
		c.out.println(c.out.ok() + " Updated successfully")
	default:
		c.out.println(c.out.ok() + " Already added (nothing changed)")
	}
	c.printEntry(res.Entry)
	if res.Action != core.ActionUnchanged {
		c.secureContextTip(res.Entry)
	}
	return ExitOK
}

// secureContextTip suggests a .localhost name for a web app on this
// computer. Browsers enable features such as crypto.subtle, the clipboard
// and some sign-in flows only on https:// or localhost pages, and they treat
// *.localhost like localhost; http://app.local is neither, so parts of an
// app can fail there while http://localhost:3000 worked.
func (c *runCtx) secureContextTip(e core.Entry) {
	ip, err := netip.ParseAddr(e.IP)
	if err != nil || !ip.IsLoopback() || e.Port == nil || strings.HasSuffix(e.Hostname, ".localhost") {
		return
	}
	base, _ := validate.SplitSuffix(e.Hostname)
	p := c.out
	p.println()
	p.println(p.dim("Tip: if parts of the app fail at http://" + e.Hostname + " (sign-in, uploads), use " + base + ".localhost;"))
	p.println(p.dim("browsers treat .localhost as secure, like localhost: localdns edit " + e.Hostname + " --name " + base + ".localhost"))
}

func (c *runCtx) printEntry(e core.Entry) {
	p := c.out
	p.println()
	p.println(p.bold(e.Hostname) + " → " + e.Address)
	if e.Port == nil {
		return
	}
	p.println()
	p.println("URL:")
	if e.ShortURL != "" {
		p.println(e.ShortURL + p.dim("   (also "+e.URL+")"))
		return
	}
	p.println(e.URL)
	if st := c.env.Router.State(); e.Routable() && !st.Installed && st.Kind != "off" {
		p.println()
		p.println(p.dim("Tip: to open " + "http://" + e.Hostname + " without the port, run: localdns router enable"))
	}
}

// ---- edit ----

func runEdit(c *runCtx) int {
	address := ""
	if len(c.args) == 2 {
		address = c.args[1]
	}
	res, err := c.manager().Edit(c.args[0], c.o.newName, address)
	if err != nil {
		if code, ok := c.tryElevate(err); ok {
			return code
		}
		return c.fail(err)
	}
	res.Entry = c.withShortURLs([]core.Entry{res.Entry})[0]
	if c.o.json {
		writeJSON(c.env.Stdout, res)
		return ExitOK
	}
	if res.Action == core.ActionUnchanged {
		c.out.println(c.out.ok() + " Nothing to change")
	} else {
		c.out.println(c.out.ok() + " Updated successfully")
	}
	c.printEntry(res.Entry)
	return ExitOK
}

// ---- pause / resume ----

func runPause(c *runCtx) int {
	res, err := c.manager().Pause(c.args[0])
	if err != nil {
		if code, ok := c.tryElevate(err); ok {
			return code
		}
		return c.fail(err)
	}
	if c.o.json {
		writeJSON(c.env.Stdout, res)
		return ExitOK
	}
	p := c.out
	if res.Action == core.ActionUnchanged {
		p.println(p.ok() + " " + res.Entry.Hostname + " is already paused")
	} else {
		p.println(p.ok() + " Paused " + res.Entry.Hostname)
	}
	p.println()
	p.println(res.Entry.Hostname + " → " + res.Entry.Address + p.dim(" (kept, but switched off)"))
	p.println()
	p.println("Switch it back on with: localdns resume " + res.Entry.Hostname)
	return ExitOK
}

func runResume(c *runCtx) int {
	res, err := c.manager().Resume(c.args[0])
	if err != nil {
		if code, ok := c.tryElevate(err); ok {
			return code
		}
		return c.fail(err)
	}
	res.Entry = c.withShortURLs([]core.Entry{res.Entry})[0]
	if c.o.json {
		writeJSON(c.env.Stdout, res)
		return ExitOK
	}
	if res.Action == core.ActionUnchanged {
		c.out.println(c.out.ok() + " " + res.Entry.Hostname + " is already on")
	} else {
		c.out.println(c.out.ok() + " Resumed " + res.Entry.Hostname)
	}
	c.printEntry(res.Entry)
	return ExitOK
}

// ---- list ----

func runList(c *runCtx) int {
	list, err := c.manager().List()
	if err != nil {
		return c.fail(err)
	}
	list = c.withShortURLs(list)
	if c.o.json {
		writeJSON(c.env.Stdout, map[string]any{"entries": list})
		return ExitOK
	}
	c.out.println(c.out.bold("LocalDNS"))
	c.out.println()
	if len(list) == 0 {
		c.out.println("No entries yet. Add one with:")
		c.out.println()
		c.out.println("  localdns add app.local 127.0.0.1:3000")
		return ExitOK
	}
	rows := [][]string{{"HOSTNAME", "ADDRESS", "STATUS", "OPEN"}}
	for _, e := range list {
		rows = append(rows, []string{e.Hostname, e.Address, statusSymbol(c.out, e.Status), openURL(e)})
	}
	c.out.table(rows, "")
	c.out.println()
	c.out.println(plural(len(list), "entry", "entries"))
	for _, e := range list {
		if e.Status != core.StatusActive && e.Status != core.StatusPaused {
			c.out.println()
			c.out.println("Run `localdns status` for details on entries that need attention.")
			break
		}
	}
	return ExitOK
}

// openURL is the address to open in a browser: port-free when the router
// serves the entry.
func openURL(e core.Entry) string {
	if e.Status == core.StatusPaused {
		return ""
	}
	if e.ShortURL != "" {
		return e.ShortURL
	}
	if e.Port != nil {
		return e.URL
	}
	return ""
}

func statusSymbol(p *printer, s core.Status) string {
	switch s {
	case core.StatusActive:
		return p.ok()
	case core.StatusMissing:
		return p.fail() + " missing"
	case core.StatusConflict:
		return p.warn() + " conflict"
	case core.StatusPaused:
		return p.dim("○ paused")
	}
	return string(s)
}

// ---- remove ----

func runRemove(c *runCtx) int {
	m := c.manager()
	entry, err := m.Get(c.args[0])
	if err != nil {
		return c.fail(err)
	}
	question := fmt.Sprintf("Remove %s → %s?", entry.Hostname, entry.Address)
	if err := c.confirm(question, "localdns remove "+entry.Hostname+" --yes"); err != nil {
		return c.fail(err)
	}
	res, err := m.Remove(entry.Hostname)
	if err != nil {
		if code, ok := c.tryElevate(err, "--yes"); ok {
			return code
		}
		return c.fail(err)
	}
	if c.o.json {
		writeJSON(c.env.Stdout, res)
		return ExitOK
	}
	c.out.println(c.out.ok() + " Removed successfully")
	c.out.println()
	c.out.println(res.Entry.Hostname + " → " + res.Entry.Address)
	return ExitOK
}

// ---- status ----

func runStatus(c *runCtx) int {
	r, err := c.manager().Status()
	if err != nil {
		return c.fail(err)
	}
	r.Entries = c.withShortURLs(r.Entries)
	if c.o.json {
		writeJSON(c.env.Stdout, r)
		return ExitOK
	}
	p := c.out
	p.println(p.bold("LocalDNS Status"))
	p.println()
	section := r.Section
	switch r.Section {
	case core.SectionPresent:
		section = "present (" + plural(len(r.Entries)-r.Counts.Missing-r.Counts.Paused, "entry", "entries") + ")"
	case core.SectionAbsent:
		section = "not created yet"
	case core.SectionInvalid:
		section = p.red("invalid") + " — " + r.SectionError
	}
	writable := p.green("yes")
	if !r.Writable {
		writable = "no — changes need administrator rights"
	}
	p.printf("  %-12s %s\n", "Hosts file", r.HostsFile)
	p.printf("  %-12s %s\n", "Config", r.ConfigFile)
	p.printf("  %-12s %s\n", "Section", section)
	p.printf("  %-12s %s\n", "Writable", writable)
	if r.ConfigError != "" {
		p.printf("  %-12s %s\n", "Config error", p.red(r.ConfigError))
	}
	p.printf("  %-12s %s\n", "Port-free", c.routerSummary())
	p.println()
	if len(r.Entries) > 0 {
		rows := [][]string{{"HOSTNAME", "ADDRESS", "STATUS"}}
		for _, e := range r.Entries {
			label := map[core.Status]string{
				core.StatusActive:   p.ok() + " active",
				core.StatusMissing:  p.fail() + " missing",
				core.StatusConflict: p.warn() + " conflict",
				core.StatusPaused:   p.dim("○ paused"),
			}[e.Status]
			rows = append(rows, []string{e.Hostname, e.Address, label})
		}
		p.table(rows, "  ")
		p.println()
		for _, e := range r.Entries {
			if e.Detail != "" {
				p.printf("  %s: %s\n", e.Hostname, e.Detail)
			}
		}
	}
	switch {
	case r.Healthy && len(r.Entries) == 0:
		p.println(p.ok() + " No entries yet")
	case r.Healthy:
		p.println(p.ok() + " All entries active")
	default:
		p.println(p.warn() + " Some things need attention. Run `localdns doctor` for fixes.")
	}
	return ExitOK
}

// ---- doctor ----

func runDoctor(c *runCtx) int {
	canElevate := c.env.Getenv(EnvNoElevate) == "" && c.env.Elevator.Available(c.env.Interactive)
	m := c.newManager(canElevate)
	report := m.Doctor(func() core.Check { return ui.AvailabilityCheck(c.o.port) }, c.routerCheck)
	if c.o.json {
		writeJSON(c.env.Stdout, report)
	} else {
		p := c.out
		p.println(p.bold("LocalDNS Doctor"))
		p.println()
		problems := 0
		for _, ch := range report.Checks {
			sym := p.ok()
			switch ch.Status {
			case core.CheckWarn:
				sym = p.warn()
				problems++
			case core.CheckFail:
				sym = p.fail()
				problems++
			}
			if ch.Status == core.CheckPass && ch.Message != "" {
				p.println(sym + " " + ch.Name + p.dim(" — "+ch.Message))
				continue
			}
			p.println(sym + " " + ch.Name)
			if ch.Status != core.CheckPass {
				if ch.Message != "" {
					p.println("    " + ch.Message)
				}
				for _, line := range strings.Split(ch.Fix, "\n") {
					if line != "" {
						p.println("    " + p.dim("Fix: ") + line)
					}
				}
			}
		}
		p.println()
		switch {
		case problems == 0:
			p.println("Everything looks good.")
		case report.OK:
			p.println(plural(problems, "warning", "warnings") + ", no failures. LocalDNS can run.")
		default:
			p.println(plural(problems, "problem", "problems") + " found. Follow the fixes above.")
		}
	}
	if !report.OK {
		return ExitError
	}
	return ExitOK
}

// ---- ui ----

func runUI(c *runCtx) int {
	if c.o.port < 0 || c.o.port > 65535 {
		return c.fail(&core.Error{Code: codeUsage, Message: "invalid port " + strconv.Itoa(c.o.port),
			Hint: "Use a port between 1 and 65535, e.g. --port 7357"})
	}
	m := c.manager()
	writable := m.CheckWritable() == nil
	openBrowser := c.shouldOpenBrowser()

	// Interactive and read-only: ask for administrator rights so the UI can
	// add and delete hosts. Only the UI server runs elevated; this
	// (unprivileged) process opens the browser once the server answers, so
	// the browser never runs as root.
	if !writable && c.env.Interactive && !c.o.json {
		stop := make(chan struct{})
		if openBrowser && c.o.port != 0 {
			go c.openWhenReady(c.o.port, stop)
		}
		perm := &core.Error{Code: core.CodePermission, Message: "the web UI needs administrator rights to edit " + c.hostsPath}
		c.elevateNote = "To add and delete hosts, the web UI must edit " + c.hostsPath + ", which needs\n" +
			"administrator rights. Enter your password to continue: only this UI process\n" +
			"runs with sudo, and it stops completely when you press Ctrl+C.\n" +
			"(Prefer read-only? Run: localdns ui --no-elevate)\n\n"
		code, ok := c.tryElevate(perm, "--no-open")
		close(stop)
		if ok {
			return code
		}
	}

	ln, err := ui.Listen(c.o.port)
	if err != nil {
		hint := fmt.Sprintf("Is the LocalDNS UI already running? Open %s, or start it on another port: localdns ui --port %d",
			ui.URL(c.o.port), c.o.port+1)
		return c.fail(&core.Error{Code: codePortInUse, Message: fmt.Sprintf("cannot listen on %s: %v", ui.URL(c.o.port), err), Hint: hint})
	}
	portNum := ln.Addr().(*net.TCPAddr).Port
	direct := ui.URL(portNum)
	url := c.dashboardURL(portNum)

	readOnlyHint := ""
	if !writable {
		readOnlyHint = "Editing " + c.hostsPath + " needs administrator rights. Stop the UI (Ctrl+C) and run: sudo localdns ui"
		if runtime.GOOS == "windows" {
			readOnlyHint = "Stop the UI and run `localdns ui` from a terminal opened with \"Run as administrator\"."
		}
	}
	handler, err := ui.NewHandler(m, ui.Options{Port: portNum, Version: version.Version, ReadOnlyHint: readOnlyHint,
		ShortURL: c.shortURLFunc(), Router: c.uiRouter()})
	if err != nil {
		_ = ln.Close()
		return c.fail(err)
	}

	if c.o.json {
		writeJSON(c.env.Stdout, map[string]any{"url": url, "direct_url": direct, "writable": writable, "hosts_file": c.hostsPath})
	} else {
		p := c.out
		p.println(p.ok() + " LocalDNS UI running")
		p.println()
		p.println("Open:")
		p.println(url)
		p.println()
		if url != direct {
			p.println(p.dim("Also at " + direct + " (works even when port-free URLs are off)"))
			p.println()
		}
		if openBrowser {
			p.println(p.dim("Opening it in your browser…"))
			p.println()
		}
		if !writable {
			p.println(p.warn() + " Read-only: " + readOnlyHint)
			p.println()
		}
		p.println(p.dim("Listening on localhost only. Press Ctrl+C to stop."))
	}
	if openBrowser && c.env.OpenBrowser != nil {
		if err := c.env.OpenBrowser(url); err != nil {
			c.errp.println(c.errp.warn() + " Could not open a browser: " + err.Error())
		}
	}
	if err := ui.Serve(c.env.Context, ln, handler); err != nil {
		return c.fail(err)
	}
	if !c.o.json {
		c.out.println()
		c.out.println("Stopped.")
	}
	return ExitOK
}

// shouldOpenBrowser: open automatically for a person at a terminal, never in
// JSON mode or when asked not to; --open forces it.
func (c *runCtx) shouldOpenBrowser() bool {
	if c.o.noOpen {
		return false
	}
	return c.o.open || (c.env.Interactive && !c.o.json)
}

// openWhenReady opens the UI in the browser as soon as a LocalDNS UI answers
// on port (the elevated server may wait for a password first).
func (c *runCtx) openWhenReady(port int, stop <-chan struct{}) {
	deadline := time.Now().Add(3 * time.Minute)
	for time.Now().Before(deadline) {
		select {
		case <-stop:
			return
		case <-time.After(300 * time.Millisecond):
		}
		if ui.Probe(port) {
			if c.env.OpenBrowser != nil {
				_ = c.env.OpenBrowser(c.dashboardURL(port))
			}
			return
		}
	}
}

// ---- uninstall ----

func runUninstall(c *runCtx) int {
	m := c.manager()
	_, planErr := m.PlanPurge()
	if planErr != nil && core.ErrorCode(planErr) != core.CodePermission {
		return c.fail(planErr)
	}
	binary := ""
	if !c.o.keepBinary {
		binary = c.env.Executable
	}

	routerInstalled := c.env.Router.State().Installed
	question := "Uninstall LocalDNS?\n\nThis will:\n\n" +
		"• Remove LocalDNS-managed host entries\n" +
		"• Remove LocalDNS configuration\n"
	if routerInstalled {
		question += "• Remove the LocalDNS router (port-free URLs)\n"
	}
	if binary != "" {
		question += "• Remove the LocalDNS application\n"
	}
	question += "\nYour other hosts entries will not be modified.\n\nContinue?"
	if err := c.confirm(question, "localdns uninstall --yes"); err != nil {
		return c.fail(err)
	}

	needElevation := planErr
	routerRemoved := false
	if routerInstalled {
		// Stop the router first: on Windows a running localdns.exe can't be
		// deleted.
		err := c.env.Router.Disable()
		switch {
		case err == nil:
			routerRemoved = true
		case fsutil.IsPermission(err):
			if needElevation == nil {
				needElevation = &core.Error{Code: core.CodePermission,
					Message: "LocalDNS needs administrator rights to remove the router service",
					Hint:    c.permissionHint()}
			}
		default:
			return c.fail(&core.Error{Code: core.CodeInternal, Message: "could not remove the router: " + err.Error()})
		}
	}
	if needElevation == nil && binary != "" {
		if err := fsutil.CheckWritable(filepath.Dir(binary)); fsutil.IsPermission(err) {
			needElevation = &core.Error{Code: core.CodePermission,
				Message: "LocalDNS needs administrator rights to remove " + binary,
				Hint:    c.permissionHint()}
		}
	}
	if needElevation != nil {
		if code, ok := c.tryElevate(needElevation, "--yes"); ok {
			return code
		}
		return c.fail(needElevation)
	}

	res, err := m.Purge()
	if err != nil {
		return c.fail(err)
	}
	binaryRemoved := false
	var binErr error
	if binary != "" && c.env.RemoveBinary != nil {
		binErr = c.env.RemoveBinary(binary)
		binaryRemoved = binErr == nil
	}

	if c.o.json {
		out := map[string]any{
			"entries_removed": res.EntriesRemoved,
			"config_removed":  res.ConfigRemoved,
			"binary_removed":  binaryRemoved,
			"binary_path":     binary,
			"router_removed":  routerRemoved,
		}
		if binErr != nil {
			out["binary_error"] = binErr.Error()
		}
		writeJSON(c.env.Stdout, out)
	} else {
		p := c.out
		p.println(p.ok() + " LocalDNS entries removed" + p.dim(" ("+plural(res.EntriesRemoved, "entry", "entries")+")"))
		p.println(p.ok() + " Configuration removed")
		if routerRemoved {
			p.println(p.ok() + " Router removed")
		}
		switch {
		case binErr != nil:
			p.println(p.fail() + " Could not remove " + binary + ": " + binErr.Error())
			p.println("    Delete it manually to finish uninstalling.")
		case binary == "":
			p.println(p.ok() + " LocalDNS uninstalled (binary kept)")
		default:
			p.println(p.ok() + " LocalDNS uninstalled")
		}
	}
	if binErr != nil {
		return ExitError
	}
	return ExitOK
}

// ---- version ----

func runVersion(c *runCtx) int {
	if c.o.json {
		writeJSON(c.env.Stdout, map[string]any{"version": version.Version, "commit": version.Commit})
		return ExitOK
	}
	c.out.println("localdns " + version.Version)
	return ExitOK
}
