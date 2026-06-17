# OpenCuttles

OpenCuttles is a single-host Android virtualization control plane for Ubuntu
Server. It manages local Google Cuttlefish instances and exposes them through a
central web dashboard with the operational feel of Google Cloud Console and
VMware vCenter.

The MVP intentionally targets one Ubuntu Server VM. Cuttlefish runs directly on
the host with KVM, while OpenCuttles provides the API, inventory, lifecycle
tracking, health checks, deployment templates, and dashboard.

## MVP Scope

- Manage one Ubuntu Server host.
- Bootstrap a local admin and protect the API/dashboard with secure sessions.
- Use an OIDC-ready RBAC model with admin, operator, and viewer roles.
- Register Android images that can be launched by Cuttlefish.
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
Quickstart installs Go 1.23 and Node.js 22 when the host versions are missing or
too old.
Quickstart installs `adb` and, if missing, builds and installs the Google
Cuttlefish host packages. Set `OPENCUTTLES_SKIP_CUTTLEFISH_INSTALL=1` to skip the
Cuttlefish build and start only the dashboard/API in dry-run mode.

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

1. Prepare the Ubuntu Server VM with KVM, Cuttlefish, ADB, and a service user.
2. Build the backend and frontend assets.
3. Run `make package`.
4. Install with `bash scripts/ubuntu/install.sh dist/package`.
5. Configure `/etc/opencuttles/opencuttles.env`.
6. Apply the firewall with `bash scripts/ubuntu/firewall.sh`.
7. Open the dashboard and bootstrap the first local admin user.

Production builds require `backend/go.sum` and `frontend/package-lock.json`.
Generate them with `go mod tidy` and `npm install` before packaging.

See `docs/architecture.md` and `docs/acceptance.md` for the detailed control
plane model and validation checklist.
