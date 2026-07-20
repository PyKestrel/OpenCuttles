# Acceptance Checklist

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

- Caddy terminates TLS and proxies everything to the binary, which serves the
  embedded SPA, the API, and the console proxy on one port.
- WebSocket upgrade headers are preserved for console-related routes.
- ADB ports are not exposed publicly.
- UFW allows only SSH and HTTPS inbound by default.
- `scripts/ubuntu/backup.sh` and `scripts/ubuntu/restore.sh` complete successfully.
- `opencuttles-backup.timer` is enabled and has run (`systemctl list-timers`).
- **Restore drill:** back up, write more data, restore, and confirm the
  post-backup writes are *gone* rather than resurrected. This is the check that
  catches stale-WAL replay, the most likely way to lose data in an incident.
- Old snapshots are pruned (`opencuttles-*` and `rollback-*` both bounded).
- Every secret in `opencuttles.env` is a generated value, not a `change-this…`
  placeholder, and no secret is empty.
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

## Desktop Runner

- The runner downloads from the dashboard for the target OS and enrolls with a
  one-time token.
- It connects **outbound only** — no inbound port is opened on the target.
- Screenshot, click, type, scroll, right-click, drag, and chord all work.
  (`["CTRL","C"]` must produce Ctrl+C, not Ctrl+Shift+C.)
- Auto-start survives a logout/reboot; the Windows tray menu items all work.
- The device reconnects on its own after the appliance restarts.
- Rotating the token disconnects the running runner and the **old** token no
  longer authenticates; the one-liner shown reconnects the machine.
- Revoking the token disconnects it and no token authenticates until a new one
  is issued. The device stays in the inventory.
- Deleting the device stops it from reconnecting.

## Agent and Vision

- The vision sidecar answers `/healthz`, and the health report shows a `vision`
  check — failing when it is down, absent when it is not configured.
- An admin can configure a provider and model in the dashboard; "Test
  connection" succeeds.
- "Test connection" against a *different* `baseUrl` does **not** send the stored
  API key.
- The Agent chat panel drives a real device end to end.
- With the vision sidecar stopped, agent tests fail loudly rather than silently
  passing.

## Test Management

- Import cases from QMetry CSV/XLSX; author a cycle; run it manually.
- A cron-scheduled cycle fires at the expected time in its configured timezone.
- Uploading a build triggers the cycles bound to that platform.
- A failing step is recorded as a failure — a crashed or abandoned run must
  never be reported as a pass.
- Per-step screenshots and video are captured and visible in the report.
- JUnit, CSV, and XLSX exports open in their target tools, with no negative
  durations.
- The completion webhook fires with the right payload.
- **Restart mid-run**, then confirm the cycle's schedule still fires afterwards
  (the stranded-run sweep) and the interrupted run reads `failed`.

## Health and Capacity

- `/api/v1/healthz` returns 503 when the database is unreachable.
- The health report shows free disk and degrades below the threshold.
- An unreferenced image can be deleted and its files leave the disk; a
  referenced one is refused with a clear message.
- An upload over the size cap is rejected with 413, not a hang or a full disk.
