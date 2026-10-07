# Locly — `localdns`

**A secure, local-only hostname manager for developers and AI agents.**

🌐 **Website (English / العربية):** <https://devehab.github.io/Locly-DNS/>

Give the services running on your machine (or on your local network) friendly names:

```text
http://127.0.0.1:3000       →  http://app.local
http://127.0.0.1:3002       →  http://api.local
http://192.168.1.60:8123    →  http://ha.local:8123
```

No port to remember either: the optional [router](#port-free-urls) forwards
`http://app.local` to `127.0.0.1:3000` (turned on by the installer, `localdns router disable`
to turn it off).

```console
$ localdns add app.local 127.0.0.1:3000
✓ Added successfully

app.local → 127.0.0.1:3000

URL:
http://app.local   (also http://app.local:3000)
```

Locly is **not a DNS server**. It writes a clearly marked section of your operating system's
`hosts` file and nothing else. Internet domains keep resolving through your normal DNS,
exactly as before.

- One small binary for macOS, Linux and Windows. No runtime, no dependencies.
- A **CLI** built for humans *and* AI agents: deterministic, scriptable, `--json` everywhere, never hangs on a prompt.
- A **web UI** (`localdns ui`) on `127.0.0.1` for people who prefer clicking.
- Both use the same core library, so they always behave the same.

---

## Contents

- [Install](#install)
- [Quick start](#quick-start)
- [Commands](#commands)
- [For AI agents and scripts](#for-ai-agents-and-scripts)
- [Web UI](#web-ui)
- [Port-free URLs](#port-free-urls)
- [How it works](#how-it-works)
- [Security](#security)
- [Uninstall](#uninstall)
- [Troubleshooting](#troubleshooting)
- [Development](#development)

---

## Install

**macOS and Linux**

```sh
curl -fsSL https://github.com/Devehab/Locly-DNS/releases/latest/download/install.sh | sh
```

**Windows (PowerShell)**

```powershell
irm https://github.com/Devehab/Locly-DNS/releases/latest/download/install.ps1 | iex
```

The installer downloads the binary for your platform from
[GitHub Releases](https://github.com/Devehab/Locly-DNS/releases), **verifies its SHA-256
checksum**, and copies it to:

| OS            | Location                                                                      |
| ------------- | ----------------------------------------------------------------------------- |
| macOS / Linux | `/usr/local/bin/localdns` (via `sudo` if needed), else `~/.local/bin/localdns` |
| Windows       | `%LOCALAPPDATA%\Programs\LocalDNS\localdns.exe` (added to your user `PATH`)   |

No shell profile edits, no DNS settings. When you install from your own terminal it also
turns on [port-free URLs](#port-free-urls) (`localdns router enable`) and opens the dashboard.

Installer options (environment variables):

| Variable               | Meaning                                                |
| ---------------------- | ------------------------------------------------------ |
| `LOCALDNS_VERSION`     | Tag to install, e.g. `v0.1.0` (default: latest)        |
| `LOCALDNS_INSTALL_DIR` | Where to put the binary                                |
| `LOCALDNS_BASE_URL`    | Download from a mirror instead of GitHub Releases      |
| `LOCALDNS_NO_ROUTER`   | Set to `1` to skip turning on port-free URLs           |
| `LOCALDNS_NO_UI`       | Set to `1` to skip opening the dashboard (macOS/Linux) |

Example: `curl -fsSL …/install.sh | LOCALDNS_INSTALL_DIR=$HOME/bin sh`

**Manual install:** download `localdns_<os>_<arch>.tar.gz` (or `.zip` on Windows) and
`checksums.txt` from the [latest release](https://github.com/Devehab/Locly-DNS/releases/latest),
check the hash, and put `localdns` on your `PATH`.

**From source** (Go 1.24+): `go install github.com/devehab/locly-dns/cmd/localdns@latest`

---

## Quick start

```sh
localdns add app.local 127.0.0.1:3000     # a local dev server
localdns add api.local 127.0.0.1:3002
localdns add ha.local 192.168.1.60        # a device on your LAN, no port
localdns list
```

```text
LocalDNS

HOSTNAME     ADDRESS           STATUS    OPEN
─────────────────────────────────────────────────────────
app.local    127.0.0.1:3000    ✓         http://app.local
api.local    127.0.0.1:3002    ✓         http://api.local
ha.local     192.168.1.60      ✓

3 entries
```

Open `http://app.local` in your browser. Done. (Without the [router](#port-free-urls), add the
port: `http://app.local:3000`.)

> Editing the hosts file needs administrator rights. On macOS and Linux, `localdns` asks for
> them with `sudo` **for that one command only**. On Windows, run it from a terminal opened
> with **Run as administrator**. Read-only commands (`list`, `status`, `info`, `doctor`) never
> need elevation.

---

## Commands

| Command                              | What it does                                         |
| ------------------------------------ | ---------------------------------------------------- |
| `localdns add <hostname> <ip[:port]>` | Add a hostname (idempotent; `--force` to replace)   |
| `localdns edit <hostname> [ip[:port]] [--name new]` | Change the address, rename, or both |
| `localdns pause <hostname>`          | Switch a hostname off without removing it           |
| `localdns resume <hostname>`         | Switch a paused hostname back on                    |
| `localdns list`                      | List hostnames with address and status              |
| `localdns remove <hostname>`         | Remove a hostname (asks first; `--yes` to skip)     |
| `localdns status`                    | Show file locations, write access and entry health  |
| `localdns doctor`                    | Diagnose problems and explain how to fix them       |
| `localdns ui`                        | Start the web interface on `http://127.0.0.1:7357`  |
| `localdns router [enable\|disable]`  | Port-free URLs: `http://app.local` (see below)      |
| `localdns uninstall`                 | Remove LocalDNS entries, config and the binary      |
| `localdns info`                      | Overview of commands, flags, exit codes and files   |

Every command has detailed help: `localdns add --help`, `localdns remove --help`, …

### `add`

```sh
localdns add app.local 127.0.0.1:3000      # hostname + IP + port
localdns add ha.local 192.168.1.60         # hostname + IP (no port)
localdns add v6.local [::1]:8080           # IPv6
localdns add api.local localhost:3002      # localhost means 127.0.0.1
localdns add web.local http://127.0.0.1:5173/   # a pasted link: just the address is kept
localdns add app.local 127.0.0.1:4000 --force   # change an existing entry
```

The hosts file cannot store ports, so `127.0.0.1:3000` becomes the hosts line
`127.0.0.1 app.local`, and LocalDNS remembers the port in its own config to show the full URL
in `list`, `--json` output and the UI.

Adding an identical entry again succeeds without changing anything. Adding the same hostname
with a *different* address fails with exit code `4` unless you pass `--force`.

### `edit`

```sh
localdns edit app.local 127.0.0.1:4000                      # new address
localdns edit app.local --name web.local                     # rename, same address
localdns edit app.local 127.0.0.1:5173 --name web.local      # both at once
```

The entry keeps its place in the hosts file; nothing else changes. In the dashboard, use the
**Edit** button, and click an ending (`.localhost`, `.local`, …) to swap it.

### `pause` and `resume`

```sh
localdns pause app.local     # switch it off: the line leaves the hosts file
localdns resume app.local    # switch it back on, exactly as it was
```

A paused entry keeps its address and port in LocalDNS's config and shows as `paused`; editing it
keeps it paused. In the dashboard, every entry has an on/off switch.

### `remove`

```console
$ localdns remove app.local
Remove app.local → 127.0.0.1:3000?

[y/N] y

✓ Removed successfully
```

Skip the question with `--yes` (`-y`): `localdns remove app.local --yes`.

### `doctor`

```text
LocalDNS Doctor

✓ Operating system supported — darwin/arm64
✓ Hosts file found — /etc/hosts
✓ Hosts file readable
✓ Hosts file writable
✓ LocalDNS section valid — 3 entries
✓ Configuration valid — /etc/localdns/config.json
✓ Local UI available — http://127.0.0.1:7357 (localhost only)
✓ Port-free URLs (router) — on: http://<name> forwards to its port (127.0.0.1:80, this machine only)

Everything looks good.
```

When something is wrong, each failing check explains why and prints a `Fix:` line.

---

## For AI agents and scripts

LocalDNS is designed to be driven by Claude Code, Codex, OpenCode, Cursor and any other agent
without a GUI and without interactive prompts.

**Discover:** `localdns info` (or `localdns info --json`) lists every command, flag,
example, exit code and file path. `localdns <command> --help` gives details.

**Machine-readable output:** every command accepts `--json`.

```console
$ localdns list --json
{
  "entries": [
    {
      "hostname": "app.local",
      "ip": "127.0.0.1",
      "port": 3000,
      "address": "127.0.0.1:3000",
      "url": "http://app.local:3000",
      "status": "active"
    },
    {
      "hostname": "ha.local",
      "ip": "192.168.1.60",
      "port": null,
      "address": "192.168.1.60",
      "url": "http://ha.local",
      "status": "active"
    }
  ]
}
```

`status` is `active` (in effect), `missing` (LocalDNS remembers it but the hosts line was
deleted by hand) or `conflict` (an earlier line outside LocalDNS overrides it).

Other JSON outputs: `add --json` and `remove --json` return `{"action": "added" | "updated" |
"unchanged" | "removed", "entry": {…}}`; `status --json`, `doctor --json`, `info --json`,
`uninstall --json` and `ui --json` (prints `{"url": …}` once the server is listening).

**Errors** are JSON too when `--json` is set (printed to stdout, with a non-zero exit code):

```json
{
  "error": {
    "code": "already_exists",
    "message": "app.local already points to 127.0.0.1:3000",
    "hint": "Use --force to replace it: localdns add app.local 127.0.0.1:4000 --force"
  }
}
```

**No prompts, ever, without a terminal.** `remove` and `uninstall` ask for confirmation only
when a person is at a terminal. In scripts and agents they fail immediately with exit code `7`
unless `--yes` is given. `--json` also implies "never prompt".

**Idempotent:** running the same `add` twice is safe (`"action": "unchanged"`).

**Exit codes**

| Code | Meaning                                                              |
| ---- | -------------------------------------------------------------------- |
| 0    | Success                                                              |
| 1    | Unexpected error, or `doctor` found a failing check                  |
| 2    | Invalid usage or input (bad hostname, address or flag)               |
| 3    | Hostname not managed by LocalDNS                                      |
| 4    | Conflict: hostname exists with another address, or is defined outside LocalDNS |
| 5    | Permission denied: administrator rights required                     |
| 6    | Hosts file section or config is invalid (run `localdns doctor`)      |
| 7    | Not confirmed: declined, or `--yes` missing without a terminal       |

**Typical agent session**

```sh
localdns info --json
localdns list --json
localdns add api.local 127.0.0.1:3002 --json
localdns list --json
localdns remove api.local --yes --json
```

**Privileges in automation:** when the hosts file is not writable, LocalDNS re-runs the
command with `sudo -n` if that works without a password, and otherwise exits with code `5`
and a hint (`sudo localdns …`). Set `LOCALDNS_NO_ELEVATE=1` or pass `--no-elevate` to never
attempt `sudo`.

---

## Web UI

```console
$ localdns ui
✓ LocalDNS UI running

Open:
http://localdns.local

Also at http://127.0.0.1:7357 (works even when port-free URLs are off)

Listening on localhost only. Press Ctrl+C to stop.
```

The dashboard opens at a name, `http://localdns.local`, so there is no IP to remember
(`localdns router enable` maps that name to the dashboard; `http://127.0.0.1:7357` always
works too). It lists every entry (hostname, IP, port, URL, status) with **+ Add Host**,
**Edit** and **Delete** buttons, an on/off switch per entry (pause and resume), and a switch for
[port-free URLs](#port-free-urls). Type a name and click an ending (`.localhost`, `.local`,
`.test`, …) to add or swap it; for apps on this computer the dashboard recommends `.localhost`
(see [Allowed hostnames](#allowed-hostnames-and-addresses)). Paste a link as it is
(`http://127.0.0.1:3000/`): the dashboard keeps just the address. Deleting always
asks for confirmation first, and **Terminal guide** opens a complete command guide in English
and Arabic (also on [the website](https://devehab.github.io/Locly-DNS/guide/)). The UI is part
of the binary (no Node.js or Python needed) and listens on `127.0.0.1` only.

`localdns ui` opens the dashboard in your browser automatically (`--no-open` to skip,
`--port 8080` for another port). The installer also opens it right after installing when you
install from your own terminal (set `LOCALDNS_NO_UI=1` to skip).

> **Why does it ask for my password?** Adding or deleting names edits `/etc/hosts`, which
> only an administrator can change. On macOS and Linux, `localdns ui` therefore asks for your
> password (sudo) when it starts. Only the UI server runs with sudo, the browser opens as
> you, and everything stops when you press Ctrl+C. Prefer not to? `localdns ui --no-elevate`
> starts it read-only (you can view, not change). On Windows, run it from a terminal opened
> with **Run as administrator**.

---

## Port-free URLs

The hosts file can map a name to an address, but never to a port. So `http://app.local`
would go to port 80, where your app isn't running. The **LocalDNS router** fills that gap:

```console
$ localdns router enable
Only an administrator can let a program use port 80, so LocalDNS asks for
your password once to install the router service. …
✓ Router enabled: it starts on its own, also after a restart
✓ Port-free URLs are on

Open your names without a port, for example:
  http://app.local   (instead of http://app.local:3000)
```

```text
browser → http://app.local → 127.0.0.1:80 (LocalDNS router) → 127.0.0.1:3000 (your app)
```

- It listens on **`127.0.0.1:80` only**: nothing on your network can reach it.
- It forwards only names you added with LocalDNS that point to `127.0.0.1` and have a port.
  Any other name gets a 404 page; it never touches other traffic, DNS, proxy or firewall
  settings. The app sees the original `Host: app.local`, as if you had opened it directly.
- It **never runs as root**, and it starts on its own:
  - **macOS:** a LaunchDaemon. macOS only lets an administrator open port 80 on
    `127.0.0.1`, so `launchd` opens that socket and hands it to the router, which runs as
    the restricted `nobody` user (from a root-owned copy in
    `/Library/Application Support/LocalDNS`). Enabling it asks for your password once.
  - **Linux:** a systemd service running as you, with only the right to use port 80
    (`CAP_NET_BIND_SERVICE`). Enabling it asks for `sudo` once.
  - **Windows:** a login item for your user; no administrator rights needed.
- `localdns list`, `add` and the web UI show `http://app.local` when it is on, and the
  `http://app.local:3000` form always keeps working.

| Command                   | What it does                                               |
| ------------------------- | ---------------------------------------------------------- |
| `localdns router`         | Is it on? Where is its service file and log?               |
| `localdns router enable`  | Install it to start on its own, and start it now           |
| `localdns router disable` | Stop it and remove it (`uninstall` does too)               |
| `localdns router run`     | Run it in the foreground instead (`--port 8080` to test)   |

`localdns router enable` also names the dashboard: `localdns.local → 127.0.0.1:7357`, so
`localdns ui` opens at `http://localdns.local`. **When you turn the router off** (with
`localdns router disable` or the switch in the dashboard), the dashboard only opens at
`http://127.0.0.1:7357`; that address is where you turn it back on.

If another program already uses port 80 (a local Apache or nginx, for example), the router
can't start; `localdns router` and `localdns doctor` say so. After updating LocalDNS, run
`sudo localdns router enable` again to update the router (the installer does it for you). Names on other machines
(`ha.local → 192.168.1.60:8123`) keep their port.

---

## How it works

```text
                    LocalDNS
                       │
                ┌──────┴──────┐
                │             │
              CLI           Web UI
                │             │
                └──────┬──────┘
                       │
                  Core Library            (no networking code at all)
                       │
              ┌────────┴────────┐
              │                 │
        Hosts Manager      Local Config
              │
       ┌──────┼──────┐
       │      │      │
     macOS  Linux  Windows
```

**The hosts file is the resolution mechanism.** LocalDNS owns exactly one block:

```text
# BEGIN LOCALDNS
127.0.0.1 app.local
127.0.0.1 api.local
192.168.1.60 ha.local
# END LOCALDNS
```

- Lines outside the block (`localhost`, `broadcasthost`, your own entries) are never modified.
  They are preserved byte for byte, including Windows `CRLF` line endings.
- The block is created on the first `add` and removed entirely when the last entry goes,
  leaving the file exactly as it was.
- If a hostname already appears outside the block, `add` refuses (exit `4`) instead of
  competing with your entry.
- If the block was damaged by hand (missing `END`, invalid line…), LocalDNS refuses to write
  and `doctor` explains how to fix it.
- The file is rewritten in place, so its owner, permissions and attributes are kept, and a
  write that fails part-way is rolled back. A copy of the previous content is saved as
  `hosts.backup` in the config directory before every change.

| OS            | Hosts file                                   | LocalDNS config                         |
| ------------- | -------------------------------------------- | --------------------------------------- |
| macOS / Linux | `/etc/hosts`                                 | `/etc/localdns/config.json`             |
| Windows       | `C:\Windows\System32\drivers\etc\hosts`      | `C:\ProgramData\LocalDNS\config.json`   |

The config only holds metadata (ports, timestamps). The hosts file stays the source of truth.
For testing, both locations can be overridden with `--hosts-file` / `LOCALDNS_HOSTS_FILE` and
`--config-dir` / `LOCALDNS_CONFIG_DIR`.

### Allowed hostnames and addresses

To guarantee LocalDNS can never take over an internet domain, hostnames must end in a suffix
reserved for local or private use:

`.local` · `.localhost` · `.test` · `.example` · `.internal` · `.home.arpa` · `.lan` · `.localdomain`

**For web apps on this computer, prefer `.localhost`.** Browsers enable some features (many
sign-in flows, `crypto.subtle`, the clipboard, the camera) only on `https://` or `localhost` pages
(a *secure context*), and they treat any `*.localhost` name like `localhost`. So
`http://app.localhost` behaves exactly like `http://localhost:3000`, while parts of an app can
fail at `http://app.local`. A `.localhost` name must point to this computer (`127.0.0.1`, `::1`):
browsers send it there themselves, whatever the hosts file says, so LocalDNS refuses
`nas.localhost → 192.168.1.20`; use `.local` or `.lan` for other devices.

`localdns add google.com 127.0.0.1` is refused. Addresses must be local: loopback
(`127.0.0.0/8`, `::1`), private networks (`10/8`, `172.16/12`, `192.168/16`, `fc00::/7`),
link-local (`169.254/16`, `fe80::/10`) or CGNAT/VPN overlay space (`100.64/10`, e.g.
Tailscale). Ports are optional (1–65535).

---

## Security

LocalDNS manages local hostname mappings on the machine it is installed on. Nothing more.

**Guarantees, from the first release:**

- ❌ No DNS server. LocalDNS never answers DNS queries.
- ❌ No process running as root in the background. Only the one command that edits the hosts
  file is elevated, and it exits immediately.
- ❌ No network listener except two that bind to `127.0.0.1` only: the web UI, while
  `localdns ui` runs, and the optional [port-free router](#port-free-urls) on port 80, which
  never runs as root (`localdns router disable` removes it).
- ❌ No outbound network requests from the core. No telemetry. No analytics. No data
  leaves your machine.
- ❌ No changes to your network router, DNS settings, VPN, proxy or firewall. No traffic
  interception: the port-free router only answers requests for names you added.
- ❌ No changes outside the `# BEGIN LOCALDNS` / `# END LOCALDNS` block of the hosts file
  (plus LocalDNS's own config directory).
- ❌ No mapping of public internet domains, so names like `google.com` always go through your
  normal DNS.

**How these are enforced in code and tests:**

- The core packages (`internal/core`, `hosts`, `config`, `validate`, …) are checked by an
  architecture test that fails if they ever import `net`, `net/http`, `crypto/tls` or
  `os/exec` on any OS.
- An integration test audits the whole source tree: the only network listeners allowed are
  the UI's and the router's, pinned to `127.0.0.1`; a socket handed over by the service
  manager is refused unless it is bound to loopback.
- CI installs the real router service on macOS, Linux and Windows, opens a name through it
  without a port, checks the router process is not root, and removes it again.
- Tests prove that existing hosts entries survive `add`, `remove`, update and uninstall byte
  for byte, and that unknown domains (`google.com`, `example.com`, …) never appear in the
  hosts file.

**Web UI hardening:** the UI defends against malicious websites in your browser.
It rejects requests whose `Host` header is not `127.0.0.1:<port>` or `localhost:<port>`
(DNS rebinding), and every API call needs a random per-session token embedded in the page
(CSRF). Cross-origin requests are refused and no CORS headers are sent. A strict
Content-Security-Policy allows no inline or third-party scripts.

**Installer:** downloads only from GitHub Releases over HTTPS and verifies SHA-256 checksums
before installing.

Report vulnerabilities privately; see [SECURITY.md](SECURITY.md).

---

## Uninstall

```console
$ localdns uninstall
Uninstall LocalDNS?

This will:

• Remove LocalDNS-managed host entries
• Remove LocalDNS configuration
• Remove the LocalDNS router (port-free URLs)
• Remove the LocalDNS application

Your other hosts entries will not be modified.

Continue?

[y/N] y

✓ LocalDNS entries removed (3 entries)
✓ Configuration removed
✓ Router removed
✓ LocalDNS uninstalled
```

Use `localdns uninstall --yes` in scripts, or `--keep-binary` to keep the executable. On
Windows the installer's `PATH` entry is removed too.

---

## Troubleshooting

Start with `localdns doctor`; it checks everything and prints a fix for each problem.

- **The app opens at its name, but sign-in, uploads or other features fail** (for example the
  browser console shows `Cannot read properties of undefined (reading 'digest')`): the page is
  not a secure context. Rename the entry to the `.localhost` ending, which browsers treat like
  `localhost`: `localdns edit app.local --name app.localhost`. If sign-in still fails, add
  `http://app.localhost` to the app's allowed sign-in / callback URLs.

- **"needs administrator rights"**: run the command with `sudo` (macOS/Linux) or from an
  administrator terminal (Windows). Interactive use on macOS/Linux asks for `sudo`
  automatically.
- **A name doesn't resolve right away**: some systems cache lookups. On macOS:
  `sudo dscacheutil -flushcache; sudo killall -HUP mDNSResponder`. On Windows:
  `ipconfig /flushdns`. Browsers may also cache; reopen the tab.
- **`.local` names are slow on macOS**: `.local` is also used by Bonjour (mDNS), and some
  lookups wait for it. Prefer `.test` or `.localhost` names on macOS if you notice delays.
- **`http://app.local` (without the port) doesn't open**: run `localdns router`. If it is off,
  `localdns router enable`. If it is on but not answering, another program probably uses
  port 80; `localdns router run` shows the exact error. The name must point to `127.0.0.1`
  and have a port (`localdns add app.local 127.0.0.1:3000`).
- **`conflict` status**: another line in your hosts file, above the LocalDNS block, maps the
  same name. Remove or rename that line yourself.
- **`missing` status**: the hosts line was deleted by hand. Restore it with
  `localdns add <name> <address> --force`, or forget it with `localdns remove <name>`.
- **Recover the previous hosts file**: LocalDNS saves it as `hosts.backup` in its config
  directory before every change.

---

## Development

```sh
make test          # unit + integration + CLI tests (all on temporary hosts files)
make lint          # gofmt, go vet, golangci-lint
make build         # ./bin/localdns
make release VERSION=0.1.0   # archives + checksums.txt in ./dist
```

The project uses only the Go standard library: no third-party dependencies.

```text
cmd/localdns/          entry point
internal/core/         business logic shared by the CLI and UI (no networking)
internal/hosts/        hosts file parsing/rendering; HostsFile abstraction
internal/hosts/hoststest/  TemporaryHostsFile for tests
internal/config/       port metadata store
internal/validate/     hostname/address parsing and the local-only policy
internal/cli/          command-line interface
internal/ui/           loopback-only web server + embedded static UI
internal/elevate/      one-shot sudo re-execution
test/integration/      CLI + UI + core together; security audits
test/cli/              black-box tests of the built binary
tools/release/         cross-compile and package release archives
tools/motion/          render the landing-page story animation to MP4
scripts/smoke-test.*   post-install end-to-end check (used by CI)
docs/                  bilingual (English/Arabic) landing page for GitHub Pages
```

**Tests never touch the real hosts file.** They use `hoststest.TemporaryHostsFile`
(`/tmp/…/hosts`) and temporary config directories, locally and in CI.

**CI** (GitHub Actions) runs on every pull request:

- Format, vet and lint for Linux, macOS and Windows builds
- Unit, integration and CLI tests on Linux, macOS and Windows, on both amd64 and arm64
- Cross-compiled release archives for all six platforms
- The one-command install, for real: `curl … | sh` (and `irm … | iex` on Windows) against the
  freshly built archives, then `info` → `add` → `list --json` → `remove --yes` →
  `uninstall --yes`, verifying every step

**Website:** `docs/` is a static, dependency-free landing page in English and Arabic
(language switch with full right-to-left layout, light/dark themes, self-hosted fonts, no
trackers), including a ~35-second story animation ("Remember names, not numbers") that
`tools/motion/` can export to MP4 for social media. To publish it, open **Settings → Pages**, choose **Deploy from a branch**, and
select `main` and the `/docs` folder. Preview it locally with
`go run ./tools/serve -dir docs -port 8080` and open `http://127.0.0.1:8080`.

**Releasing:** push a tag like `v0.1.0`. The release workflow tests, builds the archives and
`checksums.txt`, publishes the GitHub release (with `install.sh` and `install.ps1` attached),
then installs from the public URL on every platform to verify it.
