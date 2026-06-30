#!/usr/bin/env bash
set -euo pipefail

db_path="${OPENCUTTLES_DB:-/var/lib/opencuttles/opencuttles.db}"
config_dir="${OPENCUTTLES_CONFIG_DIR:-/etc/opencuttles}"
backup_dir="${1:-/var/backups/opencuttles}"
timestamp="$(date -u +%Y%m%dT%H%M%SZ)"
target="${backup_dir}/opencuttles-${timestamp}"

sudo install -d -m 0750 "$backup_dir"
sudo install -d -m 0750 "$target"

if command -v sqlite3 >/dev/null 2>&1; then
  sudo sqlite3 "$db_path" ".backup '${target}/opencuttles.db'"
else
  sudo cp "$db_path" "${target}/opencuttles.db"
fi

if sudo test -d "$config_dir"; then
  sudo tar -C "$config_dir" -czf "${target}/config.tar.gz" .
fi

# The snapshot directory is root-created, so write the checksum as root, then
# hand the whole snapshot back to the invoking user.
sudo sh -c "cd '${target}' && sha256sum opencuttles.db > SHA256SUMS"
sudo chown -R "$(id -u):$(id -g)" "$target"
echo "Backup written to ${target}"
