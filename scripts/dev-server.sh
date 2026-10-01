#!/usr/bin/env bash
set -euo pipefail

root=$(cd "$(dirname "$0")/.." && pwd)
library_dir=${EINK_LIBRARY_FIXTURE_DIR:-"$root/.dev/library"}
data_dir=${EINK_LIBRARY_DATA_DIR:-"$root/.dev/data"}

mkdir -p "$library_dir" "$data_dir"
npm ci --prefix "$root/scripts/screenshots"
npm run fixtures --prefix "$root/scripts/screenshots" -- --output "$library_dir"

cd "$root/src"
if [ ! -f "$data_dir/users.db" ]; then
  DATA_DIR="$data_dir" go run ./cmd/server admin create-user admin password admin
fi

LIBRARY_PATH="$library_dir" \
DATA_DIR="$data_dir" \
PORT="${PORT:-8080}" \
exec go run ./cmd/server
