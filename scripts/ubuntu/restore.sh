#!/usr/bin/env bash
set -euo pipefail

backup_path="${1:-}"
db_path="${OPENCUTTLES_DB:-/var/lib/opencuttles/opencuttles.db}"
config_dir="${OPENCUTTLES_CONFIG_DIR:-/etc/opencuttles}"

if [[ -z "$backup_path" || ! -f "${backup_path}/opencuttles.db" ]]; then
  echo "Usage: $0 /var/backups/opencuttles/opencuttles-YYYYmmddTHHMMSSZ" >&2
  exit 2
fi

if [[ -f "${backup_path}/SHA256SUMS" ]]; then
  (cd "$backup_path" && sha256sum -c SHA256SUMS)
else
  echo "Missing SHA256SUMS in backup; refusing restore." >&2
  exit 1
fi

sudo systemctl stop opencuttles-api || true
sudo install -d -o opencuttles -g opencuttles "$(dirname "$db_path")"
sudo install -m 0640 -o opencuttles -g opencuttles "${backup_path}/opencuttles.db" "$db_path"

if [[ -f "${backup_path}/config.tar.gz" ]]; then
  sudo install -d -m 0750 "$config_dir"
  sudo tar -C "$config_dir" -xzf "${backup_path}/config.tar.gz"
fi

sudo systemctl start opencuttles-api
echo "Restore completed from ${backup_path}"
