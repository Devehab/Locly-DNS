// Package cli implements the localdns command-line interface.
//
// The CLI is designed for humans and for AI agents alike: every command is
// deterministic, accepts --json for machine-readable output, documents itself
// via `localdns info` and `--help`, and never waits for input when there is
// no terminal (commands that need confirmation take --yes instead).
package cli

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"

	"github.com/devehab/locly-dns/internal/config"
	"github.com/devehab/locly-dns/internal/core"
	"github.com/devehab/locly-dns/internal/elevate"
	"github.com/devehab/locly-dns/internal/hosts"
	"github.com/devehab/locly-dns/internal/platform"
)

// Exit codes. They are part of the CLI contract and listed by `localdns info`.
const (
	ExitOK           = 0 // success
	ExitError        = 1 // unexpected error, or doctor found a failure
	ExitUsage        = 2 // invalid usage or input
	ExitNotFound     = 3 // hostname not managed by LocalDNS
	ExitConflict     = 4 // already exists, or defined outside the LocalDNS section
	ExitPermission   = 5 // administrator rights required
	ExitInvalidState = 6 // hosts file section or config is malformed / hosts file missing
	ExitNotConfirmed = 7 // confirmation declined, or --yes missing without a terminal
)

// CLI-level error codes (core defines the rest).
const (
	codeUsage        core.Code = "usage"
	codeConfirmation core.Code = "confirmation_required"
	codeCanceled     core.Code = "canceled"
	codePortInUse    core.Code = "port_in_use"
)

// EnvNoElevate disables automatic sudo when set to a non-empty value.
const EnvNoElevate = "LOCALDNS_NO_ELEVATE"

// Env is everything the CLI needs from the outside world. Tests construct it
// directly; Main builds it from the real process.
type Env struct {
	Args   []string
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer
	Getenv func(string) string
	// Interactive is true when stdin is a terminal a person can answer.
	Interactive bool
	// Color enables ANSI colors on Stdout.
	Color bool
	// Elevator re-runs a command with administrator rights.
	Elevator elevate.Elevator
	// Executable is the path of the running localdns binary.
	Executable string
	// Context is canceled on Ctrl+C; it stops `localdns ui`.
	Context context.Context
	// RemoveBinary deletes the installed binary during uninstall.
	RemoveBinary func(path string) error
	// OpenBrowser opens a URL in the user's browser (`localdns ui`).
	OpenBrowser func(url string) error
	// Router manages the background port-free router; nil means "off".
	Router RouterControl
	// HostsFile opens the hosts file at path; nil means the file on disk.
	// Tests use it to simulate a read-only hosts file.
	HostsFile func(path string) hosts.File
}

// Main runs the CLI for the current process and returns its exit code.
func Main() int {
	exe, err := os.Executable()
	if err == nil {
		if resolved, rerr := filepath.EvalSymlinks(exe); rerr == nil {
			exe = resolved
		}
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	var rc RouterControl = systemRouter{}
	if os.Getenv(EnvRouterService) == "off" {
		rc = offRouter{}
	}
	color := isTerminal(os.Stdout) && os.Getenv("NO_COLOR") == "" && os.Getenv("TERM") != "dumb" && enableANSI(os.Stdout)
	return Run(Env{
		Args:         os.Args[1:],
		Stdin:        os.Stdin,
		Stdout:       os.Stdout,
		Stderr:       os.Stderr,
		Getenv:       os.Getenv,
		Interactive:  isTerminal(os.Stdin),
		Color:        color,
		Elevator:     elevate.Default(),
		Executable:   exe,
		Context:      ctx,
		RemoveBinary: removeBinary,
		OpenBrowser:  openBrowser,
		Router:       rc,
	})
}

// options holds every flag; each command registers the subset it supports.
type options struct {
	help       bool
	json       bool
	noColor    bool
	yes        bool
	force      bool
	noElevate  bool
	open       bool
	noOpen     bool
	keepBinary bool
	port       int
	routerPort int
	hostsFile  string
	configDir  string
}

type runCtx struct {
	env  Env
	cmd  *command
	o    options
	args []string // positional arguments
	out  *printer
	errp *printer

	hostsPath, configDir          string
	hostsOverride, configOverride bool
	mgr                           *core.Manager
	elevateNote                   string // shown instead of the default sudo notice
	routerChecked, routerUp       bool
}

// Run executes the CLI with env and returns the exit code.
func Run(env Env) int {
	if env.Getenv == nil {
		env.Getenv = func(string) string { return "" }
	}
	if env.Elevator == nil {
		env.Elevator = elevate.None{}
	}
	if env.Context == nil {
		env.Context = context.Background()
	}
	if env.Router == nil {
		env.Router = offRouter{}
	}
	if env.Stdin == nil {
		env.Stdin = strings.NewReader("")
	}

	name, rest := splitCommand(env.Args)
	jsonMode := hasFlag(env.Args, "json")
	switch name {
	case "":
		if hasFlag(env.Args, "version") {
			name = "version"
			rest = removeArgs(rest, "--version", "-version")
		} else {
			// `localdns`, `localdns --help` and `localdns --json` all show
			// the overview.
			name = "info"
			rest = removeArgs(rest, "-h", "--help", "-help")
		}
	case "help":
		if len(rest) > 0 {
			name, rest = rest[0], []string{"--help"}
		} else {
			name = "info"
		}
	}

	cmd := lookup(name)
	if cmd == nil {
		c := &runCtx{env: env, out: &printer{w: env.Stdout}, errp: &printer{w: env.Stderr, color: env.Color}}
		c.o.json = jsonMode
		return c.fail(&core.Error{Code: codeUsage, Message: fmt.Sprintf("unknown command %q", name),
			Hint: "Run `localdns info` to see all commands."})
	}

	c := &runCtx{env: env, cmd: cmd}
	fs := flag.NewFlagSet(cmd.name, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	c.registerFlags(fs)
	args, err := parseInterspersed(fs, rest)
	c.out = &printer{w: env.Stdout, color: env.Color && !c.o.noColor && !c.o.json}
	c.errp = &printer{w: env.Stderr, color: env.Color && !c.o.noColor}
	if err != nil {
		c.o.json = c.o.json || jsonMode
		return c.fail(&core.Error{Code: codeUsage, Message: flagError(err),
			Hint: "Run `localdns " + cmd.name + " --help` for usage."})
	}
	if c.o.help {
		c.out.printf("%s", cmd.helpText())
		return ExitOK
	}
	c.args = args
	if len(args) < cmd.minArgs || len(args) > cmd.maxArgs {
		msg := "wrong number of arguments"
		if len(args) < cmd.minArgs {
			msg = "missing arguments"
		} else if cmd.maxArgs == 0 {
			msg = fmt.Sprintf("%s takes no arguments", cmd.name)
		}
		return c.fail(&core.Error{Code: codeUsage, Message: msg,
			Hint: "Usage: " + cmd.usage + "\nRun `localdns " + cmd.name + " --help` for details."})
	}
	c.resolvePaths()
	return cmd.run(c)
}

// splitCommand finds the command name: the first argument that is not a flag
// (or the value of a flag that takes one).
func splitCommand(args []string) (string, []string) {
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			break
		}
		if strings.HasPrefix(a, "-") {
			if flagTakesValue(a) && !strings.Contains(a, "=") {
				i++
			}
			continue
		}
		rest := append(append([]string{}, args[:i]...), args[i+1:]...)
		return a, rest
	}
	return "", args
}

func flagTakesValue(arg string) bool {
	switch strings.TrimLeft(arg, "-") {
	case "hosts-file", "config-dir", "port":
		return true
	}
	return false
}

func removeArgs(args []string, drop ...string) []string {
	var out []string
	for _, a := range args {
		keep := true
		for _, d := range drop {
			if a == d {
				keep = false
			}
		}
		if keep {
			out = append(out, a)
		}
	}
	return out
}

// hasFlag reports whether a boolean flag appears in args (before parsing).
func hasFlag(args []string, name string) bool {
	for _, a := range args {
		if a == "--" {
			return false
		}
		if a == "-"+name || a == "--"+name || a == "--"+name+"=true" || a == "-"+name+"=true" {
			return true
		}
	}
	return false
}

// parseInterspersed parses flags that may appear before, between or after
// positional arguments, e.g. `localdns remove app.local --yes`.
func parseInterspersed(fs *flag.FlagSet, args []string) ([]string, error) {
	var positional []string
	for {
		if err := fs.Parse(args); err != nil {
			return nil, err
		}
		args = fs.Args()
		if len(args) == 0 {
			return positional, nil
		}
		positional = append(positional, args[0])
		args = args[1:]
	}
}

func flagError(err error) string {
	msg := err.Error()
	if name, ok := strings.CutPrefix(msg, "flag provided but not defined: "); ok {
		return "unknown flag --" + strings.TrimLeft(name, "-")
	}
	return msg
}

func (c *runCtx) registerFlags(fs *flag.FlagSet) {
	fs.BoolVar(&c.o.help, "help", false, "")
	fs.BoolVar(&c.o.help, "h", false, "")
	fs.BoolVar(&c.o.json, "json", false, "")
	fs.BoolVar(&c.o.noColor, "no-color", false, "")
	fs.StringVar(&c.o.hostsFile, "hosts-file", "", "")
	fs.StringVar(&c.o.configDir, "config-dir", "", "")
	for _, f := range c.cmd.flags {
		switch f {
		case flagYes:
			fs.BoolVar(&c.o.yes, "yes", false, "")
			fs.BoolVar(&c.o.yes, "y", false, "")
		case flagForce:
			fs.BoolVar(&c.o.force, "force", false, "")
		case flagNoElevate:
			fs.BoolVar(&c.o.noElevate, "no-elevate", false, "")
		case flagPort:
			fs.IntVar(&c.o.port, "port", 7357, "")
		case flagOpen:
			fs.BoolVar(&c.o.open, "open", false, "")
		case flagRouterPort:
			fs.IntVar(&c.o.routerPort, "port", 80, "")
		case flagNoOpen:
			fs.BoolVar(&c.o.noOpen, "no-open", false, "")
		case flagKeepBinary:
			fs.BoolVar(&c.o.keepBinary, "keep-binary", false, "")
		}
	}
}

func (c *runCtx) resolvePaths() {
	c.hostsPath = platform.DefaultHostsFile()
	if v := c.env.Getenv(platform.EnvHostsFile); v != "" {
		c.hostsPath, c.hostsOverride = v, true
	}
	if c.o.hostsFile != "" {
		c.hostsPath, c.hostsOverride = c.o.hostsFile, true
	}
	c.configDir = platform.DefaultConfigDir()
	if v := c.env.Getenv(platform.EnvConfigDir); v != "" {
		c.configDir, c.configOverride = v, true
	}
	if c.o.configDir != "" {
		c.configDir, c.configOverride = c.o.configDir, true
	}
	if abs, err := filepath.Abs(c.hostsPath); err == nil {
		c.hostsPath = abs
	}
	if abs, err := filepath.Abs(c.configDir); err == nil {
		c.configDir = abs
	}
}

func (c *runCtx) manager() *core.Manager {
	if c.mgr == nil {
		c.mgr = c.newManager(false)
	}
	return c.mgr
}

func (c *runCtx) newManager(canElevate bool) *core.Manager {
	var hf hosts.File = hosts.NewDiskFile(c.hostsPath)
	if c.env.HostsFile != nil {
		hf = c.env.HostsFile(c.hostsPath)
	}
	return core.New(core.Options{
		Hosts:          hf,
		Store:          config.NewStore(c.configDir),
		PermissionHint: c.permissionHint(),
		CanElevate:     canElevate,
	})
}

func (c *runCtx) permissionHint() string {
	if runtime.GOOS == "windows" {
		return "Open PowerShell or Windows Terminal with \"Run as administrator\" and run the command again."
	}
	return "Run it with sudo: sudo localdns " + quoteArgs(removeArgs(c.env.Args, "--no-elevate", "-no-elevate"))
}

func quoteArgs(args []string) string {
	out := make([]string, len(args))
	for i, a := range args {
		if a == "" || strings.ContainsAny(a, " \t\"'$`\\*?[]{}()<>|&;!#~") {
			a = "'" + strings.ReplaceAll(a, "'", `'\''`) + "'"
		}
		out[i] = a
	}
	return strings.Join(out, " ")
}

// fail prints err (as JSON with --json, otherwise to stderr) and returns the
// matching exit code.
func (c *runCtx) fail(err error) int {
	e := core.AsError(err)
	if c.o.json {
		writeJSON(c.env.Stdout, jsonErrorBody{Error: jsonError{Code: e.Code, Message: e.Message, Hint: e.Hint}})
	} else {
		c.errp.printf("%s %s\n", c.errp.fail(), e.Message)
		if e.Hint != "" {
			c.errp.println()
			for _, line := range strings.Split(e.Hint, "\n") {
				c.errp.println("  " + line)
			}
		}
	}
	return exitCode(e.Code)
}

// jsonError is the shape of every error printed with --json:
// {"error": {"code": "...", "message": "...", "hint": "..."}}.
type jsonError struct {
	Code    core.Code `json:"code"`
	Message string    `json:"message"`
	Hint    string    `json:"hint,omitempty"`
}

type jsonErrorBody struct {
	Error jsonError `json:"error"`
}

func exitCode(code core.Code) int {
	switch code {
	case core.CodeInvalidInput, codeUsage:
		return ExitUsage
	case core.CodeNotFound:
		return ExitNotFound
	case core.CodeExists, core.CodeConflict, core.CodeNotManaged:
		return ExitConflict
	case core.CodePermission:
		return ExitPermission
	case core.CodeHostsInvalid, core.CodeConfigInvalid, core.CodeHostsNotFound:
		return ExitInvalidState
	case codeConfirmation, codeCanceled:
		return ExitNotConfirmed
	}
	return ExitError
}

// confirm asks a yes/no question. It never blocks without a terminal: in
// JSON mode or non-interactive sessions it fails unless --yes was given.
func (c *runCtx) confirm(question string, yesHint string) error {
	if c.o.yes {
		return nil
	}
	if c.o.json || !c.env.Interactive {
		return &core.Error{Code: codeConfirmation,
			Message: "confirmation required",
			Hint:    "Re-run with --yes to confirm: " + yesHint}
	}
	c.out.printf("%s\n\n[y/N] ", question)
	line, err := bufio.NewReader(c.env.Stdin).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return err
	}
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "y", "yes":
		c.out.println()
		return nil
	}
	if line == "" || !strings.HasSuffix(line, "\n") {
		c.out.println()
	}
	return &core.Error{Code: codeCanceled, Message: "canceled; nothing was changed"}
}

// tryElevate re-runs the current command with administrator rights when err
// is a permission error and elevation is possible. It returns the elevated
// command's exit code and true if it ran.
func (c *runCtx) tryElevate(err error, extraArgs ...string) (int, bool) {
	if core.ErrorCode(err) != core.CodePermission || c.o.noElevate || c.env.Getenv(EnvNoElevate) != "" {
		return 0, false
	}
	if c.env.Executable == "" || !c.env.Elevator.Available(c.env.Interactive) {
		return 0, false
	}
	args := append([]string{}, c.env.Args...)
	args = append(args, extraArgs...)
	// Environment variables do not survive sudo, so pass overridden paths
	// explicitly.
	if c.hostsOverride {
		args = append(args, "--hosts-file="+c.hostsPath)
	}
	if c.configOverride {
		args = append(args, "--config-dir="+c.configDir)
	}
	if !c.env.Color || c.o.noColor {
		// sudo drops NO_COLOR from the environment; keep the choice.
		args = append(args, "--no-color")
	}
	args = append(args, "--no-elevate")
	if c.env.Interactive {
		if c.elevateNote != "" {
			c.errp.printf("%s", c.elevateNote)
		} else {
			c.errp.printf("LocalDNS needs administrator rights to update %s.\nAsking %s for permission…\n\n",
				c.hostsPath, c.env.Elevator.Describe())
		}
	}
	code, rerr := c.env.Elevator.Run(c.env.Executable, args, c.env.Interactive, c.env.Stdin, c.env.Stdout, c.env.Stderr)
	if rerr != nil {
		return c.fail(err), true
	}
	return code, true
}
