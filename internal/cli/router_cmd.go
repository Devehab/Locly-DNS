package cli

import (
	"errors"
	"fmt"
	"io/fs"
	"strconv"
	"sync"
	"time"

	"github.com/devehab/locly-dns/internal/core"
	"github.com/devehab/locly-dns/internal/router"
	"github.com/devehab/locly-dns/internal/version"
)

// EnvRouterService set to "off" disables router service management and
// probing (used by tests so they never touch a developer's real router).
const EnvRouterService = "LOCALDNS_ROUTER_SERVICE"

// RouterControl manages the background router. Tests replace it.
type RouterControl interface {
	State() router.ServiceState
	Enable(exe string) error
	Disable() error
	Running() bool
}

type systemRouter struct{}

func (systemRouter) State() router.ServiceState { return router.State() }
func (systemRouter) Enable(exe string) error    { return router.Enable(exe) }
func (systemRouter) Disable() error             { return router.Disable() }
func (systemRouter) Running() bool              { ok, _ := router.Probe(router.DefaultPort); return ok }

type offRouter struct{}

func (offRouter) State() router.ServiceState { return router.ServiceState{Kind: "off"} }
func (offRouter) Enable(string) error {
	return errors.New("router service management is turned off (" + EnvRouterService + "=off)")
}
func (offRouter) Disable() error { return nil }
func (offRouter) Running() bool  { return false }

// routerRunning is checked once per command, and only when an entry could
// use a port-free URL.
func (c *runCtx) routerRunning() bool {
	if c.routerChecked {
		return c.routerUp
	}
	c.routerChecked = true
	c.routerUp = c.env.Router.Running()
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
	running := c.env.Router.Running()
	if c.o.json {
		writeJSON(c.env.Stdout, map[string]any{
			"running": running, "installed": st.Installed, "kind": st.Kind, "path": st.Path, "log": st.Log,
			"address": router.ListenAddr + ":80",
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
	p.printf("  %-16s %s\n", "Starts at login", starts)
	if st.Path != "" && st.Installed {
		p.printf("  %-16s %s\n", "Service", st.Path)
	}
	if st.Log != "" && st.Installed {
		p.printf("  %-16s %s\n", "Log", st.Log)
	}
	if !running {
		p.println()
		if st.Installed {
			p.println("The service is installed but not answering. Check the log above, or run")
			p.println("`localdns router run` to see why (another program may be using port 80).")
		} else {
			p.println("Turn them on with: localdns router enable")
		}
	}
	return ExitOK
}

func routerEnable(c *runCtx) int {
	if err := c.env.Router.Enable(c.env.Executable); err != nil {
		if errors.Is(err, fs.ErrPermission) {
			perm := &core.Error{Code: core.CodePermission,
				Message: "installing the router service needs administrator rights", Hint: c.permissionHint()}
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
		running = c.env.Router.Running()
	}
	if c.o.json {
		writeJSON(c.env.Stdout, map[string]any{"enabled": true, "running": running})
		return ExitOK
	}
	p := c.out
	p.println(p.ok() + " Router enabled: it starts automatically when you log in")
	if running {
		p.println(p.ok() + " Port-free URLs are on")
		p.println()
		p.println("Open your names without a port, for example:")
		p.println("  http://app.local   (instead of http://app.local:3000)")
	} else {
		p.println(p.warn() + " It is not answering yet. Run `localdns router status` in a few seconds;")
		p.println("  if port 80 is used by another program, stop that program first.")
	}
	return ExitOK
}

func routerDisable(c *runCtx) int {
	if err := c.env.Router.Disable(); err != nil {
		if errors.Is(err, fs.ErrPermission) {
			perm := &core.Error{Code: core.CodePermission,
				Message: "removing the router service needs administrator rights", Hint: c.permissionHint()}
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
	ln, err := router.Listen(port)
	if err != nil {
		if ok, _ := router.Probe(port); ok {
			c.out.println(c.out.ok() + " The LocalDNS router is already running")
			return ExitOK
		}
		return c.fail(&core.Error{Code: codePortInUse,
			Message: fmt.Sprintf("cannot listen on %s:%d: %v", router.ListenAddr, port, err),
			Hint:    "Another program is using this port. Stop it, or try: localdns router run --port 8080"})
	}
	defer router.WritePID()()
	r := router.New(c.manager().List, version.Version)
	if !c.o.json {
		c.out.printf("%s LocalDNS router listening on http://%s:%d (this machine only)\n", c.out.ok(), router.ListenAddr, port)
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
	if c.env.Router.State().Installed {
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
	case st.Installed:
		ch.Status = core.CheckWarn
		ch.Message = "installed but not answering on " + router.ListenAddr + ":80"
		ch.Fix = "Run `localdns router run` to see the error (another program may be using port 80)."
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
			up = c.env.Router.Running()
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
