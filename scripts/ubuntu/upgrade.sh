#!/usr/bin/env bash
set -euo pipefail

release_dir="${1:-dist/package}"
script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
stamp="$(date -u +%Y%m%dT%H%M%SZ)"
rollback_dir="/var/backups/opencuttles/rollback-${stamp}"

if [[ ! -d "$release_dir" ]]; then
  echo "Usage: $0 dist/package" >&2
  exit 2
fi

sudo install -d -m 0750 "$rollback_dir"
sudo cp -a /opt/opencuttles "$rollback_dir/opt-opencuttles"
sudo cp -a /etc/systemd/system/opencuttles-api.service "$rollback_dir/opencuttles-api.service" 2>/dev/null || true
sudo cp -a /etc/opencuttles "$rollback_dir/etc-opencuttles" 2>/dev/null || true
bash "${script_dir}/backup.sh" "/var/backups/opencuttles"

sudo systemctl stop opencuttles-api
sudo rsync -a "${release_dir}/opt/opencuttles/bin/" /opt/opencuttles/bin/
sudo rsync -a --delete "${release_dir}/opt/opencuttles/frontend/dist/" /opt/opencuttles/frontend/dist/
sudo install -m 0644 "${release_dir}/deploy/systemd/opencuttles-api.service" /etc/systemd/system/opencuttles-api.service
sudo install -m 0644 "${release_dir}/deploy/proxy/Caddyfile" /etc/caddy/conf.d/opencuttles.caddy
sudo systemctl daemon-reload
sudo systemctl start opencuttles-api
sudo systemctl reload caddy || true

echo "Upgrade complete. Rollback snapshot: ${rollback_dir}"
