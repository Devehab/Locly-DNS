#!/usr/bin/env bash
# End-to-end check of an installed `localdns` binary: the exact steps an AI
# agent or a person runs after the one-line install. It works on a temporary
# hosts file, so the machine's real hosts file is never touched.
set -euo pipefail

work="$(mktemp -d)"
export LOCALDNS_HOSTS_FILE="$work/hosts"
export LOCALDNS_CONFIG_DIR="$work/config"
export NO_COLOR=1
printf '127.0.0.1\tlocalhost\n::1\tlocalhost\n10.0.0.5\tnas.lan\n' >"$LOCALDNS_HOSTS_FILE"
cp "$LOCALDNS_HOSTS_FILE" "$work/hosts.orig"

step() { printf '\n==> %s\n' "$*"; }
fail() {
	printf '\nFAIL: %s\n' "$*" >&2
	exit 1
}
expect() { # file pattern
	grep -qF -- "$2" "$1" || fail "expected '$2' in $1:
$(cat "$1")"
}

hash -r
bin="$(command -v localdns)" || fail "localdns is not on PATH"
step "localdns at $bin"

step "localdns info"
localdns info | tee "$work/info.txt"
expect "$work/info.txt" "COMMANDS"

step "localdns add test.local 127.0.0.1:3000"
localdns add test.local 127.0.0.1:3000 | tee "$work/add.txt"
expect "$work/add.txt" "Added successfully"
expect "$work/add.txt" "http://test.local:3000"
expect "$LOCALDNS_HOSTS_FILE" "127.0.0.1 test.local"

step "localdns list --json"
localdns list --json | tee "$work/list.json"
expect "$work/list.json" '"hostname": "test.local"'
expect "$work/list.json" '"port": 3000'
expect "$work/list.json" '"url": "http://test.local:3000"'
expect "$work/list.json" '"status": "active"'

step "localdns status --json"
localdns status --json | tee "$work/status.json"
expect "$work/status.json" '"healthy": true'

step "localdns doctor"
localdns doctor

step "localdns remove test.local without --yes (must not prompt)"
set +e
localdns remove test.local </dev/null
code=$?
set -e
[ "$code" = 7 ] || fail "expected exit code 7, got $code"

step "localdns remove test.local --yes"
localdns remove test.local --yes
localdns list --json | tee "$work/list2.json"
expect "$work/list2.json" '"entries": []'
cmp "$LOCALDNS_HOSTS_FILE" "$work/hosts.orig" || fail "hosts file not restored after remove"

step "localdns uninstall --yes"
localdns add keep.local 192.168.1.60
localdns uninstall --yes
cmp "$LOCALDNS_HOSTS_FILE" "$work/hosts.orig" || fail "uninstall changed entries it does not own"
[ ! -e "$LOCALDNS_CONFIG_DIR" ] || fail "config directory still exists"
[ ! -e "$bin" ] || fail "binary still exists at $bin"
hash -r
if command -v localdns >/dev/null 2>&1; then fail "localdns still on PATH"; fi

rm -rf "$work"
printf '\n✓ Smoke test passed\n'
