package cli

import (
	"errors"
	"fmt"
	"io/fs"
	"net"
	"strconv"
	"sync"
	"time"

	"github.com/devehab/locly-dns/internal/core"
	"github.com/devehab/locly-dns/internal/platform"
	"github.com/devehab/locly-dns/internal/router"
	"github.com/devehab/locly-dns/internal/version"
)

// EnvRouterService set to "off" disables router service management and
// probing (used by tests so they never touch a developer's real router).
const EnvRouterService = "LOCALDNS_ROUTER_SERVICE"

// RouterControl manages the background router. Tests replace it.
type RouterControl interface {
	State() router.ServiceState
	// Enable installs the service for exe; env holds hosts file and config
	// overrides the service should use too.
	Enable(exe string, env map[string]string) error
	Disable() error
	// Running reports whether a LocalDNS router answers on port 80, and
	// its version.
	Running() (bool, string)
}

type systemRouter struct{}

func (systemRouter) State() router.ServiceState { return router.State() }
func (systemRouter) Enable(exe string, env map[string]string) error {
	return router.Enable(exe, env)
}
func (systemRouter) Disable() error          { return router.Disable() }
func (systemRouter) Running() (bool, string) { return router.Probe(router.DefaultPort) }

type offRouter struct{}

func (offRouter) State() router.ServiceState { return router.ServiceState{Kind: "off"} }
func (offRouter) Enable(string, map[string]string) error {
	return errors.New("router service management is turned off (" + EnvRouterService + "=off)")
}
func (offRouter) Disable() error          { return nil }
func (offRouter) Running() (bool, string) { return false, "" }

// routerRunning is checked once per command, and only when an entry could
// use a port-free URL.
func (c *runCtx) routerRunning() bool {
	if c.routerChecked {
		return c.routerUp
	}
	c.routerChecked = true
	c.routerUp, _ = c.env.Router.Running()
	return c.routerUp
}

// withShortURLs fills Entry.ShortURL when the router serves the entry.
func (c *runCtx) withShortURLs(list []core.Entry) []core.Entry {
	for i := range list {
		if list[i].Routable() && c.routerRunning() {
			list[i].ShortURL = router.ShortURL(list[i], router.DefaultPort)
		}
	}
	return list
}

func runRouter(c *runCtx) int {
	action := "status"
	if len(c.args) == 1 {
		action = c.args[0]
	}
	switch action {
	case "status":
		return routerStatus(c)
	case "enable", "on":
		return routerEnable(c)
	case "disable", "off":
		return routerDisable(c)
	case "run":
		return routerRun(c)
	}
	return c.fail(&core.Error{Code: codeUsage, Message: fmt.Sprintf("unknown router action %q", action),
		Hint: "Use one of: localdns router status | enable | disable | run"})
}

func routerStatus(c *runCtx) int {
	st := c.env.Router.State()
	running, runningVersion := c.env.Router.Running()
	if c.o.json {
		writeJSON(c.env.Stdout, map[string]any{
			"running": running, "version": runningVersion, "installed": st.Installed, "outdated": st.Outdated,
			"kind": st.Kind, "path": st.Path, "log": st.Log, "address": router.ListenAddr + ":80",
		})
		return ExitOK
	}
	p := c.out
	p.println(p.bold("LocalDNS router"))
	p.println()
	if running {
		p.println(p.ok() + " Port-free URLs are on: open http://app.local instead of http://app.local:3000")
	} else {
		p.println(p.warn() + " Port-free URLs are off")
	}
	p.printf("  %-16s %s\n", "Listening on", router.ListenAddr+":80 (this machine only)")
	starts := "no"
	if st.Installed {
		starts = "yes (" + st.Kind + ")"
	}
	p.printf("  %-16s %s\n", "Starts on its own", starts)
	if running && runningVersion != version.Version {
		p.printf("  %-16s %s\n", "Version", runningVersion+" (this localdns is "+version.Version+")")
	}
	if st.Path != "" && st.Installed {
		p.printf("  %-16s %s\n", "Service", st.Path)
	}
	if st.Log != "" && st.Installed {
		p.printf("  %-16s %s\n", "Log", st.Log)
	}
	switch {
	case st.Outdated:
		p.println()
		p.println("An older router from LocalDNS 0.2.0 is installed; on macOS it can't open port 80.")
		p.println("Replace it with: sudo localdns router enable")
	case running && runningVersion != version.Version && st.Installed:
		p.println()
		p.println("Update the router to this version with: sudo localdns router enable")
	case !running && st.Installed:
		p.println()
		p.println("The service is installed but not answering. Another program may be using")
		p.println("port 80; check the log above. To see the exact error, run: sudo localdns router run")
	case !running:
		p.println()
		p.println("Turn them on with: localdns router enable")
	}
	return ExitOK
}

// routerEnv is the hosts file and config overrides the service must use, so
// it serves the same entries as this command.
func (c *runCtx) routerEnv() map[string]string {
	env := map[string]string{}
	if c.hostsOverride {
		env[platform.EnvHostsFile] = c.hostsPath
	}
	if c.configOverride {
		env[platform.EnvConfigDir] = c.configDir
	}
	return env
}

func routerEnable(c *runCtx) int {
	if err := c.env.Router.Enable(c.env.Executable, c.routerEnv()); err != nil {
		if errors.Is(err, fs.ErrPermission) {
			perm := &core.Error{Code: core.CodePermission,
				Message: "installing the router service needs administrator rights",
				Hint:    "Run: sudo localdns router enable"}
			c.elevateNote = "Only an administrator can let a program use port 80, so LocalDNS asks for\n" +
				"your password once to install the router service. The router itself never runs\n" +
				"as root, and it only listens on 127.0.0.1 (this computer).\n\n"
			if code, ok := c.tryElevate(perm); ok {
				return code
			}
			return c.fail(perm)
		}
		return c.fail(&core.Error{Code: core.CodeInternal, Message: "could not enable the router: " + err.Error(),
			Hint: "You can still run it in a terminal: localdns router run"})
	}
	running := false
	for i := 0; i < 25 && !running; i++ {
		time.Sleep(200 * time.Millisecond)
		running, _ = c.env.Router.Running()
	}
	if c.o.json {
		writeJSON(c.env.Stdout, map[string]any{"enabled": true, "running": running})
		return ExitOK
	}
	p := c.out
	p.println(p.ok() + " Router enabled: it starts on its own, also after a restart")
	if running {
		p.println(p.ok() + " Port-free URLs are on")
		p.println()
		p.println("Open your names without a port, for example:")
		p.println("  http://app.local   (instead of http://app.local:3000)")
	} else {
		p.println(p.warn() + " It is not answering yet. Run `localdns router` in a few seconds;")
		p.println("  if another program uses port 80, stop that program first.")
	}
	return ExitOK
}

func routerDisable(c *runCtx) int {
	if err := c.env.Router.Disable(); err != nil {
		if errors.Is(err, fs.ErrPermission) {
			perm := &core.Error{Code: core.CodePermission,
				Message: "removing the router service needs administrator rights",
				Hint:    "Run: sudo localdns router disable"}
			c.elevateNote = "Removing the router service needs administrator rights.\n\n"
			if code, ok := c.tryElevate(perm); ok {
				return code
			}
			return c.fail(perm)
		}
		return c.fail(err)
	}
	if c.o.json {
		writeJSON(c.env.Stdout, map[string]any{"enabled": false})
		return ExitOK
	}
	c.out.println(c.out.ok() + " Router disabled; URLs need their port again (http://app.local:3000)")
	return ExitOK
}

func routerRun(c *runCtx) int {
	port := c.o.routerPort
	if fd := c.env.Getenv(router.EnvListenFD); fd != "" {
		// Started by the service manager with the socket already open.
		// Standard output may be that socket: write to the log instead.
		w := router.ServiceLog()
		c.env.Stdout, c.env.Stderr = w, w
		c.out, c.errp = &printer{w: w}, &printer{w: w}
		ln, err := router.InheritedListener(fd)
		if err != nil {
			return c.fail(err)
		}
		return serveRouter(c, ln, port)
	}
	ln, err := router.Listen(port)
	if err != nil {
		if ok, _ := router.Probe(port); ok {
			c.out.println(c.out.ok() + " The LocalDNS router is already running")
			return ExitOK
		}
		hint := "Another program is using this port. Stop it, or try: localdns router run --port 8080"
		if errors.Is(err, fs.ErrPermission) {
			hint = "Only an administrator can open this port. Turn the router on for good with:\n" +
				"  sudo localdns router enable\nor run it in this window with: sudo localdns router run"
		}
		return c.fail(&core.Error{Code: codePortInUse,
			Message: fmt.Sprintf("cannot listen on %s:%d: %v", router.ListenAddr, port, err), Hint: hint})
	}
	return serveRouter(c, ln, port)
}

func serveRouter(c *runCtx, ln net.Listener, port int) int {
	defer router.WritePID()()
	r := router.New(c.manager().List, version.Version)
	if !c.o.json {
		c.out.printf("%s LocalDNS router listening on http://%s (this machine only)\n", c.out.ok(), ln.Addr())
		example := "http://app.local"
		if port != router.DefaultPort {
			example += ":" + strconv.Itoa(port)
		}
		c.out.println("  Open your names through it, e.g. " + example + ". Press Ctrl+C to stop.")
	}
	if err := router.Serve(c.env.Context, ln, r); err != nil {
		return c.fail(err)
	}
	return ExitOK
}

// routerSummary is the one-line router state shown by `localdns status`.
func (c *runCtx) routerSummary() string {
	if c.routerRunning() {
		return c.out.green("on") + " — open names without a port (http://app.local)"
	}
	if st := c.env.Router.State(); st.Outdated {
		return c.out.yellow("outdated") + " — update with: sudo localdns router enable"
	} else if st.Installed {
		return c.out.yellow("installed but not answering") + " — see `localdns router`"
	}
	return "off — turn on with: localdns router enable"
}

// routerCheck is the doctor check for port-free URLs. The router is
// optional, so "off" passes; an installed router that doesn't answer warns.
func (c *runCtx) routerCheck() core.Check {
	ch := core.Check{ID: "router", Name: "Port-free URLs (router)", Status: core.CheckPass}
	st := c.env.Router.State()
	switch {
	case c.routerRunning():
		ch.Message = "on: http://<name> forwards to its port (" + router.ListenAddr + ":80, this machine only)"
	case st.Outdated:
		ch.Status = core.CheckWarn
		ch.Message = "an older router from LocalDNS 0.2.0 is installed; on macOS it can't open port 80"
		ch.Fix = "sudo localdns router enable"
	case st.Installed:
		ch.Status = core.CheckWarn
		ch.Message = "installed but not answering on " + router.ListenAddr + ":80"
		ch.Fix = "Another program may be using port 80. To see the exact error, run: sudo localdns router run"
		if st.Log != "" {
			ch.Fix += "\nLog: " + st.Log
		}
	default:
		ch.Message = "off (optional): turn on with `localdns router enable`"
	}
	return ch
}

// shortURLFunc gives the web UI port-free URLs, re-checking the router at
// most every few seconds so turning it on or off shows up without a restart.
func (c *runCtx) shortURLFunc() func(core.Entry) string {
	var (
		mu      sync.Mutex
		up      bool
		checked time.Time
	)
	return func(e core.Entry) string {
		if !e.Routable() {
			return ""
		}
		mu.Lock()
		if time.Since(checked) > 3*time.Second {
			up, _ = c.env.Router.Running()
			checked = time.Now()
		}
		ok := up
		mu.Unlock()
		if !ok {
			return ""
		}
		return router.ShortURL(e, router.DefaultPort)
	}
}
