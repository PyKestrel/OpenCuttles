# MVP Acceptance Checklist

Use this checklist on a prepared Ubuntu Server VM before treating a Testral
build as merge-ready.

## Host Readiness

- `scripts/ubuntu/check-host.sh` exits successfully.
- `/dev/kvm` exists and is readable/writable by the service user.
- `cvd` and `adb` are available on `PATH`; legacy `launch_cvd` / `stop_cvd` are optional fallbacks.
- The host has enough free memory and disk for at least one Cuttlefish instance.
- The Testral API service starts under systemd.

## Backend

- `go test ./...` passes from `backend/`.
- `GET /api/v1/healthz` returns `ok`.
- `GET /api/v1/bootstrap` reports bootstrap required before the first admin user exists.
- The first admin user can be created and can log in with a secure session cookie.
- Unauthenticated requests to protected API routes return `401`.
- `GET /api/v1/host` returns capacity and prerequisite checks.
- `GET /api/v1/health` returns DB, execution mode, Cuttlefish, ADB, and KVM checks.
- `GET /api/v1/metrics` returns Prometheus-compatible metrics for the authenticated caller.
- Image registration persists across API restarts.
- Instance create/start/stop/delete operations create operation records.
- Mutating actions and console access create audit events with actor and request metadata.
- Failed Cuttlefish commands move the instance to `error` with a useful message.

## Frontend

- `npm run build` passes from `frontend/`.
- `npm test -- --run` passes from `frontend/`.
- Bootstrap and login flows render correctly.
- The dashboard renders host capacity, instance inventory, and recent activity.
- The create instance form can submit a valid request.
- Instance action buttons call the backend and update displayed state.
- Delete requires confirmation.
- The console page opens the authenticated instance-bound Cuttlefish WebRTC URL.

## Deployment

- Caddy serves the frontend and proxies `/api/*` to the backend.
- WebSocket upgrade headers are preserved for console-related routes.
- ADB ports are not exposed publicly.
- UFW allows only SSH and HTTPS inbound by default.
- `scripts/ubuntu/backup.sh` and `scripts/ubuntu/restore.sh` complete successfully.
- Upgrade and rollback scripts preserve a rollback snapshot.
- Service logs are visible through `journalctl -u opencuttles-api`.

## Manual Cuttlefish Smoke Test

1. Register an image path known to work with local Cuttlefish tools.
2. Create an instance using that image.
3. Start the instance.
4. Wait for the lifecycle to reach `running`.
5. Open the WebRTC console from the dashboard.
6. Stop the instance.
7. Delete the instance and confirm its operation history remains visible.
8. Confirm console access is denied after logout.
