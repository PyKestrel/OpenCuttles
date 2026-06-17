#!/usr/bin/env bash
set -euo pipefail

rollback_dir="${1:-}"

if [[ -z "$rollback_dir" || ! -d "$rollback_dir/opt-opencuttles" ]]; then
  echo "Usage: $0 /var/backups/opencuttles/rollback-YYYYmmddTHHMMSSZ" >&2
  exit 2
fi

sudo systemctl stop opencuttles-api || true
sudo rsync -a --delete "$rollback_dir/opt-opencuttles/" /opt/opencuttles/
if [[ -f "$rollback_dir/opencuttles-api.service" ]]; then
  sudo install -m 0644 "$rollback_dir/opencuttles-api.service" /etc/systemd/system/opencuttles-api.service
fi
if [[ -d "$rollback_dir/etc-opencuttles" ]]; then
  sudo rsync -a --delete "$rollback_dir/etc-opencuttles/" /etc/opencuttles/
fi
sudo systemctl daemon-reload
sudo systemctl start opencuttles-api
sudo systemctl reload caddy || true

echo "Rollback complete from ${rollback_dir}"
