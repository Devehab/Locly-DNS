package router

import (
	"errors"
	"io/fs"
	"strings"
)

// Background service identifiers.
const (
	LaunchdLabel  = "dev.locly.router"
	SystemdUnit   = "localdns-router.service"
	WindowsRunKey = "LocalDNS Router"
)

// ErrNeedsAdmin means enabling or disabling the service needs administrator
// rights (Linux installs a system unit). It wraps fs.ErrPermission so the CLI
// can re-run the command with sudo.
var ErrNeedsAdmin = &adminError{}

type adminError struct{}

func (*adminError) Error() string {
	return "administrator rights are needed to manage the router service"
}
func (*adminError) Unwrap() error        { return fs.ErrPermission }
func (*adminError) Is(target error) bool { return target == fs.ErrPermission }

// ErrUnsupported means there is no supported service manager.
var ErrUnsupported = errors.New("no supported service manager found")

// ServiceState describes the background service.
type ServiceState struct {
	Installed bool   `json:"installed"`
	Kind      string `json:"kind"`           // launchd, systemd, login item
	Path      string `json:"path,omitempty"` // plist, unit file or registry value
	Log       string `json:"log,omitempty"`
}

func xmlEscape(s string) string {
	r := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;", "'", "&apos;")
	return r.Replace(s)
}

// launchdPlist is the macOS LaunchAgent: it runs as the logged-in user (not
// root) at login and is restarted if it stops. macOS lets ordinary users
// listen on port 80. It is restarted when it fails (e.g. port 80 was busy),
// not when it exits cleanly because a router is already running.
func launchdPlist(exe, logPath string) string {
	return `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key>
  <string>` + LaunchdLabel + `</string>
  <key>ProgramArguments</key>
  <array>
    <string>` + xmlEscape(exe) + `</string>
    <string>router</string>
    <string>run</string>
  </array>
  <key>RunAtLoad</key>
  <true/>
  <key>KeepAlive</key>
  <dict>
    <key>SuccessfulExit</key>
    <false/>
  </dict>
  <key>ThrottleInterval</key>
  <integer>30</integer>
  <key>ProcessType</key>
  <string>Background</string>
  <key>StandardOutPath</key>
  <string>` + xmlEscape(logPath) + `</string>
  <key>StandardErrorPath</key>
  <string>` + xmlEscape(logPath) + `</string>
</dict>
</plist>
`
}

// systemdUnit is the Linux service. It runs as an unprivileged user and is
// only granted the right to listen on port 80 (CAP_NET_BIND_SERVICE).
func systemdUnit(exe, user string) string {
	runAs := "DynamicUser=yes"
	if user != "" {
		runAs = "User=" + user
	}
	return `[Unit]
Description=LocalDNS router (port-free URLs for local hostnames, 127.0.0.1 only)
After=network.target

[Service]
ExecStart="` + exe + `" router run
` + runAs + `
AmbientCapabilities=CAP_NET_BIND_SERVICE
CapabilityBoundingSet=CAP_NET_BIND_SERVICE
NoNewPrivileges=yes
ProtectSystem=strict
PrivateTmp=yes
Restart=on-failure
RestartSec=5

[Install]
WantedBy=multi-user.target
`
}
