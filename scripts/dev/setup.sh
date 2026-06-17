#!/usr/bin/env bash
set -euo pipefail

root_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"

echo "Preparing backend dependencies..."
(cd "${root_dir}/backend" && go mod tidy)

echo "Preparing frontend dependencies..."
(cd "${root_dir}/frontend" && npm install)

echo "OpenCuttles development dependencies are ready."
echo "Start the app with: bash scripts/dev/start.sh"
