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

# Keep routine output in one ignored log, including failures from child commands.
mkdir -p "$here/.vcomp/logs"
log="$here/.vcomp/logs/install.log"
exec 3>&1
exec >"$log" 2>&1
step="select install directory"
finish() {
    result=$?
    trap - EXIT
    if [ "$result" -eq 0 ]; then
        echo ok >&3
    else
        echo "failed: $step (exit $result); full log: $log" >&3
        tail -n 15 "$log" >&3
    fi
    exit "$result"
}
trap finish EXIT
child=
run() {
    "$@" &
    child=$!
    wait "$child"
    child=
}
cancel() {
    if [ -n "$child" ]; then
        kill -TERM "$child" 2>/dev/null || :
        wait "$child" 2>/dev/null || :
    fi
    exit "$1"
}
trap 'cancel 130' INT
trap 'cancel 143' TERM

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

step="build binary"
cd "$here"
run go build -o "$bin/vcomp" ./cmd/vcomp
echo "installed $bin/vcomp"

step="install settings and templates"
run "$bin/vcomp" install "$@"

step="verify installed binary"
run "$bin/vcomp" --help

# A second copy left on the PATH is worse than none: shells hash the path they
# first resolved, so an old build can keep running long after this one lands.
others=$(
	echo "$PATH" | tr ':' '\n' | while IFS= read -r d; do
		# An "if" rather than an && chain: a failing test as the last command in
		# the loop body would make the whole substitution non-zero, and set -e
		# would end the script here.
		if [ -n "$d" ] && [ "$d" != "$bin" ] && [ -x "$d/vcomp" ]; then
			echo "  $d/vcomp"
		fi
	done
)
if [ -n "$others" ]; then
	echo
	echo "warning: other vcomp binaries are on your PATH:"
	echo "$others"
	echo "Delete them, or your shell may keep running one of those instead."
fi

echo
if on_path "$bin"; then
	echo "Run 'rehash' (zsh) or 'hash -r' (bash) if vcomp still behaves like an old build."
	echo "Ready. Try:  mkdir /tmp/vgame && cd /tmp/vgame && vcomp"
else
	echo "note: $bin is not on your PATH - add it, or set BIN_DIR to a directory that is."
fi
