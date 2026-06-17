#!/usr/bin/env bash
set -euo pipefail

root_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
hostname="${OPENCUTTLES_HOSTNAME:-$(hostname -f 2>/dev/null || hostname)}"
origin="${OPENCUTTLES_ALLOWED_ORIGIN:-https://${hostname}}"
env_file="/etc/opencuttles/opencuttles.env"

if [[ "$(uname -s)" != "Linux" ]]; then
  echo "This quickstart is intended for Ubuntu Server." >&2
  exit 1
fi

echo "Installing host dependencies..."
sudo apt-get update
sudo apt-get install -y ca-certificates curl git make rsync sqlite3 ufw caddy golang-go nodejs npm

echo "Checking host readiness..."
bash "${root_dir}/scripts/ubuntu/check-host.sh" || true

echo "Preparing reproducible dependency files..."
(cd "${root_dir}/backend" && go mod tidy)
(cd "${root_dir}/frontend" && npm install)

echo "Building OpenCuttles package..."
(cd "${root_dir}" && make package)

echo "Installing OpenCuttles services..."
bash "${root_dir}/scripts/ubuntu/install.sh" "${root_dir}/dist/package"

if sudo test ! -s "$env_file"; then
  sudo install -m 0640 "${root_dir}/deploy/systemd/opencuttles.env.example" "$env_file"
fi

bootstrap_token="$(openssl rand -hex 24 2>/dev/null || date +%s%N)"
sudo sed -i \
  -e "s#^OPENCUTTLES_ALLOWED_ORIGIN=.*#OPENCUTTLES_ALLOWED_ORIGIN=${origin}#" \
  -e "s#^OPENCUTTLES_BOOTSTRAP_TOKEN=.*#OPENCUTTLES_BOOTSTRAP_TOKEN=${bootstrap_token}#" \
  -e "s#^OPENCUTTLES_SECURE_COOKIES=.*#OPENCUTTLES_SECURE_COOKIES=1#" \
  -e "s#^OPENCUTTLES_TRUST_PROXY_HEADERS=.*#OPENCUTTLES_TRUST_PROXY_HEADERS=1#" \
  "$env_file"

sudo systemctl daemon-reload
sudo systemctl enable --now opencuttles-api
sudo systemctl reload caddy || sudo systemctl restart caddy

if [[ "${OPENCUTTLES_CONFIGURE_FIREWALL:-0}" == "1" ]]; then
  bash "${root_dir}/scripts/ubuntu/firewall.sh"
fi

echo
echo "OpenCuttles is starting."
echo "Dashboard: ${origin}"
echo "Bootstrap token: ${bootstrap_token}"
echo
echo "After creating the first admin user, remove or rotate OPENCUTTLES_BOOTSTRAP_TOKEN in ${env_file}."
