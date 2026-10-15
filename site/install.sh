#!/bin/sh
# Installs skilus on macOS or Linux from its GitHub releases.
#
#   curl -fsSL https://skilus.dev/install | sh
#
# Environment variables:
#   SKILUS_VERSION      version to install, such as 1.0.0 (default: the latest release)
#   SKILUS_INSTALL_DIR  folder for the binary (default: /usr/local/bin when it
#                       is writable, otherwise ~/.local/bin)
#
# The archive is checked against the release's checksums.txt before anything
# is installed. On Windows, use https://skilus.dev/install.ps1.
set -eu

REPO=colybri/skilus

fail() {
	echo "skilus install: $*" >&2
	exit 1
}

# fetch URL FILE downloads URL into FILE with curl or wget.
fetch() {
	if command -v curl >/dev/null 2>&1; then
		curl -fsSL --proto '=https' --tlsv1.2 -o "$2" "$1"
	elif command -v wget >/dev/null 2>&1; then
		wget -q -O "$2" "$1"
	else
		fail "curl or wget is needed"
	fi
}

# latest prints the newest release version: /releases/latest redirects to
# /releases/tag/vX.Y.Z.
latest() {
	url=https://github.com/$REPO/releases/latest
	if command -v curl >/dev/null 2>&1; then
		final=$(curl -fsSLI -o /dev/null -w '%{url_effective}' "$url")
	else
		final=$(wget -S --spider "$url" 2>&1 | sed -n 's/^ *[Ll]ocation: *\([^ ]*\).*/\1/p' | tail -n 1 | tr -d '\r')
	fi
	tag=${final##*/}
	case "$tag" in
	v[0-9]*) echo "${tag#v}" ;;
	*) fail "could not find the latest release at $url" ;;
	esac
}

sha256() {
	if command -v sha256sum >/dev/null 2>&1; then
		sha256sum "$1" | cut -d ' ' -f 1
	elif command -v shasum >/dev/null 2>&1; then
		shasum -a 256 "$1" | cut -d ' ' -f 1
	else
		fail "sha256sum or shasum is needed to check the download"
	fi
}

main() {
	case $(uname -s) in
	Linux) os=linux ;;
	Darwin) os=darwin ;;
	*) fail "unsupported system $(uname -s); on Windows, use https://skilus.dev/install.ps1" ;;
	esac
	case $(uname -m) in
	x86_64 | amd64) arch=amd64 ;;
	aarch64 | arm64) arch=arm64 ;;
	*) fail "unsupported architecture $(uname -m)" ;;
	esac
	# An Apple Silicon Mac running this shell under Rosetta still gets the native binary.
	if [ "$os" = darwin ] && [ "$arch" = amd64 ] && [ "$(sysctl -n sysctl.proc_translated 2>/dev/null || true)" = 1 ]; then
		arch=arm64
	fi

	version=${SKILUS_VERSION:-}
	version=${version#v}
	[ -n "$version" ] || version=$(latest)

	dir=${SKILUS_INSTALL_DIR:-}
	if [ -z "$dir" ]; then
		if [ -d /usr/local/bin ] && [ -w /usr/local/bin ]; then
			dir=/usr/local/bin
		else
			dir=$HOME/.local/bin
		fi
	fi

	file=skilus_${version}_${os}_${arch}.tar.gz
	base=https://github.com/$REPO/releases/download/v$version
	tmp=$(mktemp -d)
	trap 'rm -rf "$tmp"' EXIT

	echo "Downloading skilus $version for $os/$arch"
	fetch "$base/$file" "$tmp/$file"
	fetch "$base/checksums.txt" "$tmp/checksums.txt"

	want=$(awk -v f="$file" '$2 == f { print $1 }' "$tmp/checksums.txt")
	[ -n "$want" ] || fail "$file is not listed in checksums.txt"
	got=$(sha256 "$tmp/$file")
	[ "$want" = "$got" ] || fail "checksum mismatch for $file: expected $want, got $got"

	tar -xzf "$tmp/$file" -C "$tmp" skilus
	mkdir -p "$dir"
	# Copy next to the target and rename, so a running skilus is never half-written.
	cp "$tmp/skilus" "$dir/.skilus.new"
	chmod 0755 "$dir/.skilus.new"
	mv -f "$dir/.skilus.new" "$dir/skilus"

	echo "Installed $("$dir/skilus" --version) in $dir/skilus"
	case ":$PATH:" in
	*":$dir:"*) echo "Run 'skilus agents' to see the agents it found." ;;
	*)
		echo
		echo "$dir is not on your PATH. Add this line to your shell profile:"
		echo "  export PATH=\"$dir:\$PATH\""
		;;
	esac
}

main "$@"
