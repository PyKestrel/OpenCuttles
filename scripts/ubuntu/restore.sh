#!/usr/bin/env bash
#
# Restore an OpenCuttles snapshot taken by backup.sh.
#
# Usage:
#   bash scripts/ubuntu/restore.sh /var/backups/opencuttles/opencuttles-YYYYmmddTHHMMSSZ
#
# Env:
#   OPENCUTTLES_RESTORE_ARTIFACTS=1  also restore artifacts.tar.gz (off by
#                                    default: it is large and replaces the whole
#                                    artifact tree)
set -euo pipefail

backup_path="${1:-}"
db_path="${OPENCUTTLES_DB:-/var/lib/opencuttles/opencuttles.db}"
config_dir="${OPENCUTTLES_CONFIG_DIR:-/etc/opencuttles}"
artifact_root="${OPENCUTTLES_ARTIFACT_ROOT:-/var/lib/opencuttles/artifacts}"

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

# Remove the CURRENT database's WAL and shared-memory sidecars before dropping
# the restored file into place.
#
# This is the one step that makes a restore safe. Those files belong to the
# database being replaced, not to the backup. Left behind, SQLite treats them as
# valid journal state for the file now sitting at that path and replays frames
# from the OLD database over the RESTORED one on first open — silently
# corrupting it, or resurrecting exactly the data the restore was meant to undo.
sudo rm -f "${db_path}-wal" "${db_path}-shm"

sudo install -d -o opencuttles -g opencuttles "$(dirname "$db_path")"
sudo install -m 0640 -o opencuttles -g opencuttles "${backup_path}/opencuttles.db" "$db_path"

if [[ -f "${backup_path}/config.tar.gz" ]]; then
  sudo install -d -m 0750 "$config_dir"
  sudo tar -C "$config_dir" -xzf "${backup_path}/config.tar.gz"
fi

if [[ "${OPENCUTTLES_RESTORE_ARTIFACTS:-0}" == "1" && -f "${backup_path}/artifacts.tar.gz" ]]; then
  sudo install -d -o opencuttles -g opencuttles "$artifact_root"
  sudo tar -C "$artifact_root" -xzf "${backup_path}/artifacts.tar.gz"
  sudo chown -R opencuttles:opencuttles "$artifact_root"
elif [[ -f "${backup_path}/artifacts.tar.gz" ]]; then
  echo "Snapshot contains artifacts.tar.gz (not restored)."
  echo "To restore evidence too: OPENCUTTLES_RESTORE_ARTIFACTS=1 $0 ${backup_path}"
fi

# Verify the restored database actually opens before handing it to the API.
if command -v sqlite3 >/dev/null 2>&1; then
  if ! sudo sqlite3 "$db_path" 'PRAGMA integrity_check;' | grep -q '^ok$'; then
    echo "Restored database failed integrity_check; NOT starting the API." >&2
    exit 1
  fi
fi

sudo systemctl start opencuttles-api
echo "Restore completed from ${backup_path}"
