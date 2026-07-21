#!/usr/bin/env bash
set -euo pipefail

root_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
hostname="${OPENCUTTLES_HOSTNAME:-$(hostname -f 2>/dev/null || hostname)}"
env_file="/etc/opencuttles/opencuttles.env"
# Must satisfy backend/go.mod's `go 1.25.0`. This was pinned at 1.23.10, which
# does not: go.mod has no `toolchain` line, so a 1.23 install had to download the
# 1.25 toolchain mid-build — a surprise network dependency on an appliance that
# may be firewalled off from proxy.golang.org.
go_version="${OPENCUTTLES_GO_VERSION:-1.25.12}"
node_major="${OPENCUTTLES_NODE_MAJOR:-22}"

is_ip_address() {
  [[ "$1" =~ ^[0-9]+\.[0-9]+\.[0-9]+\.[0-9]+$ ]]
}

# Everything is HTTPS unless the operator explicitly asks for plaintext.
#
# This used to fall back to http:// for any IP address or single-label hostname,
# because no public CA will issue a certificate for those. That made plaintext
# the default for the most common install — and desktop runners then carried
# device control and executable build artifacts over it. Those appliances now
# get a self-signed certificate plus a published key pin instead (see
# ensure-tls.sh); runners authenticate the appliance by that pin.
self_signed_tls=0
if [[ -n "${OPENCUTTLES_ALLOWED_ORIGIN:-}" ]]; then
  origin="$OPENCUTTLES_ALLOWED_ORIGIN"
  if [[ "$origin" != http://* ]] && { [[ "$hostname" != *.* ]] || is_ip_address "$hostname"; }; then
    self_signed_tls=1
  fi
elif [[ "${OPENCUTTLES_HTTP:-0}" == "1" ]]; then
  # Explicit opt-out, for throwaway/dev appliances only.
  origin="http://${hostname}"
elif [[ "$hostname" != *.* ]] || is_ip_address "$hostname"; then
  origin="https://${hostname}"
  self_signed_tls=1
else
  origin="https://${hostname}"
fi

# Plain HTTP binds every host on :80 so the dashboard is reachable by hostname
# or IP. HTTPS uses a host-specific block: a real FQDN gets automatic ACME, and
# an IP/local hostname gets the self-signed certificate written above.
if [[ "$origin" == http://* ]]; then
  site_address=":80"
  secure_cookies="0"
  echo
  echo "WARNING: OPENCUTTLES_HTTP=1 — the dashboard and every desktop runner will"
  echo "         use plaintext. Runners refuse plaintext unless installed with"
  echo "         --insecure. Do not use this for real devices."
  echo
else
  site_address="$origin"
  secure_cookies="1"
fi

if [[ "$(uname -s)" != "Linux" ]]; then
  echo "This quickstart is intended for Ubuntu Server." >&2
  exit 1
fi

echo "Installing host dependencies..."
sudo apt-get update
sudo apt-get install -y ca-certificates curl git make rsync sqlite3 ufw caddy tar
sudo useradd --system --create-home --home-dir /var/lib/opencuttles --shell /usr/sbin/nologin opencuttles 2>/dev/null || true

go_bin="$(command -v go || true)"
# Accept an existing Go only if it can build backend/go.mod (>= 1.25) on its
# own; 1.23/1.24 were accepted here but would have to fetch a newer toolchain.
if [[ -z "$go_bin" ]] || ! "$go_bin" version | grep -Eq 'go1\.(2[5-9]|[3-9][0-9])'; then
  arch="$(dpkg --print-architecture)"
  case "$arch" in
    amd64) go_arch="amd64" ;;
    arm64) go_arch="arm64" ;;
    *) echo "Unsupported architecture for Go quick install: ${arch}" >&2; exit 1 ;;
  esac
  echo "Installing Go ${go_version} for linux/${go_arch}..."
  curl -fsSL "https://go.dev/dl/go${go_version}.linux-${go_arch}.tar.gz" -o /tmp/opencuttles-go.tgz
  sudo rm -rf /usr/local/go
  sudo tar -C /usr/local -xzf /tmp/opencuttles-go.tgz
  export PATH="/usr/local/go/bin:${PATH}"
else
  export PATH="$(dirname "$go_bin"):${PATH}"
fi

echo "Using $(go version)"

node_bin="$(command -v node || true)"
if [[ -z "$node_bin" ]] || ! "$node_bin" -v | grep -Eq '^v(2[0-9]|[3-9][0-9])\.'; then
  echo "Installing Node.js ${node_major}.x..."
  curl -fsSL "https://deb.nodesource.com/setup_${node_major}.x" | sudo -E bash -
  sudo apt-get install -y nodejs
else
  echo "Using $(node -v) and npm $(npm -v)"
fi

echo "Using Node $(node -v) and npm $(npm -v)"

echo "Installing Android virtualization tools..."
bash "${root_dir}/scripts/ubuntu/install-android-tools.sh"

if [[ "${OPENCUTTLES_PREPARE_DEFAULT_IMAGE:-0}" == "1" ]]; then
  echo "Preparing default Cuttlefish image..."
  OPENCUTTLES_DEFAULT_IMAGE_PATH="${OPENCUTTLES_DEFAULT_IMAGE_PATH:-/var/lib/opencuttles/images/default}" \
    bash "${root_dir}/scripts/ubuntu/prepare-default-image.sh"
fi

echo "Checking host readiness..."
if ! bash "${root_dir}/scripts/ubuntu/check-host.sh"; then
  echo
  echo "Continuing setup. Missing Cuttlefish/ADB tools only block live Android instance launch."
  echo "The dashboard and API will start with OPENCUTTLES_EXECUTE_CVD=0 until you install them."
fi

echo "Preparing Go dependencies..."
(cd "${root_dir}/backend" && go mod tidy)

# 'make package' builds the frontend, embeds it into the Go binary, and stages
# the single-artifact release. It installs npm deps itself (npm ci, falling back
# to npm install), so there is no separate frontend install step here.
echo "Building OpenCuttles package (frontend is embedded into the binary)..."
(cd "${root_dir}" && make package)

echo "Installing OpenCuttles services..."
bash "${root_dir}/scripts/ubuntu/install.sh" "${root_dir}/dist/package"

if sudo test -f /etc/caddy/conf.d/opencuttles.caddy; then
  sudo sed -i "1s#.*#${site_address} {#" /etc/caddy/conf.d/opencuttles.caddy
fi

if sudo test ! -s "$env_file"; then
  sudo install -m 0640 "${root_dir}/deploy/systemd/opencuttles.env.example" "$env_file"
fi

sudo sed -i \
  -e "s#^OPENCUTTLES_ALLOWED_ORIGIN=.*#OPENCUTTLES_ALLOWED_ORIGIN=${origin}#" \
  -e "s#^OPENCUTTLES_SECURE_COOKIES=.*#OPENCUTTLES_SECURE_COOKIES=${secure_cookies}#" \
  -e "s#^OPENCUTTLES_TRUST_PROXY_HEADERS=.*#OPENCUTTLES_TRUST_PROXY_HEADERS=1#" \
  "$env_file"

# Generate every secret (bootstrap + MCP + at-rest key), not just the bootstrap
# token — a shipped default is a publicly-known credential. install.sh already
# ran this; re-running is a no-op for values it set.
bash "${root_dir}/scripts/ubuntu/ensure-secrets.sh" "$env_file"
bootstrap_token="$(sudo sed -n 's#^OPENCUTTLES_BOOTSTRAP_TOKEN=##p' "$env_file" | head -1)"

# An appliance reached by IP or a single-label hostname cannot get a public
# certificate, so mint a self-signed one and publish its pin. Idempotent: an
# existing certificate is left alone, because regenerating it would invalidate
# the pin every enrolled runner already holds.
if [[ "$self_signed_tls" == "1" ]]; then
  bash "${root_dir}/scripts/ubuntu/ensure-tls.sh" "$hostname" "$env_file"
  if sudo test -f /etc/caddy/conf.d/opencuttles.caddy; then
    # Point Caddy at the certificate. Any previous tls line is removed first so
    # re-running quickstart replaces it rather than stacking duplicates, which
    # Caddy would reject.
    sudo sed -i '\#^[[:space:]]*tls /etc/opencuttles/tls/#d' /etc/caddy/conf.d/opencuttles.caddy
    sudo sed -i '1a\    tls /etc/opencuttles/tls/appliance.crt /etc/opencuttles/tls/appliance.key' \
      /etc/caddy/conf.d/opencuttles.caddy
  fi
fi

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
