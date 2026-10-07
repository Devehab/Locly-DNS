# Security Policy

## Reporting a vulnerability

Please report security issues privately through
[GitHub Security Advisories](https://github.com/Devehab/Locly-DNS/security/advisories/new)
rather than in public issues. Include the version (`localdns version`), your operating
system, and steps to reproduce.

## Scope

LocalDNS edits one section of the operating system's hosts file, serves a web UI on
`127.0.0.1` while `localdns ui` runs, and, when enabled, runs a router on `127.0.0.1:80` that
forwards the names it manages to their ports. Issues we especially want to hear about:

- Any way to make LocalDNS modify hosts file lines outside its `# BEGIN LOCALDNS` /
  `# END LOCALDNS` section, or files other than the hosts file and its own config.
- Any way to map a public internet domain or a non-local address.
- Any way for a website or another local user to drive the web UI (DNS rebinding, CSRF,
  token leaks, cross-origin access).
- Any network listener other than the loopback UI and loopback router, any way to make the
  router forward to a name or address LocalDNS does not manage, or any outbound network
  traffic from the core.
- Privilege escalation through the `sudo` re-execution, the installer, or the router service
  (the macOS LaunchDaemon must only ever run the router as `nobody`, from a root-owned copy).

## Design guarantees

See the [Security section of the README](README.md#security) for what LocalDNS will never do
and how the test suite enforces it.
