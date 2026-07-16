#!/usr/bin/env bash
#
# Give Cuttlefish guests internet access.
#
# Cuttlefish attaches each VM to host bridges (cvd-ebr / cvd-*tap) on a private
# subnet, but nothing on the host NATs that traffic out — and with UFW enabled the
# FORWARD chain defaults to DROP — so guests get an IP but no internet. This
# enables IPv4 forwarding and adds a NAT masquerade for the guest subnet, both
# persisted across reboots and `ufw reload`.
#
# Usage (on the appliance):
#   sudo bash scripts/ubuntu/cvd-network.sh
#
# Environment overrides (optional):
#   OPENCUTTLES_CVD_SUBNET   guest subnet to NAT (default 192.168.96.0/22)
#   OPENCUTTLES_WAN_IFACE    outbound interface (default: the default route's iface)
set -euo pipefail

CVD_SUBNET="${OPENCUTTLES_CVD_SUBNET:-192.168.96.0/22}"
WAN="${OPENCUTTLES_WAN_IFACE:-$(ip route show default 2>/dev/null | awk '/default/{print $5; exit}')}"

if [[ -z "$WAN" ]]; then
  echo "Could not detect the WAN interface. Re-run with OPENCUTTLES_WAN_IFACE=<iface>." >&2
  exit 1
fi
echo "WAN interface : $WAN"
echo "Guest subnet  : $CVD_SUBNET"

# 1. IPv4 forwarding, persisted + applied now.
echo 'net.ipv4.ip_forward=1' | sudo tee /etc/sysctl.d/99-opencuttles-forward.conf >/dev/null
sudo sysctl -w net.ipv4.ip_forward=1 >/dev/null

# 2. Allow UFW to forward guest traffic (its FORWARD policy defaults to DROP).
if [[ -f /etc/default/ufw ]] && grep -q '^DEFAULT_FORWARD_POLICY=' /etc/default/ufw; then
  sudo sed -i 's/^DEFAULT_FORWARD_POLICY=.*/DEFAULT_FORWARD_POLICY="ACCEPT"/' /etc/default/ufw
fi

# 3. Persist the NAT in UFW's before.rules (survives reboots + `ufw reload`).
BEFORE=/etc/ufw/before.rules
if [[ -f "$BEFORE" ]] && ! grep -q "OpenCuttles CVD NAT" "$BEFORE"; then
  sudo cp "$BEFORE" "${BEFORE}.oc.bak.$(date +%s)"
  sudo awk -v subnet="$CVD_SUBNET" -v wan="$WAN" '
    /^\*filter/ && !done {
      print "# OpenCuttles CVD NAT"
      print "*nat"
      print ":POSTROUTING ACCEPT [0:0]"
      print "-A POSTROUTING -s " subnet " -o " wan " -j MASQUERADE"
      print "COMMIT"
      print ""
      done=1
    }
    { print }
  ' "$BEFORE" | sudo tee "${BEFORE}.oc.tmp" >/dev/null
  sudo mv "${BEFORE}.oc.tmp" "$BEFORE"
  echo "Added NAT rule to $BEFORE"
fi

# 4. Apply immediately (idempotent) so no reboot is needed.
sudo iptables -t nat -C POSTROUTING -s "$CVD_SUBNET" -o "$WAN" -j MASQUERADE 2>/dev/null \
  || sudo iptables -t nat -A POSTROUTING -s "$CVD_SUBNET" -o "$WAN" -j MASQUERADE
sudo iptables -C FORWARD -s "$CVD_SUBNET" -j ACCEPT 2>/dev/null \
  || sudo iptables -A FORWARD -s "$CVD_SUBNET" -j ACCEPT
sudo iptables -C FORWARD -d "$CVD_SUBNET" -m state --state ESTABLISHED,RELATED -j ACCEPT 2>/dev/null \
  || sudo iptables -A FORWARD -d "$CVD_SUBNET" -m state --state ESTABLISHED,RELATED -j ACCEPT

# 5. Reload UFW so before.rules takes effect (no-op if UFW is inactive).
if command -v ufw >/dev/null 2>&1 && sudo ufw status | grep -q "Status: active"; then
  sudo ufw reload || true
fi

echo
echo "Done — Cuttlefish guests on $CVD_SUBNET are NAT'd out $WAN."
echo "Verify from a running device:"
echo "  adb -s <serial> shell ping -c1 8.8.8.8      # connectivity"
echo "  adb -s <serial> shell ping -c1 google.com   # DNS"
echo "If ping-by-IP works but DNS fails, set a resolver on the guest:"
echo "  adb -s <serial> shell 'su 0 setprop net.dns1 8.8.8.8'"
