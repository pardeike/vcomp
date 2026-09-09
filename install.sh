#!/bin/sh
# Build vcomp, put the binary on your PATH, and write the default settings and
# templates into ~/.vcomp (or $VCOMP_HOME). Safe to re-run: existing files are
# kept unless you pass -force.
#
#   ./install.sh            install, keeping any edits you have made
#   ./install.sh -force     overwrite the global defaults with the shipped ones
#   BIN_DIR=/usr/local/bin ./install.sh

set -eu

here=$(cd "$(dirname "$0")" && pwd)
bin=${BIN_DIR:-$HOME/.local/bin}

mkdir -p "$bin"
(cd "$here" && go build -o "$bin/vcomp" ./cmd/vcomp)
echo "installed $bin/vcomp"

"$bin/vcomp" install "$@"

case ":$PATH:" in
	*":$bin:"*) ;;
	*) echo "note: $bin is not on your PATH" ;;
esac
