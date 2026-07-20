#!/usr/bin/env bash
#
# Fill in any missing or placeholder secrets in the OpenCuttles env file.
#
# Every secret below is a real credential — shipping a known default is the same
# as having no auth at all:
#   OPENCUTTLES_MCP_TOKEN       full MCP device control + reads the decrypted
#                               provider API key via /api/v1/agent/runtime
#   OPENCUTTLES_BOOTSTRAP_TOKEN claims the first admin account
#   OPENCUTTLES_SECRET_KEY      encrypts stored API keys and desktop runner
#                               tokens at rest
#
# Idempotent: values that already look real are left alone, so this is safe to
# re-run on an existing appliance to fill in whatever is missing.
#
# Usage:
#   bash scripts/ubuntu/ensure-secrets.sh [env-file]
#   bash scripts/ubuntu/ensure-secrets.sh --rotate KEY[,KEY...] [env-file]
#
# Examples:
#   bash scripts/ubuntu/ensure-secrets.sh
#   bash scripts/ubuntu/ensure-secrets.sh --rotate OPENCUTTLES_MCP_TOKEN
#
# WARNING: rotating OPENCUTTLES_SECRET_KEY makes every already-encrypted value
# undecryptable — stored provider API keys and desktop runner tokens. Those
# devices must be re-enrolled. Never rotate it casually, and back it up.
set -euo pipefail

rotate_list=""
if [[ "${1:-}" == "--rotate" ]]; then
  rotate_list="${2:-}"
  shift 2
fi
env_file="${1:-/etc/opencuttles/opencuttles.env}"

if ! sudo test -f "$env_file"; then
  echo "env file not found: $env_file" >&2
  exit 2
fi

gen_hex() { openssl rand -hex 32; }
gen_b64() { openssl rand -base64 32; }

if ! command -v openssl >/dev/null 2>&1; then
  echo "openssl is required to generate secrets — install it and re-run." >&2
  exit 1
fi

current_value() { sudo sed -n "s#^$1=##p" "$env_file" | head -1; }

# A value is a placeholder if it is empty or still one of the shipped
# "change-this…" strings.
is_placeholder() {
  local v="$1"
  [[ -z "$v" || "$v" == change-this* ]]
}

should_rotate() {
  [[ ",${rotate_list}," == *",$1,"* ]]
}

set_value() {
  local key="$1" value="$2"
  # base64/hex never contain '#' or '&', so this substitution is safe.
  if sudo grep -q "^${key}=" "$env_file"; then
    sudo sed -i "s#^${key}=.*#${key}=${value}#" "$env_file"
  else
    echo "${key}=${value}" | sudo tee -a "$env_file" >/dev/null
  fi
}

generated=()
ensure() {
  local key="$1" generator="$2"
  local value
  value="$(current_value "$key")"
  if should_rotate "$key"; then
    set_value "$key" "$($generator)"
    generated+=("$key (rotated)")
  elif is_placeholder "$value"; then
    set_value "$key" "$($generator)"
    generated+=("$key")
  fi
}

ensure OPENCUTTLES_BOOTSTRAP_TOKEN gen_hex
ensure OPENCUTTLES_MCP_TOKEN gen_hex
ensure OPENCUTTLES_SECRET_KEY gen_b64

sudo chmod 0640 "$env_file"

if [[ ${#generated[@]} -eq 0 ]]; then
  echo "Secrets already set in ${env_file} — nothing to do."
else
  # Deliberately does not echo the values; read them from the env file if needed
  # (the bootstrap token is printed by quickstart.sh, which owns that UX).
  echo "Generated in ${env_file}: ${generated[*]}"
  echo "Restart the API to pick them up: sudo systemctl restart opencuttles-api"
fi
