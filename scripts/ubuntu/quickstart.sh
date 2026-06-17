#!/usr/bin/env bash
set -euo pipefail

root_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
hostname="${OPENCUTTLES_HOSTNAME:-$(hostname -f 2>/dev/null || hostname)}"
origin="${OPENCUTTLES_ALLOWED_ORIGIN:-https://${hostname}}"
env_file="/etc/opencuttles/opencuttles.env"
go_version="${OPENCUTTLES_GO_VERSION:-1.23.10}"

if [[ "$(uname -s)" != "Linux" ]]; then
  echo "This quickstart is intended for Ubuntu Server." >&2
  exit 1
fi

echo "Installing host dependencies..."
sudo apt-get update
sudo apt-get install -y ca-certificates curl git make rsync sqlite3 ufw caddy nodejs npm tar
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

echo "Installing Android virtualization tools..."
bash "${root_dir}/scripts/ubuntu/install-android-tools.sh"

echo "Checking host readiness..."
if ! bash "${root_dir}/scripts/ubuntu/check-host.sh"; then
  echo
  echo "Continuing setup. Missing Cuttlefish/ADB tools only block live Android instance launch."
  echo "The dashboard and API will start with OPENCUTTLES_EXECUTE_CVD=0 until you install them."
fi

echo "Preparing reproducible dependency files..."
(cd "${root_dir}/backend" && go mod tidy)
(
  cd "${root_dir}/frontend"
  if ! npm install; then
    echo "npm install failed; removing stale lockfile and retrying once..."
    rm -f package-lock.json
    npm install
  fi
)

echo "Building OpenCuttles package..."
(cd "${root_dir}" && make package)

echo "Installing OpenCuttles services..."
bash "${root_dir}/scripts/ubuntu/install.sh" "${root_dir}/dist/package"

if sudo test ! -s "$env_file"; then
  sudo install -m 0640 "${root_dir}/deploy/systemd/opencuttles.env.example" "$env_file"
fi

bootstrap_token="$(openssl rand -hex 24 2>/dev/null || date +%s%N)"
sudo sed -i \
  -e "s#^OPENCUTTLES_ALLOWED_ORIGIN=.*#OPENCUTTLES_ALLOWED_ORIGIN=${origin}#" \
  -e "s#^OPENCUTTLES_BOOTSTRAP_TOKEN=.*#OPENCUTTLES_BOOTSTRAP_TOKEN=${bootstrap_token}#" \
  -e "s#^OPENCUTTLES_SECURE_COOKIES=.*#OPENCUTTLES_SECURE_COOKIES=1#" \
  -e "s#^OPENCUTTLES_TRUST_PROXY_HEADERS=.*#OPENCUTTLES_TRUST_PROXY_HEADERS=1#" \
  "$env_file"

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
