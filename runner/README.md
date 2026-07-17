# Testral Runner

A small agent you run **on a desktop machine** (Windows, Linux/X11, or macOS) so a
Testral appliance can drive it for agentic UI testing.

- **Dial-home only** — it makes an *outbound* connection to the appliance and
  keeps it open. No inbound ports, firewall rules, or RDP/SSH exposure.
- **Runs in your interactive session** — it captures the real desktop and injects
  mouse/keyboard, so run it while logged in (a normal terminal window), **not** as
  a Session-0 service (that would screenshot a black screen).
- **Single self-contained binary** — cgo-free, no Node/Python runtime.

## Platform support

| | Windows | Linux (X11) | macOS |
|---|---|---|---|
| How | native GDI + `user32` | `xdotool` + a screenshot tool | `screencapture` + AppleScript |
| Extra install | none | **required** (see below) | none for the basics; `cliclick` for the rest |
| Screenshot, click, type, keys, chords | yes | yes | yes |
| Wheel scroll | yes | yes | needs `cliclick` (else falls back to Page Up/Down) |
| Right / middle click | yes | yes | right needs `cliclick`; no middle click |
| Drag | yes | yes | needs `cliclick` |

**Linux** needs `xdotool` plus one screenshot tool (`maim`, `import`, `scrot`,
`gnome-screenshot`, or `spectacle`), and `gtk-launch` to open apps by name:

```bash
sudo apt install xdotool maim libgtk-3-bin     # Debian/Ubuntu
```

It must run in an **X11/Xorg session** — Wayland does not allow synthetic input
from another process. The runner detects this and says so at startup.

**macOS** must be granted **Accessibility** and **Screen Recording** for the app
running the runner (System Settings › Privacy & Security), or every call fails.
The runner checks this at startup. AppleScript has no wheel/right-click/drag, so
install the optional helper for full capability:

```bash
brew install cliclick
```

**Other OSes** compile but report that control isn't implemented.

## Build

Requires Go 1.22+.

```bash
cd runner
go build -o opencuttles-runner.exe .          # on Windows
go build -o opencuttles-runner .              # on Linux/macOS
# or cross-compile:
GOOS=windows GOARCH=amd64 go build -o opencuttles-runner.exe .
GOOS=linux   GOARCH=amd64 go build -o opencuttles-runner .
GOOS=darwin  GOARCH=arm64 go build -o opencuttles-runner .
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
(`UP`/`DOWN`/`LEFT`/`RIGHT`), `HOME`, `END`, `PAGEUP`, `PAGEDOWN`, `WIN`,
`INSERT`, `PRINTSCREEN`, `F1`–`F12`.

Chords take the combination in order with modifiers first, e.g. `["CTRL","C"]`,
`["ALT","TAB"]`, `["WIN","R"]`. Modifiers are `CTRL`, `ALT`, `SHIFT`, and `WIN`
(which maps to Command on macOS). Express shift with the modifier — a lone
uppercase letter is normalised to lowercase, so `["CTRL","C"]` is Ctrl+C and
never Ctrl+Shift+C.

## Notes / roadmap

- Typing handles the Basic Multilingual Plane (ASCII + most accented text) via
  the active keyboard layout.
- Primary display only (multi-monitor virtual-desktop capture is a follow-up).
- No auto-start/installer yet: the runner is launched by hand in an interactive
  session and does not survive a reboot or logout.
- macOS gaps are listed in the platform table above (no middle click; wheel,
  right-click, and drag need `cliclick`).
