// Package ui serves the LocalDNS web interface. It is a thin layer over
// core.Manager: every action goes through the same business logic as the CLI.
//
// The server only ever listens on the loopback interface. It also defends
// against other websites talking to it through the browser:
//   - the Host header must be 127.0.0.1:<port> or localhost:<port>, which
//     defeats DNS-rebinding attacks. localdns.local (the dashboard's name) is
//     accepted only while the hosts file maps it to this server, because then
//     no other machine can answer for that name;
//   - every API call must carry a per-process random token that is embedded
//     in the page and unreadable by other origins (CSRF protection);
//   - cross-origin requests are rejected and no CORS headers are sent;
//   - a strict Content-Security-Policy forbids inline and third-party scripts.
package ui

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io/fs"
	"net"
	"net/http"
	"net/netip"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/devehab/locly-dns/internal/core"
	"github.com/devehab/locly-dns/internal/validate"
)

// LoopbackAddr is the only address the UI listens on.
const LoopbackAddr = "127.0.0.1"

// DefaultPort is the default UI port.
const DefaultPort = core.DashboardPort

// TokenHeader carries the per-process API token.
const TokenHeader = "X-LocalDNS-Token" //nolint:gosec // G101: a header name, not a credential

//go:embed static
var staticFS embed.FS

// Options configures the handler.
type Options struct {
	// Port the server is reachable on; used to validate Host and Origin.
	Port int
	// Version is shown in the page footer.
	Version string
	// ReadOnlyHint explains how to get write access when the hosts file is
	// not writable (e.g. "sudo localdns ui").
	ReadOnlyHint string
	// Token overrides the random API token (tests only).
	Token string
	// ShortURL returns an entry's port-free URL (http://app.local) when the
	// LocalDNS router serves it, or "". Nil means never.
	ShortURL func(core.Entry) string
	// Router lets the dashboard show and switch port-free URLs. Nil hides
	// the switch.
	Router RouterSwitch
}

// RouterSwitch turns the port-free router on and off for the dashboard.
type RouterSwitch interface {
	RouterStatus() RouterStatus
	EnableRouter() error
	DisableRouter() error
}

// RouterStatus describes the port-free router.
type RouterStatus struct {
	Running   bool `json:"running"`
	Installed bool `json:"installed"`
	// CanChange reports whether this dashboard has the rights to switch it.
	CanChange bool `json:"can_change"`
}

type asset struct {
	contentType string
	data        []byte
}

type server struct {
	m      *core.Manager
	opts   Options
	token  string
	index  *template.Template
	guide  []byte
	assets map[string]asset

	mu        sync.Mutex
	mapped    bool // the hosts file maps DashboardHostname to this server
	mappedAt  time.Time
	routerErr string // last error from switching the router off
}

// NewHandler returns the UI's HTTP handler.
func NewHandler(m *core.Manager, opts Options) (http.Handler, error) {
	token := opts.Token
	if token == "" {
		b := make([]byte, 32)
		if _, err := rand.Read(b); err != nil {
			return nil, err
		}
		token = hex.EncodeToString(b)
	}
	index, err := template.ParseFS(staticFS, "static/index.html")
	if err != nil {
		return nil, err
	}
	// Static files are loaded once, from a fixed list, so requests can only
	// ever select one of them.
	assets := map[string]asset{}
	for name, ctype := range assetTypes {
		data, err := fs.ReadFile(staticFS, "static/"+name)
		if err != nil {
			return nil, err
		}
		assets[name] = asset{contentType: ctype, data: data}
	}
	guide, err := fs.ReadFile(staticFS, "static/guide.html")
	if err != nil {
		return nil, err
	}
	s := &server{m: m, opts: opts, token: token, index: index, guide: guide, assets: assets}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", s.serveIndex)
	mux.HandleFunc("GET /guide", s.serveGuide)
	mux.HandleFunc("GET /assets/{file}", s.serveAsset)
	mux.HandleFunc("GET /healthz", s.serveHealth)
	mux.HandleFunc("GET /api/status", s.api(s.getStatus))
	mux.HandleFunc("GET /api/entries", s.api(s.listEntries))
	mux.HandleFunc("POST /api/entries", s.api(s.addEntry))
	mux.HandleFunc("PUT /api/entries/{hostname}", s.api(s.editEntry))
	mux.HandleFunc("DELETE /api/entries/{hostname}", s.api(s.removeEntry))
	mux.HandleFunc("POST /api/entries/{hostname}/pause", s.api(s.pauseEntry))
	mux.HandleFunc("POST /api/entries/{hostname}/resume", s.api(s.resumeEntry))
	mux.HandleFunc("GET /api/router", s.api(s.getRouter))
	mux.HandleFunc("POST /api/router", s.api(s.setRouter))
	return s.secure(mux), nil
}

// allowedHost reports whether host (a Host header or Origin host) names this
// server: via a loopback name, or via the dashboard's own name while the
// hosts file maps it here (directly or through the router on port 80).
func (s *server) allowedHost(host string) bool {
	port := strconv.Itoa(s.opts.Port)
	switch host {
	case LoopbackAddr + ":" + port, "localhost:" + port:
		return true
	case core.DashboardHostname, core.DashboardHostname + ":80", core.DashboardHostname + ":" + port:
		return s.dashboardMapped()
	}
	return false
}

// dashboardMapped reports whether the hosts file maps DashboardHostname to
// this server. It is checked at most every two seconds.
func (s *server) dashboardMapped() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if time.Since(s.mappedAt) < 2*time.Second {
		return s.mapped
	}
	e, err := s.m.Get(core.DashboardHostname)
	ip, perr := netip.ParseAddr(e.IP)
	s.mapped = err == nil && perr == nil && ip.IsLoopback() && e.Status == core.StatusActive &&
		e.Port != nil && int(*e.Port) == s.opts.Port
	s.mappedAt = time.Now()
	return s.mapped
}

func (s *server) secure(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Security-Policy", "default-src 'none'; script-src 'self'; style-src 'self'; "+
			"img-src 'self' data:; connect-src 'self'; base-uri 'none'; form-action 'self'; frame-ancestors 'none'")
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Cross-Origin-Opener-Policy", "same-origin")
		h.Set("Cross-Origin-Resource-Policy", "same-origin")
		h.Set("Cache-Control", "no-store")

		if !s.allowedHost(r.Host) {
			http.Error(w, "LocalDNS UI is only available at http://"+LoopbackAddr+":"+strconv.Itoa(s.opts.Port), http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// api wraps API handlers with CSRF and origin checks.
func (s *server) api(fn func(*http.Request) (int, any)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if origin := r.Header.Get("Origin"); origin != "" {
			host := strings.TrimPrefix(origin, "http://")
			if host == origin || !s.allowedHost(host) {
				writeJSON(w, http.StatusForbidden, errorBody("forbidden", "cross-origin requests are not allowed", ""))
				return
			}
		}
		if site := r.Header.Get("Sec-Fetch-Site"); site != "" && site != "same-origin" && site != "none" {
			writeJSON(w, http.StatusForbidden, errorBody("forbidden", "cross-site requests are not allowed", ""))
			return
		}
		got := r.Header.Get(TokenHeader)
		if subtle.ConstantTimeCompare([]byte(got), []byte(s.token)) != 1 {
			writeJSON(w, http.StatusForbidden, errorBody("forbidden", "missing or invalid "+TokenHeader+" header",
				"Reload the LocalDNS page."))
			return
		}
		if (r.Method == http.MethodPost || r.Method == http.MethodPut) &&
			!strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
			writeJSON(w, http.StatusUnsupportedMediaType, errorBody("invalid_input", "expected a JSON body", ""))
			return
		}
		status, body := fn(r)
		writeJSON(w, status, body)
	}
}

func (s *server) serveIndex(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	data := struct {
		Token   string
		Version string
	}{s.token, s.opts.Version}
	if err := s.index.Execute(w, data); err != nil {
		http.Error(w, "template error", http.StatusInternalServerError)
	}
}

func (s *server) serveGuide(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(s.guide)
}

var assetTypes = map[string]string{
	"app.js":      "text/javascript; charset=utf-8",
	"guide.js":    "text/javascript; charset=utf-8",
	"style.css":   "text/css; charset=utf-8",
	"guide.css":   "text/css; charset=utf-8",
	"favicon.svg": "image/svg+xml",
}

func (s *server) serveAsset(w http.ResponseWriter, r *http.Request) {
	a, ok := s.assets[r.PathValue("file")]
	if !ok {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", a.contentType)
	_, _ = w.Write(a.data)
}

func (s *server) serveHealth(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("X-LocalDNS", "1")
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "app": "localdns"})
}

type statusResponse struct {
	core.StatusReport
	Version      string `json:"version"`
	ReadOnlyHint string `json:"read_only_hint,omitempty"`
	// LocalSuffixes are the endings a hostname may use, offered as one-click
	// suggestions in the Add and Edit dialogs.
	LocalSuffixes []string `json:"local_suffixes"`
}

func (s *server) getStatus(*http.Request) (int, any) {
	report, err := s.m.Status()
	if err != nil {
		return s.errorResponse(err)
	}
	resp := statusResponse{StatusReport: report, Version: s.opts.Version, LocalSuffixes: validate.LocalSuffixes}
	if !report.Writable {
		resp.ReadOnlyHint = s.opts.ReadOnlyHint
	}
	return http.StatusOK, resp
}

func (s *server) listEntries(*http.Request) (int, any) {
	list, err := s.m.List()
	if err != nil {
		return s.errorResponse(err)
	}
	if s.opts.ShortURL != nil {
		for i := range list {
			list[i].ShortURL = s.opts.ShortURL(list[i])
		}
	}
	return http.StatusOK, map[string]any{"entries": list}
}

type addRequest struct {
	Hostname string `json:"hostname"`
	Address  string `json:"address"`
	Force    bool   `json:"force"`
}

func (s *server) addEntry(r *http.Request) (int, any) {
	var req addRequest
	dec := json.NewDecoder(http.MaxBytesReader(nil, r.Body, 16<<10))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		return http.StatusBadRequest, errorBody("invalid_input", "invalid JSON body: "+err.Error(), "")
	}
	res, err := s.m.Add(req.Hostname, req.Address, core.AddOptions{Force: req.Force})
	if err != nil {
		return s.errorResponse(err)
	}
	status := http.StatusOK
	if res.Action == core.ActionAdded {
		status = http.StatusCreated
	}
	return status, res
}

type editRequest struct {
	Hostname string `json:"hostname"`
	Address  string `json:"address"`
}

func (s *server) editEntry(r *http.Request) (int, any) {
	var req editRequest
	dec := json.NewDecoder(http.MaxBytesReader(nil, r.Body, 16<<10))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		return http.StatusBadRequest, errorBody("invalid_input", "invalid JSON body: "+err.Error(), "")
	}
	res, err := s.m.Edit(r.PathValue("hostname"), req.Hostname, req.Address)
	if err != nil {
		return s.errorResponse(err)
	}
	return http.StatusOK, res
}

type routerResponse struct {
	Supported bool `json:"supported"`
	RouterStatus
	// DashboardURL is the dashboard's port-free address, when it works.
	DashboardURL string `json:"dashboard_url,omitempty"`
	// DirectURL always works while the dashboard runs, router or not.
	DirectURL string `json:"direct_url"`
	LastError string `json:"last_error,omitempty"`
}

func (s *server) routerState() routerResponse {
	resp := routerResponse{DirectURL: URL(s.opts.Port)}
	if s.opts.Router == nil {
		return resp
	}
	resp.Supported = true
	resp.RouterStatus = s.opts.Router.RouterStatus()
	if resp.Running && s.dashboardMapped() {
		resp.DashboardURL = "http://" + core.DashboardHostname
	}
	s.mu.Lock()
	resp.LastError = s.routerErr
	s.mu.Unlock()
	return resp
}

func (s *server) getRouter(*http.Request) (int, any) {
	return http.StatusOK, s.routerState()
}

// routerOffDelay lets the reply to "turn the router off" reach the browser
// first: the page may itself be loaded through the router.
var routerOffDelay = 800 * time.Millisecond

func (s *server) setRouter(r *http.Request) (int, any) {
	var req struct {
		Enabled *bool `json:"enabled"`
	}
	dec := json.NewDecoder(http.MaxBytesReader(nil, r.Body, 1<<10))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil || req.Enabled == nil {
		return http.StatusBadRequest, errorBody("invalid_input", `expected {"enabled": true} or {"enabled": false}`, "")
	}
	if s.opts.Router == nil {
		return http.StatusNotFound, errorBody("not_found", "the port-free router is not available here", "")
	}
	if !s.opts.Router.RouterStatus().CanChange {
		return http.StatusForbidden, errorBody(string(core.CodePermission),
			"switching port-free URLs needs administrator rights",
			"Stop this dashboard (Ctrl+C) and start it again with: sudo localdns ui")
	}
	s.mu.Lock()
	s.routerErr = ""
	s.mu.Unlock()
	if *req.Enabled {
		if err := s.opts.Router.EnableRouter(); err != nil {
			return http.StatusInternalServerError, errorBody(string(core.CodeInternal),
				"could not turn on port-free URLs: "+err.Error(), "Run `localdns router` in a terminal to see why.")
		}
		s.mu.Lock()
		s.mappedAt = time.Time{} // the dashboard name may have just been added
		s.mu.Unlock()
		return http.StatusOK, s.routerState()
	}
	go func() {
		time.Sleep(routerOffDelay)
		if err := s.opts.Router.DisableRouter(); err != nil {
			s.mu.Lock()
			s.routerErr = "could not turn off port-free URLs: " + err.Error()
			s.mu.Unlock()
		}
	}()
	resp := s.routerState()
	resp.Running, resp.Installed, resp.DashboardURL = false, false, ""
	return http.StatusAccepted, resp
}

func (s *server) pauseEntry(r *http.Request) (int, any) {
	res, err := s.m.Pause(r.PathValue("hostname"))
	if err != nil {
		return s.errorResponse(err)
	}
	return http.StatusOK, res
}

func (s *server) resumeEntry(r *http.Request) (int, any) {
	res, err := s.m.Resume(r.PathValue("hostname"))
	if err != nil {
		return s.errorResponse(err)
	}
	return http.StatusOK, res
}

func (s *server) removeEntry(r *http.Request) (int, any) {
	res, err := s.m.Remove(r.PathValue("hostname"))
	if err != nil {
		return s.errorResponse(err)
	}
	return http.StatusOK, res
}

func (s *server) errorResponse(err error) (int, any) {
	e := core.AsError(err)
	hint := e.Hint
	status := http.StatusInternalServerError
	switch e.Code {
	case core.CodeInvalidInput:
		status = http.StatusBadRequest
	case core.CodeNotFound:
		status = http.StatusNotFound
	case core.CodeExists, core.CodeConflict, core.CodeNotManaged, core.CodeHostsInvalid, core.CodeConfigInvalid:
		status = http.StatusConflict
	case core.CodePermission:
		status = http.StatusForbidden
		hint = s.opts.ReadOnlyHint
	}
	return status, errorBody(string(e.Code), e.Message, hint)
}

func errorBody(code, message, hint string) map[string]any {
	body := map[string]any{"code": code, "message": message}
	if hint != "" {
		body["hint"] = hint
	}
	return map[string]any{"error": body}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(v)
}

// Listen opens the UI listener on the loopback interface. Port 0 picks a free
// port.
func Listen(port int) (net.Listener, error) {
	return net.Listen("tcp", net.JoinHostPort(LoopbackAddr, strconv.Itoa(port)))
}

// Serve runs the HTTP server on ln until ctx is canceled, then shuts down
// gracefully.
func Serve(ctx context.Context, ln net.Listener, h http.Handler) error {
	srv := &http.Server{
		Handler:           h,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    64 << 10,
	}
	errc := make(chan error, 1)
	go func() { errc <- srv.Serve(ln) }()
	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			return err
		}
		if err := <-errc; err != nil && !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	}
}

// URL returns the address users open in a browser.
func URL(port int) string {
	return fmt.Sprintf("http://%s:%d", LoopbackAddr, port)
}

// Probe reports whether a LocalDNS UI is already answering on port. It only
// ever contacts the loopback interface.
func Probe(port int) bool {
	client := &http.Client{
		Timeout: time.Second,
		Transport: &http.Transport{
			Proxy: nil, // never route a loopback probe through a proxy
		},
	}
	resp, err := client.Get(URL(port) + "/healthz")
	if err != nil {
		return false
	}
	defer func() { _ = resp.Body.Close() }()
	return resp.StatusCode == http.StatusOK && resp.Header.Get("X-LocalDNS") == "1"
}

// AvailabilityCheck is a doctor check that the UI can start on port.
func AvailabilityCheck(port int) core.Check {
	c := core.Check{ID: "ui_available", Name: "Local UI available", Status: core.CheckPass}
	if _, err := fs.Stat(staticFS, "static/index.html"); err != nil {
		c.Status = core.CheckFail
		c.Message = "web assets missing from this build"
		c.Fix = "Reinstall LocalDNS."
		return c
	}
	ln, err := Listen(port)
	if err == nil {
		_ = ln.Close()
		c.Message = URL(port) + " (localhost only)"
		return c
	}
	if Probe(port) {
		c.Message = "already running at " + URL(port)
		return c
	}
	c.Status = core.CheckWarn
	c.Message = fmt.Sprintf("port %d is used by another program", port)
	c.Fix = fmt.Sprintf("Start the UI on another port: localdns ui --port %d", port+1)
	return c
}
