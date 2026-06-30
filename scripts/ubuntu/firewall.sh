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

# WebRTC interactive console (cuttlefish-operator signaling is proxied through
# OpenCuttles, but browsers connect to the media ports directly on the LAN/VPN).
if [[ "${OPENCUTTLES_OPEN_WEBRTC:-1}" != "0" ]]; then
  sudo ufw allow 15550:15599/tcp comment "Cuttlefish WebRTC media (TCP)"
  sudo ufw allow 15550:15599/udp comment "Cuttlefish WebRTC media (UDP)"
fi

sudo ufw --force enable
sudo ufw status verbose

echo "Firewall configured."
echo "  - 80/443 open for the OpenCuttles dashboard."
echo "  - WebRTC media ports 15550-15599 (TCP/UDP) open for the device console."
echo "  - The operator (:1443) and ADB ports stay host-local (proxied via OpenCuttles)."
echo "  - Set OPENCUTTLES_OPEN_WEBRTC=0 to skip opening the media port range."
