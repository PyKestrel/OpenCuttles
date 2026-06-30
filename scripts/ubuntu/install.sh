#!/usr/bin/env bash
set -euo pipefail

release_dir="${1:-dist/package}"
caddy_include_marker="import /etc/caddy/conf.d/opencuttles.caddy"

if [[ ! -d "$release_dir" ]]; then
  echo "Usage: $0 dist/package" >&2
  exit 2
fi

sudo useradd --system --create-home --home-dir /var/lib/opencuttles --shell /usr/sbin/nologin opencuttles 2>/dev/null || true
sudo usermod -aG kvm opencuttles
sudo install -d -o opencuttles -g opencuttles /var/lib/opencuttles /var/lib/opencuttles/images /var/log/opencuttles /opt/opencuttles/bin /etc/opencuttles
sudo install -d -m 0755 /etc/caddy/conf.d

# cvd downloads images to /var/tmp/cvd and hardlinks them into the image dir.
# When /var/lib/opencuttles is a separate mount that fails with EXDEV, so keep
# the cvd cache on the same filesystem as the images via a symlink. Only replace
# /var/tmp/cvd when it is absent or already a symlink (never a real directory).
sudo install -d -o opencuttles -g opencuttles /var/lib/opencuttles/images/.cvd-cache
if [[ -L /var/tmp/cvd || ! -e /var/tmp/cvd ]]; then
  sudo ln -sfn /var/lib/opencuttles/images/.cvd-cache /var/tmp/cvd
fi

# The frontend is embedded in the binary, so only the binary needs deploying.
sudo rsync -a "${release_dir}/opt/opencuttles/bin/" /opt/opencuttles/bin/
sudo install -m 0644 "${release_dir}/deploy/systemd/opencuttles-api.service" /etc/systemd/system/opencuttles-api.service
if [[ ! -f /etc/opencuttles/opencuttles.env ]]; then
  sudo install -m 0640 "${release_dir}/deploy/systemd/opencuttles.env.example" /etc/opencuttles/opencuttles.env
fi
sudo install -m 0644 "${release_dir}/deploy/proxy/Caddyfile" /etc/caddy/conf.d/opencuttles.caddy
if [[ -f /etc/caddy/Caddyfile ]] && sudo grep -q "/usr/share/caddy" /etc/caddy/Caddyfile; then
  echo "$caddy_include_marker" | sudo tee /etc/caddy/Caddyfile >/dev/null
elif [[ -f /etc/caddy/Caddyfile ]] && ! sudo grep -qF "$caddy_include_marker" /etc/caddy/Caddyfile; then
  printf '\n%s\n' "$caddy_include_marker" | sudo tee -a /etc/caddy/Caddyfile >/dev/null
elif [[ ! -f /etc/caddy/Caddyfile ]]; then
  echo "$caddy_include_marker" | sudo tee /etc/caddy/Caddyfile >/dev/null
fi

sudo chown -R opencuttles:opencuttles /var/lib/opencuttles /var/log/opencuttles
sudo systemctl daemon-reload
sudo systemctl enable --now opencuttles-api
sudo systemctl reload caddy || sudo systemctl restart caddy

echo "OpenCuttles installed. Visit the configured Caddy hostname and bootstrap the local admin user."
