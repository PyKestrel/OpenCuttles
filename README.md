# Testral

Testral is a control plane for **agentic UI testing across operating systems**.
A local cognitive-core agent, grounded by a lightweight vision model, drives real
devices in natural language and runs replayable, self-healing UI tests — on
**Android** (Google Cuttlefish VMs on the host) and on **Windows / Linux / macOS**
desktops (onboarded via a dial-home runner). One dashboard, with the operational
feel of Google Cloud Console and VMware vCenter.

Android devices are provisioned and controlled over ADB; desktop targets run a
small runner that connects outbound to the appliance (no inbound ports) and
exposes screenshot + input so the same vision grounding and test runner work
everywhere. Testral runs on a single Ubuntu Server host with KVM and provides the
API, inventory, lifecycle tracking, health checks, deployment templates, and
dashboard.

## MVP Scope

- Manage one Ubuntu Server host.
- Bootstrap a local admin and protect the API/dashboard with secure sessions.
- Use an OIDC-ready RBAC model with admin, operator, and viewer roles.
- Automatically register the default Android image for one-click instance creation.
- Optionally register additional Android images that can be launched by Cuttlefish.
- Create, start, stop, and delete local Cuttlefish instances.
- Track lifecycle state, allocated ports, capacity, health, and operation
  history in SQLite.
- Use authenticated, instance-bound Cuttlefish native WebRTC console access.
- Keep ADB local to the host by default.
- Provide systemd, reverse proxy, firewall, backup/restore, install, upgrade,
  rollback, and uninstall assets for production single-host installs.

Future releases can add ws-scrcpy, multi-host agents, RBAC, image upload
workflows, and richer networking isolation.

## Repository Layout

```text
backend/          Go API, SQLite store, Cuttlefish orchestration adapters
frontend/         React + TypeScript dashboard
deploy/proxy/     Caddy reverse proxy template
deploy/systemd/   systemd unit templates
docs/             Architecture and acceptance documentation
scripts/ubuntu/   Ubuntu host readiness checks
```

## Host Requirements

The target Ubuntu Server VM should provide:

- Ubuntu Server 22.04 LTS or 24.04 LTS.
- Nested virtualization enabled on the physical hypervisor.
- `/dev/kvm` available to the `opencuttles` service user.
- Google Cuttlefish host tools installed and on `PATH`.
- Android platform tools with `adb` installed and on `PATH`.
- Enough RAM, CPU, and disk for the desired number of Android instances.

Run the readiness check on the target host:

```bash
bash scripts/ubuntu/check-host.sh
```

## Fast Start

Local development on Windows:

```powershell
powershell -ExecutionPolicy Bypass -File scripts\dev\setup.ps1
powershell -ExecutionPolicy Bypass -File scripts\dev\start.ps1
```

Local development on Linux/macOS:

```bash
bash scripts/dev/setup.sh
bash scripts/dev/start.sh
```

Then open `http://localhost:5173`. The dev server stores data in `data/dev`,
runs in dry-run Cuttlefish mode, and does not require a bootstrap token.

One-command Ubuntu Server quickstart:

```bash
bash scripts/ubuntu/quickstart.sh
```

Set `OPENCUTTLES_HOSTNAME=opencuttles.example.com` before running quickstart to
control the Caddy hostname and allowed browser origin.
Single-label hostnames and IP addresses, such as `opencuttles` or `192.168.1.50`,
default to plain HTTP to avoid local TLS certificate errors. In HTTP mode the
reverse proxy listens on every host on port 80, so the dashboard is reachable by
the machine's IP **and** its hostname without extra configuration (e.g.
`http://192.168.1.50/`). Use `OPENCUTTLES_ALLOWED_ORIGIN=https://your.domain.example`
for a real HTTPS hostname, which switches the proxy to a TLS-enabled,
host-specific block.
Quickstart installs Go 1.23 and Node.js 22 when the host versions are missing or
too old.
Quickstart installs `adb` and, if missing, the Google Cuttlefish host packages.
By default it installs the prebuilt `cuttlefish-base`/`cuttlefish-user` packages
(Google Artifact Registry first, then the latest GitHub release `.deb` assets),
which take seconds and almost no disk. Set
`OPENCUTTLES_BUILD_CUTTLEFISH_FROM_SOURCE=1` to fall back to the slow (~40 GB)
Bazel source build, or `OPENCUTTLES_SKIP_CUTTLEFISH_INSTALL=1` to start only the
dashboard/API in dry-run mode.
Set `OPENCUTTLES_PREPARE_DEFAULT_IMAGE=1` to fetch and unpack a default
Cuttlefish image into `/var/lib/opencuttles/images/default` using `cvd fetch`.
Override the build with `OPENCUTTLES_CVD_BUILD`, for example
`aosp-android-latest-release/aosp_cf_x86_64_only_phone-userdebug` (the default).
Build-target names differ by branch: `aosp-main` uses `aosp_cf_x86_64_phone`,
while release/GSI branches use `aosp_cf_x86_64_only_phone`.

## Local Development

Backend:

```bash
cd backend
go mod tidy
go test ./...
OPENCUTTLES_SECURE_COOKIES=0 go run ./cmd/opencuttles-api
```

Frontend:

```bash
cd frontend
npm install
npm run dev
```

The frontend dev server proxies API calls to `http://localhost:8080`.

## Deployment Sketch

Testral builds into a single Go binary with the dashboard embedded, so
deployment is one artifact. `make package` builds the frontend, embeds it into
the binary, and stages the release.

1. Prepare the Ubuntu Server VM with KVM, Cuttlefish, ADB, and a service user.
2. Run `make package` (builds frontend, embeds it, builds the binary).
3. Install with `bash scripts/ubuntu/install.sh dist/package`.
4. Configure `/etc/opencuttles/opencuttles.env`.
5. Apply the firewall with `bash scripts/ubuntu/firewall.sh`.
6. Open the dashboard and bootstrap the first local admin user.

Caddy only terminates TLS and reverse-proxies everything to the binary on
`127.0.0.1:8080`; there is no separate static-asset deployment.

## Updating

Pull, rebuild, and roll out in one command on the host:

```bash
bash scripts/ubuntu/update.sh   # or: make update
```

It pulls, packages, runs the rollback-safe upgrade, refreshes the firewall,
ensures the WebRTC operator is running, and health-checks the API. Schema
migrations apply automatically on restart.

Production builds require `backend/go.sum` and `frontend/package-lock.json`.
Generate them with `go mod tidy` and `npm install` before packaging.

See `docs/architecture.md` and `docs/acceptance.md` for the detailed control
plane model and validation checklist.
