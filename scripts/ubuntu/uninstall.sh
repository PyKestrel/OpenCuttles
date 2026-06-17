#!/usr/bin/env bash
set -euo pipefail

purge_data="${PURGE_DATA:-0}"

sudo systemctl disable --now opencuttles-api || true
sudo rm -f /etc/systemd/system/opencuttles-api.service
sudo systemctl daemon-reload
sudo rm -rf /opt/opencuttles
sudo rm -f /etc/caddy/conf.d/opencuttles.caddy
sudo systemctl reload caddy || true

if [[ "$purge_data" == "1" ]]; then
  sudo rm -rf /var/lib/opencuttles /var/log/opencuttles /etc/opencuttles
  sudo userdel opencuttles 2>/dev/null || true
  echo "OpenCuttles uninstalled and data purged."
else
  echo "OpenCuttles uninstalled. Data preserved under /var/lib/opencuttles and /etc/opencuttles."
fi
