// Package router lets local services be opened without typing their port:
// http://app.local instead of http://app.local:3000.
//
// The hosts file can only map a name to an address, never to a port, so a
// browser opening http://app.local connects to port 80. The router listens
// there, on 127.0.0.1 only, reads the Host header and forwards the request to
// the port LocalDNS recorded for that name. It serves nothing but names
// managed by LocalDNS: any other Host gets a 404, and it never touches other
// traffic, system proxy settings or DNS.
package router

import (
	"context"
	"errors"
	"fmt"
	"html"
	"net"
	"net/http"
	"net/http/httputil"
	"net/netip"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/devehab/locly-dns/internal/core"
)

// ListenAddr is the only address the router listens on.
const ListenAddr = "127.0.0.1"

// DefaultPort is the standard HTTP port, so URLs need no port at all.
const DefaultPort = 80

// HealthPath answers "is the LocalDNS router running?" for 127.0.0.1/localhost.
const HealthPath = "/__localdns/router"

// Lister returns the managed entries (core.Manager.List).
type Lister func() ([]core.Entry, error)

// Router forwards requests for managed names to their ports.
type Router struct {
	list    Lister
	version string

	mu      sync.Mutex
	cached  map[string]core.Entry
	expires time.Time
}

// New returns a Router reading entries through list.
func New(list Lister, version string) *Router {
	return &Router{list: list, version: version}
}

// lookup finds the entry for host, re-reading the hosts file and config at
// most once a second so changes made with `localdns add` apply right away.
func (r *Router) lookup(host string) (core.Entry, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if time.Now().After(r.expires) {
		entries, err := r.list()
		if err != nil {
			return core.Entry{}, false, err
		}
		r.cached = make(map[string]core.Entry, len(entries))
		for _, e := range entries {
			r.cached[e.Hostname] = e
		}
		r.expires = time.Now().Add(time.Second)
	}
	e, ok := r.cached[host]
	return e, ok, nil
}

// hostOnly lowercases the Host header and strips any port.
func hostOnly(h string) string {
	if host, _, err := net.SplitHostPort(h); err == nil {
		h = host
	}
	return strings.TrimSuffix(strings.ToLower(h), ".")
}

func (r *Router) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	host := hostOnly(req.Host)
	if host == ListenAddr || host == "localhost" {
		if req.URL.Path == HealthPath {
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("X-LocalDNS-Router", r.version)
			_, _ = fmt.Fprintf(w, "{\"ok\":true,\"version\":%q}\n", r.version)
			return
		}
		page(w, http.StatusNotFound, "LocalDNS router",
			"This is the LocalDNS router. Open one of your names instead, for example <code>http://app.local</code>. "+
				"Run <code>localdns list</code> to see them.")
		return
	}

	e, ok, err := r.lookup(host)
	switch {
	case err != nil:
		page(w, http.StatusInternalServerError, "LocalDNS can't read its entries",
			html.EscapeString(err.Error())+"<br>Run <code>localdns doctor</code> for details.")
		return
	case !ok:
		page(w, http.StatusNotFound, html.EscapeString(host)+" isn't managed by LocalDNS",
			"Add it with <code>localdns add "+html.EscapeString(host)+" 127.0.0.1:PORT</code>, or open the dashboard with <code>localdns ui</code>.")
		return
	case e.Status == core.StatusPaused:
		page(w, http.StatusNotFound, html.EscapeString(host)+" is paused",
			"It is switched off in LocalDNS. Turn it back on in the dashboard (<code>localdns ui</code>) "+
				"or with <code>localdns resume "+html.EscapeString(host)+"</code>.")
		return
	case e.Port == nil:
		page(w, http.StatusNotFound, html.EscapeString(host)+" has no port",
			"LocalDNS knows <b>"+html.EscapeString(host)+"</b> → "+html.EscapeString(e.IP)+" but not which port the app uses. "+
				"Set it with <code>localdns add "+html.EscapeString(host)+" "+html.EscapeString(e.IP)+":PORT --force</code>.")
		return
	}

	ip, err := netip.ParseAddr(e.IP)
	if err != nil {
		page(w, http.StatusBadGateway, "Invalid address for "+html.EscapeString(host), html.EscapeString(e.IP))
		return
	}
	target := &url.URL{Scheme: "http", Host: netip.AddrPortFrom(ip, *e.Port).String()}
	proxy := &httputil.ReverseProxy{
		Rewrite: func(pr *httputil.ProxyRequest) {
			pr.SetURL(target)
			pr.Out.Host = pr.In.Host // the app sees app.local, as if opened directly
			pr.SetXForwarded()
		},
		ErrorHandler: func(w http.ResponseWriter, _ *http.Request, err error) {
			if host == core.DashboardHostname {
				page(w, http.StatusBadGateway, "The LocalDNS dashboard isn't running",
					"Start it in a terminal with <code>localdns ui</code>, then reload this page. "+
						"It stops when you close that terminal or press Ctrl+C.")
				return
			}
			page(w, http.StatusBadGateway, "Nothing is running on "+html.EscapeString(target.Host),
				"<b>"+html.EscapeString(host)+"</b> points to <code>"+html.EscapeString(target.Host)+"</code>, but no app answered there. "+
					"Start your app (or check its port), then reload this page.<br><small>"+html.EscapeString(errSummary(err))+"</small>")
		},
	}
	proxy.ServeHTTP(w, req)
}

func errSummary(err error) string {
	var opErr *net.OpError
	if errors.As(err, &opErr) && opErr.Err != nil {
		return opErr.Err.Error()
	}
	return err.Error()
}

func page(w http.ResponseWriter, status int, title, body string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(status)
	_, _ = fmt.Fprintf(w, `<!doctype html><html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>%s</title><style>
body{margin:0;font:16px/1.6 -apple-system,BlinkMacSystemFont,"Segoe UI",Roboto,sans-serif;background:#f6f8fb;color:#111827;display:grid;place-items:center;min-height:100vh}
main{max-width:560px;margin:24px;padding:32px;background:#fff;border:1px solid #e5e7eb;border-radius:14px;box-shadow:0 8px 24px rgba(15,23,42,.07)}
h1{font-size:20px;margin:0 0 12px}p{margin:0;color:#374151}code{font:14px ui-monospace,Menlo,Consolas,monospace;background:#f3f5f9;padding:1px 6px;border-radius:6px}
small{color:#6b7280}footer{margin-top:20px;font-size:13px;color:#6b7280}
@media (prefers-color-scheme:dark){body{background:#0a0e17;color:#e7ebf3}main{background:#111827;border-color:#1f2a3c}p{color:#c3cad8}code{background:#162032}}
</style></head><body><main><h1>%s</h1><p>%s</p><footer>LocalDNS router · 127.0.0.1 only</footer></main></body></html>`,
		stripTags(title), title, body)
}

func stripTags(s string) string {
	var b strings.Builder
	in := false
	for _, r := range s {
		switch {
		case r == '<':
			in = true
		case r == '>':
			in = false
		case !in:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// Listen opens the router's listener on 127.0.0.1.
func Listen(port int) (net.Listener, error) {
	return net.Listen("tcp", net.JoinHostPort(ListenAddr, strconv.Itoa(port)))
}

// EnvListenFD names a file descriptor the router inherits already listening.
// On macOS launchd opens 127.0.0.1:80 and passes it as descriptor 0.
const EnvListenFD = "LOCALDNS_ROUTER_FD"

// InheritedListener returns the listening socket on file descriptor fd. It
// refuses a socket that isn't bound to the loopback interface, so a
// misconfigured service can never expose the router to the network.
func InheritedListener(fd string) (net.Listener, error) {
	n, err := strconv.Atoi(fd)
	if err != nil || n < 0 {
		return nil, fmt.Errorf("invalid %s=%q", EnvListenFD, fd)
	}
	f := os.NewFile(uintptr(n), "inherited-listener")
	if f == nil {
		return nil, fmt.Errorf("%s=%d is not an open file", EnvListenFD, n)
	}
	defer func() { _ = f.Close() }()
	ln, err := net.FileListener(f)
	if err != nil {
		return nil, fmt.Errorf("%s=%d is not a listening socket: %w", EnvListenFD, n, err)
	}
	if !isLoopback(ln.Addr()) {
		_ = ln.Close()
		return nil, fmt.Errorf("refusing to serve on %s: the router only listens on %s", ln.Addr(), ListenAddr)
	}
	return ln, nil
}

func isLoopback(addr net.Addr) bool {
	ap, err := netip.ParseAddrPort(addr.String())
	return err == nil && ap.Addr().Unmap().IsLoopback()
}

// Serve runs the router on ln until ctx is canceled.
func Serve(ctx context.Context, ln net.Listener, h http.Handler) error {
	srv := &http.Server{
		Handler:           h,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       2 * time.Minute,
		// No read/write timeouts: dev servers keep long-lived connections
		// (hot reload, streaming, WebSockets).
	}
	errc := make(chan error, 1)
	go func() { errc <- srv.Serve(ln) }()
	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
		if err := <-errc; err != nil && !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	}
}

// Probe reports whether a LocalDNS router answers on port and returns its
// version. It only contacts 127.0.0.1.
func Probe(port int) (bool, string) {
	client := &http.Client{Timeout: 400 * time.Millisecond, Transport: &http.Transport{Proxy: nil}}
	u := "http://" + net.JoinHostPort(ListenAddr, strconv.Itoa(port)) + HealthPath
	resp, err := client.Get(u)
	if err != nil {
		return false, ""
	}
	defer func() { _ = resp.Body.Close() }()
	v := resp.Header.Get("X-LocalDNS-Router")
	return resp.StatusCode == http.StatusOK && v != "", v
}

// ShortURL returns the port-free URL for e when the router can serve it.
func ShortURL(e core.Entry, port int) string {
	if !e.Routable() {
		return ""
	}
	if port == DefaultPort {
		return "http://" + e.Hostname
	}
	return "http://" + e.Hostname + ":" + strconv.Itoa(port)
}
