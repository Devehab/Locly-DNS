package router

import (
	"errors"
	"io/fs"
	"sort"
	"strings"
)

// Background service identifiers.
const (
	LaunchdLabel  = "dev.locly.router"
	SystemdUnit   = "localdns-router.service"
	WindowsRunKey = "LocalDNS Router"
)

// ServiceUser is the account the macOS router runs as: the standard
// unprivileged "nobody" user, never root.
const ServiceUser = "nobody"

// ErrNeedsAdmin means enabling or disabling the service needs administrator
// rights (macOS and Linux install a system service). It wraps
// fs.ErrPermission so the CLI can re-run the command with sudo.
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
	// Outdated means an older router service is installed that must be
	// replaced (`localdns router enable`): LocalDNS 0.2.0's macOS
	// LaunchAgent could not open 127.0.0.1:80.
	Outdated bool `json:"outdated,omitempty"`
}

func xmlEscape(s string) string {
	r := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;", "'", "&apos;")
	return r.Replace(s)
}

func sortedKeys(env map[string]string) []string {
	keys := make([]string, 0, len(env))
	for k := range env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// launchdPlist is the macOS LaunchDaemon. macOS only lets root open port 80
// on 127.0.0.1, so launchd opens that socket itself and starts the router
// as the unprivileged ServiceUser when the first request arrives, handing
// it the listening socket on standard input (inetd "wait" mode). The
// router never runs as root. env carries hosts file and config overrides.
func launchdPlist(exe string, env map[string]string) string {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>
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
  <key>UserName</key>
  <string>` + ServiceUser + `</string>
  <key>GroupName</key>
  <string>` + ServiceUser + `</string>
  <key>Sockets</key>
  <dict>
    <key>Listener</key>
    <dict>
      <key>SockNodeName</key>
      <string>` + ListenAddr + `</string>
      <key>SockServiceName</key>
      <string>80</string>
      <key>SockFamily</key>
      <string>IPv4</string>
      <key>SockType</key>
      <string>stream</string>
    </dict>
  </dict>
  <key>inetdCompatibility</key>
  <dict>
    <key>Wait</key>
    <true/>
  </dict>
  <key>EnvironmentVariables</key>
  <dict>
    <key>` + EnvListenFD + `</key>
    <string>0</string>
`)
	for _, k := range sortedKeys(env) {
		b.WriteString("    <key>" + xmlEscape(k) + "</key>\n    <string>" + xmlEscape(env[k]) + "</string>\n")
	}
	b.WriteString(`  </dict>
</dict>
</plist>
`)
	return b.String()
}

// systemdQuote quotes s for a systemd unit file value.
func systemdQuote(s string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`, "%", "%%", "\n", " ").Replace(s) + `"`
}

// systemdUnit is the Linux service. It runs as an unprivileged user and is
// only granted the right to listen on port 80 (CAP_NET_BIND_SERVICE).
func systemdUnit(exe, user string, env map[string]string) string {
	runAs := "DynamicUser=yes"
	if user != "" {
		runAs = "User=" + user
	}
	var envLines strings.Builder
	for _, k := range sortedKeys(env) {
		envLines.WriteString("Environment=" + systemdQuote(k+"="+env[k]) + "\n")
	}
	return `[Unit]
Description=LocalDNS router (port-free URLs for local hostnames, 127.0.0.1 only)
After=network.target

[Service]
ExecStart=` + systemdQuote(exe) + ` router run
` + runAs + `
` + envLines.String() + `AmbientCapabilities=CAP_NET_BIND_SERVICE
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
