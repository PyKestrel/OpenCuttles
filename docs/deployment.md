# Ubuntu Deployment Guide

This guide describes the intended single-host MVP deployment. It assumes an
Ubuntu Server VM with nested virtualization enabled.

## Quickstart

For a new Ubuntu Server host, run:

```bash
OPENCUTTLES_HOSTNAME=opencuttles.example.com bash scripts/ubuntu/quickstart.sh
```

The script installs common host dependencies, installs Go 1.23 and Node.js 22
when needed, installs `adb`, installs the Google Cuttlefish host packages if they
are missing, prepares Go/npm dependencies, builds a single-binary package,
installs the systemd/Caddy assets, generates a one-time bootstrap token, starts
the API, and prints the dashboard URL.

Cuttlefish is installed from prebuilt packages by default (Google Artifact
Registry, then the latest GitHub release `.deb` assets) - seconds, not hours, and
almost no disk. The slow Bazel source build is opt-in via
`OPENCUTTLES_BUILD_CUTTLEFISH_FROM_SOURCE=1` and is disk-gated (it needs ~40 GB).

Set `OPENCUTTLES_CONFIGURE_FIREWALL=1` to let quickstart apply the bundled UFW
rules.
Set `OPENCUTTLES_SKIP_CUTTLEFISH_INSTALL=1` to skip Cuttlefish entirely and run
only the dashboard/API in dry-run mode.
Set `OPENCUTTLES_PREPARE_DEFAULT_IMAGE=1` to download and unpack the default
Cuttlefish image under `/var/lib/opencuttles/images/default`. By default this
uses `cvd fetch --default_build=aosp-android-latest-release/aosp_cf_x86_64_only_phone-userdebug`.
The stable release branch is used because the `aosp-main` tip can transiently
lack the device `-img-` artifact on its newest build. Note the build-target name
differs by branch: `aosp-main` uses `aosp_cf_x86_64_phone`, while the release and
GSI branches use `aosp_cf_x86_64_only_phone`.
Single-label hostnames and IP addresses default to HTTP, for example
`OPENCUTTLES_HOSTNAME=opencuttles`. Fully qualified domains default to HTTPS.
Set `OPENCUTTLES_ALLOWED_ORIGIN=https://your.domain.example` to force the exact
browser origin.
Enable HSTS only for real HTTPS domains. Do not send HSTS for local names such as
`opencuttles`, because browsers can cache it and force future HTTP requests to
HTTPS.

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

> **Ubuntu 23.10+/24.04:** crosvm sandboxes its device processes with minijail,
> which needs unprivileged user namespaces. These distros restrict them by
> default, causing `VIRTUAL_DEVICE_BOOT_FAILED` with
> `unshare(CLONE_NEWNS): Operation not permitted` in the launcher log.
> `install-android-tools.sh` sets the required sysctls; to apply manually:
>
> ```bash
> echo 'kernel.apparmor_restrict_unprivileged_userns=0' | sudo tee /etc/sysctl.d/60-cuttlefish-userns.conf
> echo 'kernel.unprivileged_userns_clone=1' | sudo tee /etc/sysctl.d/61-cuttlefish-userns.conf
> sudo sysctl --system
> ```

Run the readiness check:

```bash
bash scripts/ubuntu/check-host.sh
```

## 2. Build artifacts

OpenCuttles ships as a single binary with the dashboard embedded. `make package`
builds the frontend, stages it into the embed directory, builds the binary, and
assembles the release under `dist/package`:

```bash
make package
```

There is no separate frontend bundle to deploy; the binary serves the SPA, the
API, and the device-console proxy on one port.

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
OPENCUTTLES_DEFAULT_IMAGE_PATH=/var/lib/opencuttles/images/default
```

The Caddy config is installed as `/etc/caddy/conf.d/opencuttles.caddy` and the
installer appends an import line to `/etc/caddy/Caddyfile` if needed. Review the
site hostname in both Caddy and `OPENCUTTLES_ALLOWED_ORIGIN` before first login.
Place the default Cuttlefish image bundle under
`/var/lib/opencuttles/images/default` to enable one-click instance creation
without registering an image first.
You can prepare it automatically with:

```bash
OPENCUTTLES_PREPARE_DEFAULT_IMAGE=1 bash scripts/ubuntu/quickstart.sh
```

If `cvd fetch` is unavailable in your Cuttlefish version, provide explicit
artifact URLs from the same Android CI build:

```bash
OPENCUTTLES_CVD_IMAGE_URL=https://.../aosp_cf_x86_64_phone-img-xxxxxx.zip \
OPENCUTTLES_CVD_HOST_PACKAGE_URL=https://.../cvd-host_package.tar.gz \
bash scripts/ubuntu/prepare-default-image.sh
```

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

## Interactive console (WebRTC)

The device console is Cuttlefish's built-in WebRTC stream served by the host-wide
`cuttlefish-operator`. Current Cuttlefish serves this on HTTPS `:1443` (older
builds used `:8443`); set `OPENCUTTLES_OPERATOR_PORT` if your build differs.
OpenCuttles reverse-proxies it per instance at
`/api/v1/instances/<id>/console/...` (reusing OpenCuttles auth and TLS), so the
operator port itself stays host-local. Browsers connect to the media stream
directly over `TCP/UDP 15550-15599`, which `scripts/ubuntu/firewall.sh` opens.

- Make sure the `cuttlefish-operator` service is enabled (the Android tools
  installer does this automatically). Confirm the port with
  `sudo ss -ltnp | grep operator`.
- The console only shows a device once a real Cuttlefish instance is actually
  launched (`OPENCUTTLES_EXECUTE_CVD=1` and the device booted). In dry-run mode
  no device is registered with the operator.
- This default works for LAN/VPN access where the browser can reach the host's
  `15550-15599` range. For browsers behind arbitrary NAT/firewalls, deploy a TURN
  relay (e.g. `coturn`) and point the operator at it. TURN is an optional
  follow-up and is not bundled.

## Image storage and disk

Each Android version is fetched on first deploy with `cvd fetch` into
`OPENCUTTLES_IMAGE_ROOT/<versionId>` and is several GB. Provision the image
volume accordingly (the host check warns when `/var/lib` has < 50 GB free) and
expect the first deploy of a new version to take several minutes while the image
downloads. Subsequent instances of the same version reuse the cached image.

## Backup and restore

```bash
bash scripts/ubuntu/backup.sh
bash scripts/ubuntu/restore.sh /var/backups/opencuttles/opencuttles-YYYYmmddTHHMMSSZ
```

## Upgrade and rollback

One-command update (pull, package, rollback-safe upgrade, firewall, operator,
health check):

```bash
bash scripts/ubuntu/update.sh   # or: make update
```

Manual upgrade/rollback:

```bash
bash scripts/ubuntu/upgrade.sh dist/package
bash scripts/ubuntu/rollback.sh /var/backups/opencuttles/rollback-YYYYmmddTHHMMSSZ
```
