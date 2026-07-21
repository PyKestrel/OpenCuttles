#!/usr/bin/env bash
#
# Give the appliance a TLS identity, so desktop runners never have to talk to it
# over plaintext.
#
# Two deployment shapes:
#
#   * A real domain (FQDN) — Caddy already obtains a public certificate via ACME
#     and this script does nothing. Runners verify against the system trust store
#     and need no pin.
#
#   * An IP address or a single-label hostname — no public CA will issue for
#     those, so quickstart used to fall back to plain HTTP. That is the case this
#     script exists for: it mints a long-lived self-signed certificate and
#     publishes its public-key pin. Runners authenticate the appliance by that
#     pin, which is what makes TLS usable here at all.
#
# The pin is written to the env file as OPENCUTTLES_TLS_PIN so the API can hand
# it to the dashboard, which embeds it in the one-line install command.
#
# Idempotent: an existing certificate is never silently regenerated, because
# doing so would invalidate the pin held by every enrolled runner and they would
# all fail to reconnect. Use --force only when you intend that, and expect to
# re-enroll every device.
#
# Usage:
#   bash scripts/ubuntu/ensure-tls.sh <hostname-or-ip> [env-file]
#   bash scripts/ubuntu/ensure-tls.sh --force <hostname-or-ip> [env-file]
set -euo pipefail

force=0
if [[ "${1:-}" == "--force" ]]; then
  force=1
  shift
fi

host="${1:-}"
env_file="${2:-/etc/opencuttles/opencuttles.env}"
cert_dir="/etc/opencuttles/tls"
cert_file="${cert_dir}/appliance.crt"
key_file="${cert_dir}/appliance.key"

if [[ -z "$host" ]]; then
  echo "Usage: $0 [--force] <hostname-or-ip> [env-file]" >&2
  exit 2
fi

if ! command -v openssl >/dev/null 2>&1; then
  echo "openssl is required to generate the appliance certificate." >&2
  exit 1
fi

is_ip_address() {
  [[ "$1" =~ ^[0-9]+\.[0-9]+\.[0-9]+\.[0-9]+$ ]]
}

# A fully-qualified domain gets a real certificate from Caddy; nothing to do.
if [[ "$host" == *.* ]] && ! is_ip_address "$host"; then
  echo "${host} is a fully-qualified domain — Caddy will obtain a public certificate."
  echo "No self-signed certificate or pin is needed; runners verify against the system trust store."
  exit 0
fi

# The SAN must match how runners will address the appliance, or even a pinned
# connection fails the hostname check on any client that enforces it.
if is_ip_address "$host"; then
  san="IP:${host}"
else
  san="DNS:${host}"
fi

sudo install -d -m 0750 "$cert_dir"

if sudo test -f "$cert_file" && sudo test -f "$key_file" && [[ "$force" -eq 0 ]]; then
  echo "Certificate already present at ${cert_file} — leaving it alone."
else
  if [[ "$force" -eq 1 ]] && sudo test -f "$cert_file"; then
    echo "WARNING: regenerating the appliance certificate. Every enrolled runner"
    echo "         holds the OLD pin and will refuse to reconnect until it is"
    echo "         re-enrolled with the new one."
  fi
  echo "Generating a self-signed certificate for ${host}..."
  # 10 years: this is pinned by public key, so rotation buys little and an
  # expiry surprise on an appliance nobody logs into costs a lot.
  sudo openssl req -x509 -newkey rsa:2048 -sha256 -days 3650 -nodes \
    -keyout "$key_file" -out "$cert_file" \
    -subj "/CN=${host}" -addext "subjectAltName=${san}" \
    -addext "basicConstraints=critical,CA:FALSE" \
    -addext "keyUsage=critical,digitalSignature,keyEncipherment" \
    -addext "extendedKeyUsage=serverAuth" 2>/dev/null
  sudo chmod 0640 "$key_file"
  sudo chmod 0644 "$cert_file"
  # Caddy runs as its own user and must be able to read the key.
  if id -u caddy >/dev/null 2>&1; then
    sudo chgrp caddy "$key_file" "$cert_file" 2>/dev/null || true
  fi
fi

# The pin is over the public key (SPKI), not the certificate, so a future
# re-issue with the same key keeps every enrolled runner working.
pin_b64="$(sudo openssl x509 -in "$cert_file" -pubkey -noout \
  | openssl pkey -pubin -outform der \
  | openssl dgst -sha256 -binary \
  | openssl base64)"
pin="sha256/${pin_b64}"

# Guard against publishing a bogus pin. set -euo pipefail already aborts on a
# broken pipeline, but the failure mode here is bad enough to check explicitly:
# hashing empty input yields a perfectly well-formed pin that matches nothing, so
# every runner would enroll with it and then silently refuse to connect.
empty_sha256="47DEQpj8HBSa+/TImW+5JCeuQeRkm5NMpJWZG3hSuFU="
if [[ -z "$pin_b64" || "$pin_b64" == "$empty_sha256" ]]; then
  echo "Refusing to publish a pin computed from empty input — the certificate at" >&2
  echo "${cert_file} could not be read. No changes were written to ${env_file}." >&2
  exit 1
fi

if sudo test -f "$env_file"; then
  if sudo grep -q '^OPENCUTTLES_TLS_PIN=' "$env_file"; then
    sudo sed -i "s#^OPENCUTTLES_TLS_PIN=.*#OPENCUTTLES_TLS_PIN=${pin}#" "$env_file"
  else
    echo "OPENCUTTLES_TLS_PIN=${pin}" | sudo tee -a "$env_file" >/dev/null
  fi
fi

echo
echo "Appliance certificate: ${cert_file}"
echo "Pin: ${pin}"
echo
echo "The dashboard embeds this pin in the install command it shows for each device."
echo "It is not a secret — it identifies the appliance; it does not authenticate to it."
