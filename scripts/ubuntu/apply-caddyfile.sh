#!/usr/bin/env bash
#
# Install the packaged Caddy site config without discarding the host's TLS
# configuration.
#
# The packaged Caddyfile is a template: its first line is ":80", and quickstart.sh
# rewrites that in place to the real site address and inserts a "tls" line when
# the appliance uses a self-signed certificate. Those edits live in the installed
# file and nowhere else, so copying the template over it — which install.sh and
# upgrade.sh both used to do directly — silently reverted an HTTPS appliance to
# plaintext HTTP on every upgrade.
#
# That is not a cosmetic regression. Desktop runners refuse plaintext, so the
# downgrade surfaces as every runner disconnecting after an upgrade, with nothing
# in the logs pointing at Caddy.
#
# Usage:
#   bash apply-caddyfile.sh <template> [dest]
set -euo pipefail

template="${1:?usage: apply-caddyfile.sh <template> [dest]}"
dest="${2:-/etc/caddy/conf.d/opencuttles.caddy}"

if [[ ! -f "$template" ]]; then
  echo "apply-caddyfile: template not found: ${template}" >&2
  exit 2
fi

# SUDO is overridable so the unit test can run the real script unprivileged
# against a temp directory. It defaults to sudo, which is how both callers use it.
SUDO="${OPENCUTTLES_SUDO-sudo}"

site_address=""
tls_line=""
backup=""
if $SUDO test -f "$dest"; then
  # Line 1 is "<site address> {". Capture the address without the brace.
  site_address="$($SUDO sed -n '1s/[[:space:]]*{[[:space:]]*$//p' "$dest")"
  # Any tls directive pointing at the appliance certificate.
  tls_line="$($SUDO sed -n '\#^[[:space:]]*tls /etc/opencuttles/tls/#p' "$dest" | head -1)"
  backup="$(mktemp)"
  $SUDO cat "$dest" > "$backup"
fi

$SUDO install -m 0644 "$template" "$dest"

if [[ -n "$site_address" ]]; then
  $SUDO sed -i "1s#.*#${site_address} {#" "$dest"
fi

if [[ -n "$tls_line" ]]; then
  # Drop any tls line the template carried, then re-insert the captured one, so
  # re-running cannot stack duplicates — which Caddy rejects.
  $SUDO sed -i '\#^[[:space:]]*tls /etc/opencuttles/tls/#d' "$dest"
  $SUDO sed -i "1a\\${tls_line}" "$dest"
fi

# Validate before anyone reloads. A broken site config that is only discovered at
# reload time leaves the dashboard unreachable with the previous config already
# overwritten, so restore it and fail loudly instead.
if [[ -n "$backup" ]] && command -v caddy >/dev/null 2>&1 && [[ -f /etc/caddy/Caddyfile ]]; then
  if ! $SUDO caddy validate --adapter caddyfile --config /etc/caddy/Caddyfile >/dev/null 2>&1; then
    echo "apply-caddyfile: the resulting Caddy config is invalid; restoring the previous one." >&2
    $SUDO install -m 0644 "$backup" "$dest"
    rm -f "$backup"
    exit 1
  fi
fi

# An if rather than "[[ ... ]] && rm": under set -e the && form survives only
# because bash exempts the non-final command in a list, which is too subtle a
# thing for a fresh install's exit status to rest on.
if [[ -n "$backup" ]]; then
  rm -f "$backup"
fi

if [[ -n "$site_address" && "$site_address" != ":80" ]]; then
  echo "Caddy site config updated, keeping the local site address (${site_address})."
else
  echo "Caddy site config updated."
fi
