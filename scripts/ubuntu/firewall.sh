#!/usr/bin/env bash
set -euo pipefail

ssh_port="${SSH_PORT:-22}"

if ! command -v ufw >/dev/null 2>&1; then
  echo "ufw is required. Install it with: sudo apt-get install ufw" >&2
  exit 1
fi

sudo ufw default deny incoming
sudo ufw default allow outgoing
sudo ufw allow "${ssh_port}/tcp" comment "SSH"
sudo ufw allow 80/tcp comment "HTTP for ACME and redirect"
sudo ufw allow 443/tcp comment "OpenCuttles HTTPS"
sudo ufw --force enable
sudo ufw status verbose

echo "Firewall configured. ADB and Cuttlefish internal ports remain host-local."
