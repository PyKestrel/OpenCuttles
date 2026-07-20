#!/usr/bin/env bash
set -euo pipefail

root_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
db_path="${OPENCUTTLES_DB:-${root_dir}/data/dev/opencuttles.db}"

mkdir -p "${root_dir}/data/dev" "${root_dir}/data/images"

export OPENCUTTLES_LISTEN="${OPENCUTTLES_LISTEN:-127.0.0.1:8080}"
export OPENCUTTLES_DB="$db_path"
export OPENCUTTLES_ALLOWED_ORIGIN="${OPENCUTTLES_ALLOWED_ORIGIN:-http://localhost:5173}"
export OPENCUTTLES_SECURE_COOKIES=0
# Allows claiming the first admin without a bootstrap token. Local dev only:
# no install path sets this, so a real deployment can never inherit it.
export OPENCUTTLES_DEV_MODE=1
export OPENCUTTLES_EXECUTE_CVD="${OPENCUTTLES_EXECUTE_CVD:-0}"
export OPENCUTTLES_IMAGE_ROOT="${OPENCUTTLES_IMAGE_ROOT:-${root_dir}/data/images}"

cleanup() {
  if [[ -n "${api_pid:-}" ]]; then
    kill "$api_pid" 2>/dev/null || true
  fi
  if [[ -n "${ui_pid:-}" ]]; then
    kill "$ui_pid" 2>/dev/null || true
  fi
}
trap cleanup EXIT INT TERM

echo "Starting OpenCuttles API on http://${OPENCUTTLES_LISTEN}"
(cd "${root_dir}/backend" && go run ./cmd/opencuttles-api) &
api_pid=$!

if [[ ! -d "${root_dir}/frontend/node_modules" ]]; then
  echo "Installing frontend dependencies..."
  (cd "${root_dir}/frontend" && npm install)
fi

echo "Starting OpenCuttles dashboard on http://localhost:5173"
(cd "${root_dir}/frontend" && npm run dev -- --host 127.0.0.1) &
ui_pid=$!

echo
echo "Open http://localhost:5173"
echo "Bootstrap token is not required in local dev mode."
echo "Press Ctrl+C to stop both services."
wait
