# Testral Runner

A small agent you run **on a desktop machine** (Windows today; Linux/macOS on the
same seam next) so a Testral appliance can drive it for agentic UI testing.

- **Dial-home only** — it makes an *outbound* connection to the appliance and
  keeps it open. No inbound ports, firewall rules, or RDP/SSH exposure.
- **Runs in your interactive session** — it captures the real desktop and injects
  mouse/keyboard, so run it while logged in (a normal terminal window), **not** as
  a Session-0 service (that would screenshot a black screen).
- **Single self-contained binary** — native Windows control (GDI screen capture +
  `user32` input). No Node/Python/other runtime required.

## Build

Requires Go 1.22+.

```bash
cd runner
go build -o opencuttles-runner.exe .          # on Windows
# or cross-compile from Linux/macOS:
GOOS=windows GOARCH=amd64 go build -o opencuttles-runner.exe .
```

## Run

1. Add the device in the Testral dashboard (or via the API) and copy the
   **enrollment token** shown once.
2. In an interactive session on the target:

```powershell
.\opencuttles-runner.exe --appliance http://YOUR-APPLIANCE --token <enrollment-token>
```

Or via environment variables:

```powershell
$env:OPENCUTTLES_APPLIANCE = "http://YOUR-APPLIANCE"
$env:OPENCUTTLES_ENROLL_TOKEN = "<enrollment-token>"
.\opencuttles-runner.exe
```

The device flips to **online** in the dashboard once connected; the runner
reconnects automatically if the link drops.

## What it exposes

The appliance drives the desktop through a small, server-agnostic vocabulary:
`screenshot`, `click(x,y)`, `drag`, `type(text)`, `key(name)`, plus `list_apps`,
`open_app(name)`, and `current_activity` (Start-menu enumeration/launch and the
foreground window). Testral's Florence-2 vision grounding turns "tap the Start
button" into a click at the right pixel — the same engine used for Android.

Key names: `ENTER`, `TAB`, `ESC`, `BACKSPACE`, `DELETE`, `SPACE`, arrows
(`UP`/`DOWN`/`LEFT`/`RIGHT`), `HOME`, `END`, `PAGEUP`, `PAGEDOWN`, `WIN`.

## Notes / roadmap

- v1 typing handles the Basic Multilingual Plane (ASCII + most accented text) via
  the active keyboard layout.
- Primary display only (multi-monitor virtual-desktop capture is a follow-up).
- Linux (X11) and macOS controllers plug into the same `screen` interface.
