#!/bin/sh
# Build vcomp, put the binary somewhere already on your PATH, and write the
# default settings and templates into ~/.vcomp (or $VCOMP_HOME).
#
# Safe to re-run: your edited settings and templates are kept unless -force.
#
#   ./install.sh                  install, keeping any edits you have made
#   ./install.sh -force           overwrite the global defaults with the shipped ones
#   BIN_DIR=/usr/local/bin ./install.sh
#
# Without BIN_DIR the first of these that exists, is writable, and is on your
# PATH wins. User-owned directories are preferred so nothing needs sudo.

set -eu

here=$(cd "$(dirname "$0")" && pwd)

candidates="$HOME/Scripts $HOME/bin $HOME/.local/bin /usr/local/bin /opt/homebrew/bin"

on_path() {
	case ":$PATH:" in
		*":$1:"*) return 0 ;;
		*) return 1 ;;
	esac
}

pick_dir() {
	for d in $candidates; do
		if [ -d "$d" ] && [ -w "$d" ] && on_path "$d"; then
			echo "$d"
			return 0
		fi
	done
	return 1
}

bin=${BIN_DIR:-$(pick_dir || echo "$HOME/.local/bin")}
mkdir -p "$bin"

(cd "$here" && go build -o "$bin/vcomp" ./cmd/vcomp)
echo "installed $bin/vcomp"

"$bin/vcomp" install "$@"

if on_path "$bin"; then
	echo
	echo "Ready. Try:  mkdir /tmp/vgame && cd /tmp/vgame && vcomp"
else
	echo
	echo "note: $bin is not on your PATH - add it, or set BIN_DIR to a directory that is."
fi
