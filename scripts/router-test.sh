#!/usr/bin/env bash
# Installs the real port-free router service on this machine (launchd on
# macOS, systemd on Linux), opens a name through it without typing a port,
# checks it never runs as root, then removes it with `router disable` and
# with `uninstall`. Meant for CI runners: it needs sudo without a password.
# The real hosts file is never touched; entries live in a sandbox.
#
#   scripts/router-test.sh <path-to-localdns> <sandbox-dir>
#
# The sandbox must be readable by the service user ("nobody" on macOS).
set -euo pipefail

BIN=${1:?usage: router-test.sh <path-to-localdns> <sandbox-dir>}
SANDBOX=${2:?usage: router-test.sh <path-to-localdns> <sandbox-dir>}
APP_PORT=3457
MARKER="hello through the LocalDNS router"

step() { printf '\n== %s\n' "$*"; }
fail() {
	printf 'FAIL: %s\n' "$*" >&2
	"$BIN" router || true
	if [ "$(uname -s)" = Darwin ]; then
		sudo launchctl print system/dev.locly.router 2>&1 | tail -40 || true
		sudo tail -20 /Library/Logs/localdns-router.log 2>/dev/null || true
	else
		sudo systemctl status --no-pager localdns-router.service || true
		sudo journalctl -u localdns-router.service --no-pager -n 40 || true
	fi
	exit 1
}

# get <host> prints the body of http://<host>/ fetched through 127.0.0.1:80.
get() { curl -fsS -m 3 -H "Host: $1" http://127.0.0.1/; }

answering() { curl -s -o /dev/null -m 2 http://127.0.0.1/__localdns/router; }

# A name must work right after `router enable`: people try it immediately.
wait_for_router() {
	local start=$SECONDS
	while [ $((SECONDS - start)) -lt 10 ]; do
		if body=$(get app.test 2>/dev/null) && [ "$body" = "$MARKER" ]; then
			echo "✓ http://app.test → $body (after $((SECONDS - start))s)"
			return 0
		fi
		sleep 0.2
	done
	fail "http://app.test (no port) did not reach the app within 10s"
}

rm -rf "$SANDBOX"
mkdir -p "$SANDBOX/www"
chmod 755 "$SANDBOX" "$SANDBOX/www"
printf '127.0.0.1 localhost\n' >"$SANDBOX/hosts"
printf '%s\n' "$MARKER" >"$SANDBOX/www/index.html"
chmod 644 "$SANDBOX/hosts" "$SANDBOX/www/index.html"
paths=("--hosts-file=$SANDBOX/hosts" "--config-dir=$SANDBOX/cfg")

if answering; then
	fail "something already answers on 127.0.0.1:80"
fi

step "start a test app on 127.0.0.1:$APP_PORT"
python3 -m http.server "$APP_PORT" --bind 127.0.0.1 --directory "$SANDBOX/www" >/dev/null 2>&1 &
app=$!
trap 'kill "$app" 2>/dev/null || true; sudo "$BIN" router disable >/dev/null 2>&1 || true' EXIT
# Python's server can be slow to start (it looks up its own host name), so
# wait for it before timing the router.
started=$SECONDS
until curl -fsS -m 2 -o /dev/null "http://127.0.0.1:$APP_PORT/"; do
	[ $((SECONDS - started)) -lt 120 ] || fail "the test app did not start"
	sleep 0.5
done
echo "test app ready after $((SECONDS - started))s"

step "add app.test → 127.0.0.1:$APP_PORT"
"$BIN" add app.test "127.0.0.1:$APP_PORT" "${paths[@]}" --no-elevate

if [ "$(uname -s)" = Darwin ]; then
	step "an outdated 0.2.0 LaunchAgent is reported and replaced"
	legacy="$HOME/Library/LaunchAgents/dev.locly.router.plist"
	mkdir -p "$(dirname "$legacy")"
	printf '<?xml version="1.0" encoding="UTF-8"?>\n<plist version="1.0"><dict><key>Label</key><string>dev.locly.router</string></dict></plist>\n' >"$legacy"
	"$BIN" router | tee "$SANDBOX/status.txt"
	grep -q "sudo localdns router enable" "$SANDBOX/status.txt" || fail "outdated router not reported"
fi

step "sudo localdns router enable"
sudo "$BIN" router enable "${paths[@]}"
wait_for_router

if [ "$(uname -s)" = Darwin ] && [ -e "$legacy" ]; then
	fail "the 0.2.0 LaunchAgent was not removed"
fi

step "the router never runs as root"
pids=$(pgrep -f 'localdns router run') || fail "router process not found"
for pid in $pids; do
	owner=$(ps -o user= -p "$pid" | tr -d ' ')
	echo "pid $pid runs as $owner"
	[ "$owner" != root ] || fail "the router runs as root"
done

step "unknown names are not forwarded"
code=$(curl -s -o /dev/null -w '%{http_code}' -m 3 -H "Host: other.test" http://127.0.0.1/)
[ "$code" = 404 ] || fail "other.test answered $code, want 404"

step "list shows the port-free URL"
"$BIN" list --json "${paths[@]}" >"$SANDBOX/list.json"
python3 -I -c 'import json,sys; e=json.load(open(sys.argv[1]))["entries"][0]; assert e["short_url"]=="http://app.test", e; print("✓ short_url", e["short_url"])' "$SANDBOX/list.json" ||
	fail "list --json has no short_url"
"$BIN" router

step "a paused name is not forwarded, and resume brings it back"
"$BIN" pause app.test "${paths[@]}" --no-elevate
sleep 1.5 # the router re-reads entries at most once a second
code=$(curl -s -o /dev/null -w '%{http_code}' -m 3 -H "Host: app.test" http://127.0.0.1/)
[ "$code" = 404 ] || fail "paused app.test answered $code, want 404"
"$BIN" resume app.test "${paths[@]}" --no-elevate
wait_for_router

step "the dashboard opens by name: http://localdns.local"
code=$(curl -s -o /dev/null -w '%{http_code}' -m 3 -H "Host: localdns.local" http://127.0.0.1/)
[ "$code" = 502 ] || fail "localdns.local without a running dashboard answered $code, want 502"
# The log belongs to this user, so it is opened before sudo on purpose.
# shellcheck disable=SC2024
sudo "$BIN" ui --no-open "${paths[@]}" >"$SANDBOX/ui.log" 2>&1 &
ui=$!
for _ in $(seq 1 50); do
	curl -fsS -m 2 -o /dev/null http://127.0.0.1:7357/healthz && break
	sleep 0.2
done
page=$(curl -fsS -m 3 -H "Host: localdns.local" http://127.0.0.1/) || fail "http://localdns.local did not reach the dashboard"
grep -q "<title>LocalDNS</title>" <<<"$page" || fail "unexpected page at http://localdns.local"
grep -q "http://localdns.local" "$SANDBOX/ui.log" || fail "localdns ui did not print http://localdns.local"
echo "✓ http://localdns.local → dashboard"
sudo kill "$ui" 2>/dev/null || true

step "sudo localdns router disable"
sudo "$BIN" router disable
sleep 1
if answering; then
	fail "the router still answers after disable"
fi
if [ "$(uname -s)" = Darwin ]; then
	[ ! -e /Library/LaunchDaemons/dev.locly.router.plist ] || fail "plist left behind"
	[ ! -e "/Library/Application Support/LocalDNS" ] || fail "support folder left behind"
else
	[ ! -e /etc/systemd/system/localdns-router.service ] || fail "unit file left behind"
fi
echo "✓ removed"

step "uninstall removes the router too"
sudo "$BIN" router enable "${paths[@]}"
wait_for_router
sudo "$BIN" uninstall --yes --keep-binary "${paths[@]}"
sleep 1
if answering; then
	fail "the router still answers after uninstall"
fi
echo "✓ the router service works end to end"
