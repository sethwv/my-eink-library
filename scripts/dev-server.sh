#!/usr/bin/env bash
set -euo pipefail

root=$(cd "$(dirname "$0")/.." && pwd)
library_dir=${EINK_LIBRARY_FIXTURE_DIR:-"$root/.dev/library"}
data_dir=${EINK_LIBRARY_DATA_DIR:-"$root/.dev/data"}

if [ "${1:-}" = "--prepare" ]; then
  prepare_only=true
elif [ "$#" -gt 0 ]; then
  printf 'usage: %s [--prepare]\n' "$0" >&2
  exit 2
else
  prepare_only=false
fi

mkdir -p "$library_dir" "$data_dir"
npm ci --prefix "$root/scripts/screenshots"
npm run fixtures --prefix "$root/scripts/screenshots" -- --output "$library_dir"

cd "$root/src"

if "$prepare_only"; then
  exit 0
fi

LIBRARY_PATH="$library_dir" \
DATA_DIR="$data_dir" \
PORT="${PORT:-8080}" \
exec go run ./cmd/server
