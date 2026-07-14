#!/usr/bin/env bash
#
# One-command updater for an OpenCuttles host.
#
# Pulls the latest code, builds + packages a release, runs the safe upgrade
# (with rollback snapshot), refreshes the firewall rules, ensures the WebRTC
# operator is running, and health-checks the API.
#
# Usage:
#   bash scripts/ubuntu/update.sh
#
# Environment toggles (all optional):
#   OPENCUTTLES_SKIP_GIT_PULL=1     Do not run "git pull" (use the current tree).
#   OPENCUTTLES_GIT_REF=<ref>       git checkout this ref before pulling.
#   OPENCUTTLES_SKIP_FIREWALL=1     Do not (re)apply UFW rules.
#   OPENCUTTLES_SKIP_OPERATOR=1     Do not enable/start cuttlefish-operator.
#   OPENCUTTLES_ENABLE_EXECUTE_CVD=1  Force OPENCUTTLES_EXECUTE_CVD=1 in the env file.
#   OPENCUTTLES_HEALTH_URL=<url>    Health endpoint (default http://127.0.0.1:8080/api/v1/healthz).
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd "${script_dir}/../.." && pwd)"
health_url="${OPENCUTTLES_HEALTH_URL:-http://127.0.0.1:8080/api/v1/healthz}"

log() { printf '\n\033[1;36m==> %s\033[0m\n' "$*"; }
warn() { printf '\033[1;33m[warn]\033[0m %s\n' "$*" >&2; }

cd "$repo_root"

# 1. Update source -----------------------------------------------------------
if [[ "${OPENCUTTLES_SKIP_GIT_PULL:-0}" == "1" ]]; then
  log "Skipping git pull (OPENCUTTLES_SKIP_GIT_PULL=1); using current tree."
elif command -v git >/dev/null 2>&1 && [[ -d .git ]]; then
  if [[ -n "${OPENCUTTLES_GIT_REF:-}" ]]; then
    log "Checking out ${OPENCUTTLES_GIT_REF}"
    git checkout "${OPENCUTTLES_GIT_REF}"
  fi
  log "Pulling latest changes"
  git pull --ff-only || warn "git pull failed; continuing with the current tree."
else
  warn "Not a git checkout; skipping pull and building the current tree."
fi

# 2. Build + package ---------------------------------------------------------
log "Building and packaging release (make package)"
make package

# 3. Apply upgrade (snapshots + rollback handled by upgrade.sh) --------------
log "Applying upgrade (this restarts opencuttles-api)"
bash "${script_dir}/upgrade.sh" "${repo_root}/dist/package"

# 3b. Agent sidecar (Flue) ---------------------------------------------------
# The API upgrade above does NOT touch the agent sidecar: it runs a prebuilt
# agent/dist/server.mjs, so without rebuilding it here a `git pull` silently keeps
# the old bundle (and thus the old model/prompt/skills). Rebuild + restart it.
if [[ "${OPENCUTTLES_SKIP_AGENT:-0}" == "1" ]]; then
  log "Skipping agent sidecar rebuild (OPENCUTTLES_SKIP_AGENT=1)."
elif systemctl list-unit-files 2>/dev/null | grep -q '^opencuttles-agent'; then
  log "Rebuilding the agent sidecar bundle (make build-agent) and restarting it"
  # 'flue build' needs Node >= 22.18; if it fails, stop loudly rather than leave
  # a stale sidecar running (the failure this whole step exists to prevent).
  make build-agent
  sudo systemctl restart opencuttles-agent
else
  warn "opencuttles-agent service not found; skipping sidecar rebuild."
fi

# 4. Firewall ----------------------------------------------------------------
if [[ "${OPENCUTTLES_SKIP_FIREWALL:-0}" == "1" ]]; then
  log "Skipping firewall refresh (OPENCUTTLES_SKIP_FIREWALL=1)."
else
  log "Refreshing firewall rules (WebRTC console ports)"
  bash "${script_dir}/firewall.sh" || warn "Firewall step failed; review UFW manually."
fi

# 5. WebRTC operator ---------------------------------------------------------
if [[ "${OPENCUTTLES_SKIP_OPERATOR:-0}" == "1" ]]; then
  log "Skipping operator check (OPENCUTTLES_SKIP_OPERATOR=1)."
elif systemctl list-unit-files 2>/dev/null | grep -q '^cuttlefish-operator'; then
  log "Ensuring cuttlefish-operator is enabled and running"
  sudo systemctl enable --now cuttlefish-operator || warn "Could not start cuttlefish-operator."
else
  warn "cuttlefish-operator service not found; install Cuttlefish host tools for the device console."
fi

# 6. Optional: enable live cvd execution ------------------------------------
if [[ "${OPENCUTTLES_ENABLE_EXECUTE_CVD:-0}" == "1" ]]; then
  env_file="/etc/opencuttles/opencuttles.env"
  if [[ -f "$env_file" ]]; then
    log "Enabling live Cuttlefish execution (OPENCUTTLES_EXECUTE_CVD=1)"
    if sudo grep -q '^OPENCUTTLES_EXECUTE_CVD=' "$env_file"; then
      sudo sed -i 's/^OPENCUTTLES_EXECUTE_CVD=.*/OPENCUTTLES_EXECUTE_CVD=1/' "$env_file"
    else
      echo 'OPENCUTTLES_EXECUTE_CVD=1' | sudo tee -a "$env_file" >/dev/null
    fi
    sudo systemctl restart opencuttles-api
  else
    warn "${env_file} not found; cannot set OPENCUTTLES_EXECUTE_CVD."
  fi
fi

# 7. Health check ------------------------------------------------------------
log "Waiting for the API to become healthy"
healthy=0
for _ in $(seq 1 20); do
  if curl -fsS "$health_url" >/dev/null 2>&1; then
    healthy=1
    break
  fi
  sleep 1
done

if [[ "$healthy" == "1" ]]; then
  log "Update complete and API is healthy."
else
  warn "API did not pass health check at ${health_url}."
  warn "Inspect logs with: journalctl -u opencuttles-api -n 100 --no-pager"
  exit 1
fi
