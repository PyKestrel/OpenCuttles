# Ubuntu Deployment Guide

This guide describes the intended single-host MVP deployment. It assumes an
Ubuntu Server VM with nested virtualization enabled.

## Quickstart

For a new Ubuntu Server host, run:

```bash
OPENCUTTLES_HOSTNAME=testral.cloud bash scripts/ubuntu/quickstart.sh
```

The script installs common host dependencies, installs Go 1.25 and Node.js 22
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
**Everything defaults to HTTPS.** A fully qualified domain gets an ACME
certificate from Caddy; a single-label hostname or IP address gets a self-signed
certificate plus a published pin (see *TLS and desktop runners* below). This used
to fall back to plain HTTP for those, which made plaintext the default for the
most common install. `OPENCUTTLES_HTTP=1` still opts out, for throwaway
appliances only.
Set `OPENCUTTLES_ALLOWED_ORIGIN=https://your.domain.example` to force the exact
browser origin.
Enable HSTS only for real HTTPS domains. Do not send HSTS for local names such as
`opencuttles`, because browsers can cache it and force future HTTP requests to
HTTPS.

## 1. Prepare the host

Install Cuttlefish, Android platform tools, Go 1.25+, Node.js 22+, Caddy, UFW, sqlite3,
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

Testral ships as a single binary with the dashboard embedded. `make package`
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
OPENCUTTLES_TRUST_PROXY_HEADERS=1
OPENCUTTLES_EXECUTE_CVD=1
OPENCUTTLES_IMAGE_ROOT=/var/lib/opencuttles/images
OPENCUTTLES_DEFAULT_IMAGE_PATH=/var/lib/opencuttles/images/default
```

### TLS and desktop runners

Desktop runners refuse plaintext. This channel carries device control **and**
build artifacts that the runner downloads and executes, so a MITM on `http://`
means code execution on the target machine, not just eavesdropping.

* **Real domain** — Caddy obtains a certificate via ACME. Runners verify against
  the system trust store and need no pin. Nothing extra to do.
* **IP address or single-label hostname** — no public CA will issue for those, so
  `quickstart.sh` mints a self-signed certificate via
  `scripts/ubuntu/ensure-tls.sh` and publishes its public-key pin as
  `OPENCUTTLES_TLS_PIN`. The dashboard embeds that pin in the install command it
  shows for each device, and the runner authenticates the appliance by it.

The pin is **not a secret** — it identifies the appliance; it does not
authenticate to it. It covers the public key rather than the certificate, so
re-issuing with the same key keeps every enrolled runner working.

```bash
bash scripts/ubuntu/ensure-tls.sh 10.1.0.104     # idempotent; prints the pin
```

> Regenerating the certificate (`--force`, or deleting `/etc/opencuttles/tls`)
> invalidates the pin every enrolled runner holds, and they will refuse to
> reconnect until re-enrolled with the new one. The script will not do it
> accidentally.

`OPENCUTTLES_HTTP=1` still forces plaintext for throwaway development
appliances. Runners then need `--insecure`, which they warn about on every
start. Never use it for a device that matters.

Build artifacts are hashed on upload and the hash is served with the download;
the runner verifies it before executing the file. Artifacts uploaded before this
existed have no hash — the runner logs that it cannot verify them rather than
failing.

### Optional: mutual TLS for runners

**Off by default.** An enrollment token is a bearer credential — replayable by
anyone who observes it. A client certificate adds proof-of-possession, so an
attacker also needs a private key that never leaves the enrolled machine.

```bash
OPENCUTTLES_RUNNER_MTLS_LISTEN=0.0.0.0:8443   # enables it; empty = off
OPENCUTTLES_RUNNER_MTLS_URL=                  # override if port-forwarded
```

The appliance mints its own CA on first use and stores the key encrypted with
`OPENCUTTLES_SECRET_KEY` — which is therefore required. Enrolling or rotating a
device then also issues it a client certificate, which the dashboard hands over
with the token.

The certificate is verified by the API process on its own TLS listener, not by
Caddy with the identity forwarded in a header. Trusting a header for
authentication is the same shape as the `X-Forwarded-For` spoofing this codebase
already had to fix, and it would hold only while the backend port stayed
unreachable. The listener reuses the appliance certificate
(`OPENCUTTLES_TLS_CERT` / `OPENCUTTLES_TLS_KEY`), so runners verify it with the
pin they already hold. Open the port in your firewall.

> **Enabling this disconnects every existing runner.** Once on, the
> Caddy-fronted path refuses runner traffic outright — otherwise an attacker
> with a stolen token would simply use that port and the certificate requirement
> would be decorative. Each device must be re-enrolled (or have its token
> rotated) to receive a certificate. Plan the cutover.

Revocation needs no certificate machinery: the appliance checks the device
record on every connection, so revoking a device stops its certificate working
immediately. Certificates are also short-lived, so a lost machine's key expires
on its own.

### Secrets

Three values in that file are real credentials:

| Variable | Grants |
| --- | --- |
| `OPENCUTTLES_MCP_TOKEN` | the full MCP tool surface — click/type/screenshot on every device, no session — plus `GET /api/v1/agent/runtime`, which returns the **decrypted** provider API key |
| `OPENCUTTLES_BOOTSTRAP_TOKEN` | claims the first admin account |
| `OPENCUTTLES_SECRET_KEY` | encrypts stored provider API keys and desktop runner tokens at rest |

`install.sh` and `quickstart.sh` generate all three via
`scripts/ubuntu/ensure-secrets.sh`. They ship **empty** in
`opencuttles.env.example` so a missed generation fails closed rather than
installing a known credential. The helper is idempotent, so it is safe to re-run
on an existing appliance to fill in whatever is missing:

```bash
bash scripts/ubuntu/ensure-secrets.sh
sudo systemctl restart opencuttles-api
```

**Upgrading an existing appliance:** the helper only fills in empty or
`change-this…` placeholder values, so an appliance installed before this change
keeps whatever it already has — including the old shipped default MCP token.
Rotate anything that was ever a shipped default, by hand:

```bash
bash scripts/ubuntu/ensure-secrets.sh --rotate OPENCUTTLES_MCP_TOKEN
sudo systemctl restart opencuttles-api
```

Then update the token in the agent sidecar's own config so it can reconnect.

> Do **not** rotate `OPENCUTTLES_SECRET_KEY` casually: every already-encrypted
> value becomes undecryptable, so stored provider keys must be re-entered and
> every desktop device must be re-enrolled. Back this key up.

Claiming the first admin without a bootstrap token is possible only under
`OPENCUTTLES_DEV_MODE=1`, which is set solely by `scripts/dev/start.sh`. Never
set it on a deployed appliance: with no token configured it would let anyone who
can reach the port claim the admin account before you do.

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
the `OPENCUTTLES_BOOTSTRAP_TOKEN` value that `ensure-secrets.sh` generated
(`quickstart.sh` prints it at the end), then remove the token from
`/etc/opencuttles/opencuttles.env` after the admin exists.

## Interactive console (WebRTC)

The device console is Cuttlefish's built-in WebRTC stream served by the host-wide
`cuttlefish-operator`. Current Cuttlefish serves this on HTTPS `:1443` (older
builds used `:8443`); set `OPENCUTTLES_OPERATOR_PORT` if your build differs.
Testral reverse-proxies it per instance at
`/api/v1/instances/<id>/console/...` (reusing Testral auth and TLS), so the
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

`install.sh` enables `opencuttles-backup.timer`, which snapshots nightly at
03:30 into `/var/backups/opencuttles` and keeps the newest 14
(`OPENCUTTLES_BACKUP_KEEP`). To take one by hand, or to restore:

```bash
bash scripts/ubuntu/backup.sh
bash scripts/ubuntu/restore.sh /var/backups/opencuttles/opencuttles-YYYYmmddTHHMMSSZ

systemctl list-timers opencuttles-backup.timer
journalctl -u opencuttles-backup.service
```

A snapshot holds `opencuttles.db` (via `sqlite3 .backup`, the only consistent
way to copy a live WAL database), `config.tar.gz`, and `artifacts.tar.gz` unless
`OPENCUTTLES_SKIP_ARTIFACTS=1`. Everything is checksummed in `SHA256SUMS`, and
`restore.sh` refuses to proceed without it.

Restore replaces the database and config, and verifies `PRAGMA integrity_check`
before starting the API. Test evidence is **not** restored by default, since it
replaces the whole artifact tree — pass `OPENCUTTLES_RESTORE_ARTIFACTS=1` for
that.

> `config.tar.gz` contains `opencuttles.env` — the MCP token and
> `OPENCUTTLES_SECRET_KEY`. Treat snapshots as secrets. It is also what makes
> them restorable: without `SECRET_KEY`, every stored provider key and runner
> token in the database is undecryptable.

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
