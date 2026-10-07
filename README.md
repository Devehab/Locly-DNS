# Locly — `localdns`

**A secure, local-only hostname manager for developers and AI agents.**

🌐 **Website (English / العربية):** <https://devehab.github.io/Locly-DNS/>

Give the services running on your machine (or on your local network) friendly names:

```text
http://127.0.0.1:3000       →  http://app.local:3000
http://127.0.0.1:3002       →  http://api.local:3002
http://192.168.1.60:8123    →  http://ha.local:8123
```

```console
$ localdns add app.local 127.0.0.1:3000
✓ Added successfully

app.local → 127.0.0.1:3000

URL:
http://app.local:3000
```

Locly is **not a DNS server**. It writes a clearly marked section of your operating system's
`hosts` file and nothing else. Internet domains keep resolving through your normal DNS,
exactly as before.

- One small binary for macOS, Linux and Windows. No runtime, no daemon, no dependencies.
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

It changes nothing else: no services, no shell profile edits, no DNS settings.

Installer options (environment variables):

| Variable               | Meaning                                                |
| ---------------------- | ------------------------------------------------------ |
| `LOCALDNS_VERSION`     | Tag to install, e.g. `v0.1.0` (default: latest)        |
| `LOCALDNS_INSTALL_DIR` | Where to put the binary                                |
| `LOCALDNS_BASE_URL`    | Download from a mirror instead of GitHub Releases      |

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

HOSTNAME     ADDRESS           STATUS
─────────────────────────────────────
app.local    127.0.0.1:3000    ✓
api.local    127.0.0.1:3002    ✓
ha.local     192.168.1.60      ✓

3 entries
```

Open `http://app.local:3000` in your browser. Done.

> Editing the hosts file needs administrator rights. On macOS and Linux, `localdns` asks for
> them with `sudo` **for that one command only**. On Windows, run it from a terminal opened
> with **Run as administrator**. Read-only commands (`list`, `status`, `info`, `doctor`) never
> need elevation.

---

## Commands

| Command                              | What it does                                         |
| ------------------------------------ | ---------------------------------------------------- |
| `localdns add <hostname> <ip[:port]>` | Add a hostname (idempotent; `--force` to replace)   |
| `localdns list`                      | List hostnames with address and status              |
| `localdns remove <hostname>`         | Remove a hostname (asks first; `--yes` to skip)     |
| `localdns status`                    | Show file locations, write access and entry health  |
| `localdns doctor`                    | Diagnose problems and explain how to fix them       |
| `localdns ui`                        | Start the web interface on `http://127.0.0.1:7357`  |
| `localdns uninstall`                 | Remove LocalDNS entries, config and the binary      |
| `localdns info`                      | Overview of commands, flags, exit codes and files   |

Every command has detailed help: `localdns add --help`, `localdns remove --help`, …

### `add`

```sh
localdns add app.local 127.0.0.1:3000      # hostname + IP + port
localdns add ha.local 192.168.1.60         # hostname + IP (no port)
localdns add v6.local [::1]:8080           # IPv6
localdns add app.local 127.0.0.1:4000 --force   # change an existing entry
```

The hosts file cannot store ports, so `127.0.0.1:3000` becomes the hosts line
`127.0.0.1 app.local`, and LocalDNS remembers the port in its own config to show the full URL
in `list`, `--json` output and the UI.

Adding an identical entry again succeeds without changing anything. Adding the same hostname
with a *different* address fails with exit code `4` unless you pass `--force`.

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

✓ Operating system supported
✓ Hosts file found
✓ Hosts file readable
✓ Hosts file writable
✓ LocalDNS section valid
✓ Configuration valid
✓ Local UI available

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
http://127.0.0.1:7357

Listening on localhost only. Press Ctrl+C to stop.
```

The dashboard lists every entry (hostname, IP, port, URL, status) with **+ Add Host** and
**Delete** buttons. Deleting always asks for confirmation first. The UI is part of the
binary (no Node.js or Python needed) and listens on `127.0.0.1` only.

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

`localdns add google.com 127.0.0.1` is refused. Addresses must be local: loopback
(`127.0.0.0/8`, `::1`), private networks (`10/8`, `172.16/12`, `192.168/16`, `fc00::/7`),
link-local (`169.254/16`, `fe80::/10`) or CGNAT/VPN overlay space (`100.64/10`, e.g.
Tailscale). Ports are optional (1–65535).

---

## Security

LocalDNS manages local hostname mappings on the machine it is installed on. Nothing more.

**Guarantees, from the first release:**

- ❌ No DNS server. LocalDNS never answers DNS queries.
- ❌ No daemon or background service, and no process running as root. Only the one command
  that edits the hosts file is elevated, and it exits immediately.
- ❌ No network listener except the web UI, which binds to `127.0.0.1` only and only while
  `localdns ui` is running.
- ❌ No outbound network requests from the core. No telemetry. No analytics. No data
  leaves your machine.
- ❌ No changes to your router, DNS settings, VPN, proxy or firewall. No traffic interception.
- ❌ No changes outside the `# BEGIN LOCALDNS` / `# END LOCALDNS` block of the hosts file
  (plus LocalDNS's own config directory).
- ❌ No mapping of public internet domains, so names like `google.com` always go through your
  normal DNS.

**How these are enforced in code and tests:**

- The core packages (`internal/core`, `hosts`, `config`, `validate`, …) are checked by an
  architecture test that fails if they ever import `net`, `net/http`, `crypto/tls` or
  `os/exec` on any OS.
- An integration test audits the whole source tree: the only network listener allowed is the
  UI's `net.Listen` on `127.0.0.1`.
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
• Remove the LocalDNS application

Your other hosts entries will not be modified.

Continue?

[y/N] y

✓ LocalDNS entries removed (3 entries)
✓ Configuration removed
✓ LocalDNS uninstalled
```

Use `localdns uninstall --yes` in scripts, or `--keep-binary` to keep the executable. On
Windows the installer's `PATH` entry is removed too.

---

## Troubleshooting

Start with `localdns doctor`; it checks everything and prints a fix for each problem.

- **"needs administrator rights"**: run the command with `sudo` (macOS/Linux) or from an
  administrator terminal (Windows). Interactive use on macOS/Linux asks for `sudo`
  automatically.
- **A name doesn't resolve right away**: some systems cache lookups. On macOS:
  `sudo dscacheutil -flushcache; sudo killall -HUP mDNSResponder`. On Windows:
  `ipconfig /flushdns`. Browsers may also cache; reopen the tab.
- **`.local` names are slow on macOS**: `.local` is also used by Bonjour (mDNS), and some
  lookups wait for it. Prefer `.test` or `.localhost` names on macOS if you notice delays.
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
