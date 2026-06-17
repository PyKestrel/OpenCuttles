# Ubuntu Deployment Guide

This guide describes the intended single-host MVP deployment. It assumes an
Ubuntu Server VM with nested virtualization enabled.

## Quickstart

For a new Ubuntu Server host, run:

```bash
OPENCUTTLES_HOSTNAME=opencuttles.example.com bash scripts/ubuntu/quickstart.sh
```

The script installs common host dependencies, installs Go 1.23 and Node.js 22
when needed, installs `adb`, builds and installs Google Cuttlefish host packages
if they are missing, prepares Go/npm dependencies, builds a package, installs the
systemd/Caddy assets, generates a one-time bootstrap token, starts the API, and
prints the dashboard URL.

Set `OPENCUTTLES_CONFIGURE_FIREWALL=1` to let quickstart apply the bundled UFW
rules.
Set `OPENCUTTLES_SKIP_CUTTLEFISH_INSTALL=1` to skip the Cuttlefish build and run
only the dashboard/API in dry-run mode.

## 1. Prepare the host

Install Cuttlefish, Android platform tools, Go 1.23+, Node.js 20+, Caddy, UFW, sqlite3,
and rsync using your standard package source, or run:

```bash
bash scripts/ubuntu/install-android-tools.sh
```

Then create the service user:

```bash
sudo useradd --system --create-home --home-dir /var/lib/opencuttles --shell /usr/sbin/nologin opencuttles
sudo usermod -aG kvm opencuttles
sudo install -d -o opencuttles -g opencuttles /var/lib/opencuttles /var/log/opencuttles /opt/opencuttles/bin
```

Run the readiness check:

```bash
bash scripts/ubuntu/check-host.sh
```

## 2. Build artifacts

Backend:

```bash
cd backend
go build -o ../dist/opencuttles-api ./cmd/opencuttles-api
```

Frontend:

```bash
cd frontend
npm install
npm run build
```

Package:

```bash
make package
```

## 3. Install artifacts

```bash
bash scripts/ubuntu/install.sh dist/package
```

Set `OPENCUTTLES_EXECUTE_CVD=1` in the systemd unit only after Cuttlefish launch
has been validated manually on the host.

Production configuration lives in `/etc/opencuttles/opencuttles.env`. At
minimum, set:

```bash
OPENCUTTLES_ALLOWED_ORIGIN=https://your-opencuttles-hostname
OPENCUTTLES_SECURE_COOKIES=1
OPENCUTTLES_BOOTSTRAP_TOKEN=generate-a-long-random-one-time-token
OPENCUTTLES_TRUST_PROXY_HEADERS=1
OPENCUTTLES_EXECUTE_CVD=1
OPENCUTTLES_IMAGE_ROOT=/var/lib/opencuttles/images
```

The Caddy config is installed as `/etc/caddy/conf.d/opencuttles.caddy` and the
installer appends an import line to `/etc/caddy/Caddyfile` if needed. Review the
site hostname in both Caddy and `OPENCUTTLES_ALLOWED_ORIGIN` before first login.

## 4. Start services

```bash
sudo systemctl daemon-reload
sudo systemctl enable --now opencuttles-api
sudo systemctl reload caddy
bash scripts/ubuntu/firewall.sh
```

## 5. Validate

```bash
curl http://127.0.0.1:8080/api/v1/healthz
journalctl -u opencuttles-api -f
```

Open the Caddy hostname in a browser, bootstrap the first local admin user using
`OPENCUTTLES_BOOTSTRAP_TOKEN`, then rotate or remove the token from
`/etc/opencuttles/opencuttles.env` after the admin exists.

## Backup and restore

```bash
bash scripts/ubuntu/backup.sh
bash scripts/ubuntu/restore.sh /var/backups/opencuttles/opencuttles-YYYYmmddTHHMMSSZ
```

## Upgrade and rollback

```bash
bash scripts/ubuntu/upgrade.sh dist/package
bash scripts/ubuntu/rollback.sh /var/backups/opencuttles/rollback-YYYYmmddTHHMMSSZ
```
