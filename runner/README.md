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

## Auto-start at login

To have the runner start automatically after a reboot or logout, install it
instead of running it directly (no admin required — it's a per-user autostart):

```powershell
.\opencuttles-runner.exe install --appliance http://YOUR-APPLIANCE --token <enrollment-token>
```

`install` copies the runner to a stable per-user location, registers a login
autostart, and starts it immediately:

| OS | Mechanism | Location |
|---|---|---|
| Windows | `HKCU\…\Run` registry value | `%LOCALAPPDATA%\OpenCuttles\` |
| Linux | XDG autostart `.desktop` | `~/.config/autostart/` (runs at graphical login) |
| macOS | LaunchAgent (`RunAtLoad`) | `~/Library/LaunchAgents/` |

Remove it with:

```
opencuttles-runner uninstall
```

The dashboard's onboarding dialog generates a one-line command that downloads
and runs `install` for you.

## Windows agent (tray + wizard)

On Windows the runner behaves like a proper desktop agent:

- **System tray icon.** While running it shows a tray icon whose tooltip
  reflects connection status (connected / reconnecting). Right-click for a menu:
  *Open dashboard*, *View log*, *Reconnect*, *Start at login* (toggles the
  autostart entry), and *Quit*.
- **Install wizard.** Double-clicking `opencuttles-runner.exe` with no arguments
  opens a small window to paste the appliance URL + enrollment token and click
  *Install* — the GUI equivalent of `install`.

Both are built in with no extra dependencies (native Win32). Logs are written to
`%LOCALAPPDATA%\OpenCuttles\runner.log` so *View log* and auto-started runs (no
console) both have a trail.

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
- Auto-start via `install` (per-user, no admin) survives reboot/logout; there is
  no signed installer package or code-signing yet, so the downloaded binary may
  trip SmartScreen/Gatekeeper on first run.
- macOS gaps are listed in the platform table above (no middle click; wheel,
  right-click, and drag need `cliclick`).
