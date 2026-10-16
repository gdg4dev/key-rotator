#!/usr/bin/env sh
# Rebuild keyrotator and restart it.
#
# A running instance is stopped first, then the new binary is built over it
# and started again in the background. Output goes to bin/keyrotator.log.
# Usage:  ./scripts/update.sh
set -eu

root=$(cd "$(dirname "$0")/.." && pwd)
go=${GO:-go}
exe="$root/bin/keyrotator$("$go" env GOEXE)"

if command -v pgrep >/dev/null 2>&1 && pgrep -x keyrotator >/dev/null 2>&1; then
    echo "stopping running instance..."
    pkill -x keyrotator || true
    sleep 1
    if pgrep -x keyrotator >/dev/null 2>&1; then
        echo "keyrotator is still running and could not be stopped" >&2
        exit 1
    fi
fi

"$root/scripts/build.sh"
echo "keyrotator updated"

nohup "$exe" -quiet >>"$root/bin/keyrotator.log" 2>&1 &
sleep 1
echo "restarted on http://127.0.0.1:8787/"
