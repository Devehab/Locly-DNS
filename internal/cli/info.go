package cli

import (
	"fmt"
	"strings"

	"github.com/devehab/locly-dns/internal/validate"
	"github.com/devehab/locly-dns/internal/version"
)

var infoExamples = []string{
	"localdns add app.local 127.0.0.1:3000",
	"localdns add ha.local 192.168.1.60",
	"localdns list",
	"localdns list --json",
	"localdns remove app.local",
	"localdns ui",
}

var exitCodeDocs = []struct {
	Code int    `json:"code"`
	Name string `json:"name"`
	Desc string `json:"description"`
}{
	{ExitOK, "ok", "success"},
	{ExitError, "error", "unexpected error, or doctor found a failing check"},
	{ExitUsage, "usage", "invalid usage or input (bad hostname, address or flag)"},
	{ExitNotFound, "not_found", "hostname is not managed by LocalDNS"},
	{ExitConflict, "conflict", "hostname already exists, or is defined outside LocalDNS"},
	{ExitPermission, "permission_denied", "administrator rights are required"},
	{ExitInvalidState, "invalid_state", "hosts file section or config is invalid (run localdns doctor)"},
	{ExitNotConfirmed, "not_confirmed", "confirmation declined, or --yes missing without a terminal"},
}

func runInfo(c *runCtx) int {
	if c.o.json {
		type cmdInfo struct {
			Name     string   `json:"name"`
			Summary  string   `json:"summary"`
			Usage    string   `json:"usage"`
			Flags    []string `json:"flags"`
			Examples []string `json:"examples"`
		}
		var cmds []cmdInfo
		for _, cmd := range commands {
			ci := cmdInfo{Name: cmd.name, Summary: cmd.summary, Usage: cmd.usage, Examples: cmd.examples}
			for _, f := range cmd.flags {
				ci.Flags = append(ci.Flags, flagDocs[f].name)
			}
			for _, f := range commonFlagDocs {
				ci.Flags = append(ci.Flags, f.name)
			}
			cmds = append(cmds, ci)
		}
		writeJSON(c.env.Stdout, map[string]any{
			"name":             "localdns",
			"description":      "Local hostname manager for developers.",
			"version":          version.Version,
			"commands":         cmds,
			"examples":         infoExamples,
			"exit_codes":       exitCodeDocs,
			"allowed_suffixes": validate.LocalSuffixes,
			"hosts_file":       c.hostsPath,
			"config_file":      c.manager().ConfigPath(),
			"json_errors":      `{"error": {"code": "...", "message": "...", "hint": "..."}}`,
		})
		return ExitOK
	}

	p := c.out
	var b strings.Builder
	w := func(format string, a ...any) { fmt.Fprintf(&b, format, a...) }
	w("%s\n\n", p.bold("LocalDNS"))
	w("Local hostname manager for developers.\n\n")
	w("%s\n  %s\n\n", p.bold("VERSION"), version.Version)
	w("%s\n", p.bold("COMMANDS"))
	for _, cmd := range commands {
		if cmd.listed {
			w("  %-12s %s\n", cmd.name, cmd.summary)
		}
	}
	w("\n%s\n\n", p.bold("EXAMPLES"))
	for _, e := range infoExamples {
		w("  %s\n", e)
	}
	w("\n%s\n", p.bold("FLAGS"))
	w("  %-12s %s\n", "--json", "Machine-readable JSON output (every command, errors included)")
	w("  %-12s %s\n", "--yes, -y", "Skip confirmation (remove, uninstall)")
	w("  %-12s %s\n", "--help, -h", "Help for a command, e.g. localdns add --help")
	w("\n%s\n", p.bold("AUTOMATION / AI AGENTS"))
	w("  Start with `localdns info --json` or `localdns list --json`.\n")
	w("  Commands never wait for input without a terminal: confirmations fail\n")
	w("  with exit code %d unless --yes is given. `add` is idempotent.\n", ExitNotConfirmed)
	w("\n%s\n", p.bold("EXIT CODES"))
	for _, e := range exitCodeDocs {
		w("  %d  %s\n", e.Code, e.Desc)
	}
	w("\n%s\n", p.bold("HOSTNAMES"))
	w("  Local suffixes only: %s\n", validate.SuffixList())
	w("  Addresses: loopback or private-network IPs, with an optional port.\n")
	w("\n%s\n", p.bold("FILES"))
	w("  %-12s %s\n", "Hosts file", c.hostsPath)
	w("  %-12s %s\n", "Config", c.manager().ConfigPath())
	p.printf("%s", b.String())
	return ExitOK
}
