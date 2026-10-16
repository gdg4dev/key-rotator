#!/usr/bin/env sh
# Build keyrotator into bin/keyrotator (bin/keyrotator.exe on Windows).
# Usage:  ./scripts/build.sh
# Set GO to use a specific Go binary, e.g. GO=/usr/local/go/bin/go ./scripts/build.sh
set -eu

root=$(cd "$(dirname "$0")/.." && pwd)
go=${GO:-go}
out="$root/bin/keyrotator$("$go" env GOEXE)"

mkdir -p "$root/bin"
cd "$root"
"$go" build -trimpath -ldflags "-s -w" -o "$out" ./cmd/keyrotator
echo "Built $out"
