#!/usr/bin/env bash
#
# Snapshot the OpenCuttles database, config, and test evidence.
#
# Usage:
#   bash scripts/ubuntu/backup.sh [backup-dir]
#
# Env:
#   OPENCUTTLES_DB             database path
#   OPENCUTTLES_CONFIG_DIR     config dir (holds the env file — see below)
#   OPENCUTTLES_ARTIFACT_ROOT  per-step screenshots and session video
#   OPENCUTTLES_BACKUP_KEEP    snapshots to retain (default 14, 0 = keep all)
#   OPENCUTTLES_SKIP_ARTIFACTS=1  database + config only (artifacts are large)
#
# NOTE: config.tar.gz contains opencuttles.env, which holds OPENCUTTLES_SECRET_KEY
# and the MCP token. The snapshot is mode 0750 — treat it as a secret. It is also
# what makes the backup restorable: without SECRET_KEY, every stored provider key
# and runner token in the database is undecryptable.
set -euo pipefail

db_path="${OPENCUTTLES_DB:-/var/lib/opencuttles/opencuttles.db}"
config_dir="${OPENCUTTLES_CONFIG_DIR:-/etc/opencuttles}"
artifact_root="${OPENCUTTLES_ARTIFACT_ROOT:-/var/lib/opencuttles/artifacts}"
backup_dir="${1:-/var/backups/opencuttles}"
keep="${OPENCUTTLES_BACKUP_KEEP:-14}"
timestamp="$(date -u +%Y%m%dT%H%M%SZ)"
target="${backup_dir}/opencuttles-${timestamp}"

# A plain `cp` of a live WAL database copies the main file without the committed
# frames still sitting in the -wal, producing a silently stale (or torn) backup.
# sqlite3 .backup is the only safe way to snapshot a database that is in use, so
# fail loudly rather than quietly writing an unreliable one.
if ! command -v sqlite3 >/dev/null 2>&1; then
  echo "sqlite3 is required for a consistent backup of a live WAL database." >&2
  echo "Install it and re-run:  sudo apt-get install -y sqlite3" >&2
  exit 1
fi

if ! sudo test -f "$db_path"; then
  echo "database not found: ${db_path}" >&2
  exit 1
fi

sudo install -d -m 0750 "$backup_dir"
sudo install -d -m 0750 "$target"

# Clean up a partial snapshot if anything below fails, so a failed run can never
# be mistaken for a usable backup.
cleanup_failed() {
  local code=$?
  if [[ $code -ne 0 ]]; then
    echo "Backup failed; removing partial snapshot ${target}" >&2
    sudo rm -rf "$target"
  fi
  exit $code
}
trap cleanup_failed EXIT

sudo sqlite3 "$db_path" ".backup '${target}/opencuttles.db'"

if sudo test -d "$config_dir"; then
  sudo tar -C "$config_dir" -czf "${target}/config.tar.gz" .
fi

if [[ "${OPENCUTTLES_SKIP_ARTIFACTS:-0}" != "1" ]] && sudo test -d "$artifact_root"; then
  echo "Archiving test artifacts from ${artifact_root}..."
  sudo tar -C "$artifact_root" -czf "${target}/artifacts.tar.gz" .
fi

# Checksum everything in the snapshot, not just the database — a corrupt
# config.tar.gz loses the secret key, which is just as fatal to a restore.
sudo sh -c "cd '${target}' && sha256sum opencuttles.db \
  \$( [ -f config.tar.gz ] && echo config.tar.gz ) \
  \$( [ -f artifacts.tar.gz ] && echo artifacts.tar.gz ) > SHA256SUMS"

sudo chown -R "$(id -u):$(id -g)" "$target"
trap - EXIT

# Retention: keep the newest N snapshots. Without this, backups run from a timer
# fill the disk and take the appliance down.
if [[ "$keep" -gt 0 ]]; then
  mapfile -t stale < <(
    find "$backup_dir" -mindepth 1 -maxdepth 1 -type d -name 'opencuttles-*' \
      | sort -r | tail -n "+$((keep + 1))"
  )
  for old in "${stale[@]:-}"; do
    [[ -n "$old" ]] || continue
    echo "Pruning old snapshot ${old}"
    sudo rm -rf "$old"
  done
fi

echo "Backup written to ${target}"
du -sh "$target" 2>/dev/null || true
