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
# The frontend is embedded in the binary; only the binary needs replacing.
sudo rsync -a "${release_dir}/opt/opencuttles/bin/" /opt/opencuttles/bin/
sudo install -m 0644 "${release_dir}/deploy/systemd/opencuttles-api.service" /etc/systemd/system/opencuttles-api.service
bash "${script_dir}/apply-caddyfile.sh" "${release_dir}/deploy/proxy/Caddyfile"
sudo systemctl daemon-reload
sudo systemctl start opencuttles-api
sudo systemctl reload caddy || true

# Each upgrade copies the whole of /opt/opencuttles here. Unpruned, that grows
# without bound until the appliance runs out of disk — the same failure the
# nightly backup retention guards against.
keep_rollbacks="${OPENCUTTLES_ROLLBACK_KEEP:-5}"
if [[ "$keep_rollbacks" -gt 0 ]]; then
  mapfile -t stale_rollbacks < <(
    find /var/backups/opencuttles -mindepth 1 -maxdepth 1 -type d -name 'rollback-*' \
      | sort -r | tail -n "+$((keep_rollbacks + 1))"
  )
  for old in "${stale_rollbacks[@]:-}"; do
    [[ -n "$old" ]] || continue
    echo "Pruning old rollback snapshot ${old}"
    sudo rm -rf "$old"
  done
fi

echo "Upgrade complete. Rollback snapshot: ${rollback_dir}"
echo "Keeping the newest ${keep_rollbacks} rollback snapshots (OPENCUTTLES_ROLLBACK_KEEP)."
