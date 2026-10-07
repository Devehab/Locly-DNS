#!/bin/sh
# LocalDNS installer for macOS and Linux.
#
#   curl -fsSL https://github.com/Devehab/Locly-DNS/releases/latest/download/install.sh | sh
#
# What it does: downloads the localdns binary for your platform from GitHub
# Releases, verifies its SHA-256 checksum, and copies it to /usr/local/bin
# (using sudo if needed) or ~/.local/bin. Nothing else on your system is
# changed: no services, no shell profile edits, no DNS settings.
#
# Environment variables:
#   LOCALDNS_VERSION      version tag to install, e.g. v0.1.0 (default: latest)
#   LOCALDNS_INSTALL_DIR  install directory (default: /usr/local/bin or ~/.local/bin)
#   LOCALDNS_REPO         GitHub repository (default: Devehab/Locly-DNS)
#   LOCALDNS_BASE_URL     download from this URL instead of GitHub Releases
set -eu

REPO="${LOCALDNS_REPO:-Devehab/Locly-DNS}"
VERSION="${LOCALDNS_VERSION:-latest}"
BASE_URL="${LOCALDNS_BASE_URL:-}"
INSTALL_DIR="${LOCALDNS_INSTALL_DIR:-}"

say() { printf '%s\n' "$*"; }
fail() {
	printf 'localdns install: %s\n' "$*" >&2
	exit 1
}
have() { command -v "$1" >/dev/null 2>&1; }

detect_platform() {
	case "$(uname -s)" in
	Linux) OS=linux ;;
	Darwin) OS=darwin ;;
	MINGW* | MSYS* | CYGWIN*) fail "on Windows, use PowerShell: irm https://github.com/$REPO/releases/latest/download/install.ps1 | iex" ;;
	*) fail "unsupported operating system: $(uname -s) (LocalDNS supports macOS, Linux and Windows)" ;;
	esac
	case "$(uname -m)" in
	x86_64 | amd64) ARCH=amd64 ;;
	arm64 | aarch64) ARCH=arm64 ;;
	*) fail "unsupported CPU architecture: $(uname -m)" ;;
	esac
	# An Intel shell under Rosetta on Apple Silicon should still get arm64.
	if [ "$OS" = darwin ] && [ "$ARCH" = amd64 ] && [ "$(sysctl -n sysctl.proc_translated 2>/dev/null || echo 0)" = 1 ]; then
		ARCH=arm64
	fi
}

download() { # url dest
	if have curl; then
		if [ -z "$BASE_URL" ]; then
			# Official downloads: HTTPS only.
			curl -fsSL --proto '=https' --tlsv1.2 -o "$2" "$1"
		else
			curl -fsSL -o "$2" "$1"
		fi
	elif have wget; then
		wget -q -O "$2" "$1"
	else
		fail "curl or wget is required"
	fi
}

sha256() {
	if have sha256sum; then
		sha256sum "$1" | awk '{print $1}'
	elif have shasum; then
		shasum -a 256 "$1" | awk '{print $1}'
	elif have openssl; then
		openssl dgst -sha256 "$1" | awk '{print $NF}'
	else
		fail "sha256sum, shasum or openssl is required to verify the download"
	fi
}

can_sudo() {
	have sudo || return 1
	sudo -n true 2>/dev/null && return 0
	# Interactive installs can prompt for a password via the terminal.
	[ -t 1 ] && (: </dev/tty) 2>/dev/null
}

choose_install_dir() {
	SUDO=""
	if [ -n "$INSTALL_DIR" ]; then
		mkdir -p "$INSTALL_DIR" 2>/dev/null || true
		[ -w "$INSTALL_DIR" ] || { can_sudo && SUDO=sudo; } || fail "cannot write to $INSTALL_DIR"
		return
	fi
	if [ -d /usr/local/bin ] && [ -w /usr/local/bin ]; then
		INSTALL_DIR=/usr/local/bin
	elif can_sudo; then
		INSTALL_DIR=/usr/local/bin
		SUDO=sudo
	else
		INSTALL_DIR="$HOME/.local/bin"
		mkdir -p "$INSTALL_DIR"
	fi
}

main() {
	detect_platform
	asset="localdns_${OS}_${ARCH}.tar.gz"
	if [ -n "$BASE_URL" ]; then
		base="${BASE_URL%/}"
	elif [ "$VERSION" = latest ]; then
		base="https://github.com/$REPO/releases/latest/download"
	else
		base="https://github.com/$REPO/releases/download/$VERSION"
	fi

	tmp="$(mktemp -d 2>/dev/null || mktemp -d -t localdns)"
	trap 'rm -rf "$tmp"' EXIT INT TERM

	say "Downloading LocalDNS ($OS/$ARCH)..."
	download "$base/$asset" "$tmp/$asset" || fail "download failed: $base/$asset"
	download "$base/checksums.txt" "$tmp/checksums.txt" || fail "download failed: $base/checksums.txt"

	expected="$(awk -v f="$asset" '$2 == f || $2 == "*" f {print $1}' "$tmp/checksums.txt")"
	[ -n "$expected" ] || fail "no checksum for $asset in checksums.txt"
	actual="$(sha256 "$tmp/$asset")"
	[ "$expected" = "$actual" ] || fail "checksum mismatch for $asset (expected $expected, got $actual)"
	say "Checksum verified."

	tar -xzf "$tmp/$asset" -C "$tmp" localdns || fail "could not extract $asset"

	choose_install_dir
	if [ -n "$SUDO" ]; then
		say "Installing to $INSTALL_DIR (requires sudo)..."
	fi
	$SUDO mkdir -p "$INSTALL_DIR"
	$SUDO cp "$tmp/localdns" "$INSTALL_DIR/localdns.tmp.$$"
	$SUDO chmod 0755 "$INSTALL_DIR/localdns.tmp.$$"
	$SUDO mv -f "$INSTALL_DIR/localdns.tmp.$$" "$INSTALL_DIR/localdns"

	installed="$("$INSTALL_DIR/localdns" version 2>/dev/null)" || fail "installed binary does not run: $INSTALL_DIR/localdns"

	say ""
	say "✓ $installed installed to $INSTALL_DIR/localdns"
	case ":$PATH:" in
	*":$INSTALL_DIR:"*) ;;
	*)
		say ""
		say "$INSTALL_DIR is not in your PATH. Add it with:"
		say "  export PATH=\"$INSTALL_DIR:\$PATH\""
		;;
	esac
	say ""
	say "Get started:"
	say "  localdns info"
	say "  localdns add app.local 127.0.0.1:3000"
	say "  localdns ui"
}

main "$@"
