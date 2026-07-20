#!/usr/bin/env bash
set -euo pipefail

root_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
hostname="${OPENCUTTLES_HOSTNAME:-$(hostname -f 2>/dev/null || hostname)}"
env_file="/etc/opencuttles/opencuttles.env"
go_version="${OPENCUTTLES_GO_VERSION:-1.23.10}"
node_major="${OPENCUTTLES_NODE_MAJOR:-22}"

is_ip_address() {
  [[ "$1" =~ ^[0-9]+\.[0-9]+\.[0-9]+\.[0-9]+$ ]]
}

if [[ -n "${OPENCUTTLES_ALLOWED_ORIGIN:-}" ]]; then
  origin="$OPENCUTTLES_ALLOWED_ORIGIN"
elif [[ "${OPENCUTTLES_HTTP:-0}" == "1" || "$hostname" != *.* ]] || is_ip_address "$hostname"; then
  origin="http://${hostname}"
else
  origin="https://${hostname}"
fi

# For HTTP (local hostnames / IP addresses) bind Caddy to all hosts on :80 so
# the dashboard is reachable by hostname OR by the machine's IP. For a real
# HTTPS domain, keep a host-specific block so automatic TLS works.
if [[ "$origin" == http://* ]]; then
  site_address=":80"
  secure_cookies="0"
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
if [[ -z "$go_bin" ]] || ! "$go_bin" version | grep -Eq 'go1\.(23|24|25)'; then
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
