//go:build windows

package main

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	user32 = windows.NewLazySystemDLL("user32.dll")
	gdi32  = windows.NewLazySystemDLL("gdi32.dll")

	procSetProcessDpiAwarenessContext = user32.NewProc("SetProcessDpiAwarenessContext")
	procSetProcessDPIAware            = user32.NewProc("SetProcessDPIAware")

	procGetSystemMetrics = user32.NewProc("GetSystemMetrics")
	procGetDC            = user32.NewProc("GetDC")
	procReleaseDC        = user32.NewProc("ReleaseDC")
	procSetCursorPos       = user32.NewProc("SetCursorPos")
	procMouseEvent         = user32.NewProc("mouse_event")
	procKeybdEvent         = user32.NewProc("keybd_event")
	procVkKeyScanW         = user32.NewProc("VkKeyScanW")
	procGetForegroundWindow = user32.NewProc("GetForegroundWindow")
	procGetWindowTextW      = user32.NewProc("GetWindowTextW")

	procCreateCompatibleDC     = gdi32.NewProc("CreateCompatibleDC")
	procCreateCompatibleBitmap = gdi32.NewProc("CreateCompatibleBitmap")
	procSelectObject           = gdi32.NewProc("SelectObject")
	procBitBlt                 = gdi32.NewProc("BitBlt")
	procGetDIBits              = gdi32.NewProc("GetDIBits")
	procDeleteObject           = gdi32.NewProc("DeleteObject")
	procDeleteDC               = gdi32.NewProc("DeleteDC")
)

const (
	smCXScreen = 0
	smCYScreen = 1
	srcCopy    = 0x00CC0020
	biRGB      = 0

	mouseLeftDown = 0x0002
	mouseLeftUp   = 0x0004
	keyEventUp    = 0x0002
	vkShift       = 0x10
)

type bitmapInfoHeader struct {
	Size          uint32
	Width         int32
	Height        int32
	Planes        uint16
	BitCount      uint16
	Compression   uint32
	SizeImage     uint32
	XPelsPerMeter int32
	YPelsPerMeter int32
	ClrUsed       uint32
	ClrImportant  uint32
}

type bitmapInfo struct {
	Header bitmapInfoHeader
	Colors [1]uint32
}

// init makes the process DPI-aware BEFORE any capture or cursor call, so screen
// metrics, GDI capture, and SetCursorPos all use the same physical-pixel space
// regardless of the display's scaling (100/125/150/…%). This eliminates the
// capture-vs-input coordinate mismatch entirely — no per-scale correction needed
// — and captures at full native resolution (sharp, not upscaled). Prefers
// Per-Monitor-V2 (Windows 10 1703+, handles multi-monitor mixed DPI), falling
// back to system DPI-aware on older builds.
func init() {
	const perMonitorAwareV2 = ^uintptr(3) // (DPI_AWARENESS_CONTEXT)-4
	if procSetProcessDpiAwarenessContext.Find() == nil {
		if r, _, _ := procSetProcessDpiAwarenessContext.Call(perMonitorAwareV2); r != 0 {
			return
		}
	}
	procSetProcessDPIAware.Call()
}

type winScreen struct{}

func newScreen() (screen, error) { return &winScreen{}, nil }

func (winScreen) Screenshot() ([]byte, error) {
	w, _, _ := procGetSystemMetrics.Call(smCXScreen)
	h, _, _ := procGetSystemMetrics.Call(smCYScreen)
	width, height := int(w), int(h)
	if width == 0 || height == 0 {
		return nil, fmt.Errorf("could not determine screen size")
	}

	hdc, _, _ := procGetDC.Call(0)
	if hdc == 0 {
		return nil, fmt.Errorf("GetDC failed")
	}
	defer procReleaseDC.Call(0, hdc)

	memDC, _, _ := procCreateCompatibleDC.Call(hdc)
	if memDC == 0 {
		return nil, fmt.Errorf("CreateCompatibleDC failed")
	}
	defer procDeleteDC.Call(memDC)

	bmp, _, _ := procCreateCompatibleBitmap.Call(hdc, uintptr(width), uintptr(height))
	if bmp == 0 {
		return nil, fmt.Errorf("CreateCompatibleBitmap failed")
	}
	defer procDeleteObject.Call(bmp)

	old, _, _ := procSelectObject.Call(memDC, bmp)
	defer procSelectObject.Call(memDC, old)

	if ret, _, _ := procBitBlt.Call(memDC, 0, 0, uintptr(width), uintptr(height), hdc, 0, 0, srcCopy); ret == 0 {
		return nil, fmt.Errorf("BitBlt failed")
	}

	// Request a top-down 32-bit BGRA buffer.
	bi := bitmapInfo{Header: bitmapInfoHeader{
		Size:        uint32(unsafe.Sizeof(bitmapInfoHeader{})),
		Width:       int32(width),
		Height:      -int32(height),
		Planes:      1,
		BitCount:    32,
		Compression: biRGB,
	}}
	buf := make([]byte, width*height*4)
	if ret, _, _ := procGetDIBits.Call(memDC, bmp, 0, uintptr(height),
		uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&bi)), 0); ret == 0 {
		return nil, fmt.Errorf("GetDIBits failed")
	}

	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for i := 0; i < width*height; i++ {
		b, g, r := buf[i*4+0], buf[i*4+1], buf[i*4+2]
		img.Pix[i*4+0] = r
		img.Pix[i*4+1] = g
		img.Pix[i*4+2] = b
		img.Pix[i*4+3] = 255
	}

	var out bytes.Buffer
	if err := png.Encode(&out, img); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

func (winScreen) Click(x, y int) error {
	procSetCursorPos.Call(uintptr(x), uintptr(y))
	time.Sleep(20 * time.Millisecond)
	procMouseEvent.Call(mouseLeftDown, 0, 0, 0, 0)
	time.Sleep(20 * time.Millisecond)
	procMouseEvent.Call(mouseLeftUp, 0, 0, 0, 0)
	return nil
}

func (winScreen) Drag(x1, y1, x2, y2, durationMs int) error {
	procSetCursorPos.Call(uintptr(x1), uintptr(y1))
	time.Sleep(20 * time.Millisecond)
	procMouseEvent.Call(mouseLeftDown, 0, 0, 0, 0)
	steps := 10
	for i := 1; i <= steps; i++ {
		x := x1 + (x2-x1)*i/steps
		y := y1 + (y2-y1)*i/steps
		procSetCursorPos.Call(uintptr(x), uintptr(y))
		if durationMs > 0 {
			time.Sleep(time.Duration(durationMs/steps) * time.Millisecond)
		} else {
			time.Sleep(10 * time.Millisecond)
		}
	}
	procMouseEvent.Call(mouseLeftUp, 0, 0, 0, 0)
	return nil
}

func (winScreen) Type(text string) error {
	for _, r := range text {
		if r > 0xFFFF {
			continue // outside the BMP; skip for v1
		}
		vk, _, _ := procVkKeyScanW.Call(uintptr(uint16(r)))
		res := int16(vk)
		if res == -1 {
			continue
		}
		low := byte(res & 0xFF)
		shift := (res >> 8) & 1
		if shift != 0 {
			procKeybdEvent.Call(vkShift, 0, 0, 0)
		}
		procKeybdEvent.Call(uintptr(low), 0, 0, 0)
		procKeybdEvent.Call(uintptr(low), 0, keyEventUp, 0)
		if shift != 0 {
			procKeybdEvent.Call(vkShift, 0, keyEventUp, 0)
		}
		time.Sleep(5 * time.Millisecond)
	}
	return nil
}

// keyMap maps portable key names to Windows virtual-key codes. Android-only
// names (HOME/BACK/APP_SWITCH/VOLUME_*) don't apply to a desktop.
var keyMap = map[string]byte{
	"ENTER": 0x0D, "RETURN": 0x0D,
	"TAB": 0x09, "ESC": 0x1B, "ESCAPE": 0x1B,
	"BACKSPACE": 0x08, "BACK": 0x08,
	"DELETE": 0x2E, "DEL": 0x2E,
	"SPACE": 0x20,
	"UP":    0x26, "DOWN": 0x28, "LEFT": 0x25, "RIGHT": 0x27,
	"HOME": 0x24, "END": 0x23, "PAGEUP": 0x21, "PAGEDOWN": 0x22,
	"WIN": 0x5B, "SUPER": 0x5B, "META": 0x5B,
}

func (winScreen) Key(name string) error {
	vk, ok := keyMap[name]
	if !ok {
		return fmt.Errorf("unsupported key %q on this platform", name)
	}
	procKeybdEvent.Call(uintptr(vk), 0, 0, 0)
	procKeybdEvent.Call(uintptr(vk), 0, keyEventUp, 0)
	return nil
}

// hidden runs a helper process without flashing a console window.
func hidden(name string, args ...string) *exec.Cmd {
	cmd := exec.Command(name, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	return cmd
}

func (winScreen) ListApps() ([]string, error) {
	out, err := hidden("powershell", "-NoProfile", "-NonInteractive", "-Command",
		"Get-StartApps | Select-Object -ExpandProperty Name").Output()
	if err != nil {
		return nil, fmt.Errorf("list apps: %w", err)
	}
	var apps []string
	seen := map[string]bool{}
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line != "" && !seen[line] {
			seen[line] = true
			apps = append(apps, line)
		}
	}
	return apps, nil
}

func (winScreen) OpenApp(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", fmt.Errorf("open_app needs an app name (e.g. \"Settings\"); none was given")
	}
	// Resolve the best Start-menu match to its AppID and launch it. Match
	// precedence is exact (case-insensitive) → starts-with → contains, so a query
	// like "Settings" can't fall through to the alphabetically-first app.
	//
	// Launch: Start-Process on the AppsFolder moniker works for classic desktop
	// apps (Notepad), but UWP/immersive apps like Settings REJECT it — so on
	// failure we fall back to explorer.exe, which launches every app kind. Only a
	// genuine no-match exits non-zero (with a reason on stderr); a launch that
	// needed the fallback still succeeds. Echo the resolved Name on success.
	//
	// The name is passed via $env:OC_APPNAME, NOT a positional arg: powershell
	// -Command does NOT reliably bind trailing args to $args (the old $args[0]
	// came through empty, so every app matched "*" and it opened 7-Zip).
	const script = `$n=$env:OC_APPNAME
$apps = Get-StartApps
$m = @($apps | Where-Object { $_.Name -ieq $n })
if (-not $m) { $m = @($apps | Where-Object { $_.Name -like "$n*" }) }
if (-not $m) { $m = @($apps | Where-Object { $_.Name -like "*$n*" }) }
if (-not $m) { [Console]::Error.WriteLine("no Start-menu app matches"); exit 2 }
$app = $m[0]
try {
  Start-Process ("shell:AppsFolder\" + $app.AppID) -ErrorAction Stop
} catch {
  Start-Process -FilePath explorer.exe -ArgumentList ("shell:AppsFolder\" + $app.AppID)
}
Write-Output $app.Name`
	cmd := hidden("powershell", "-NoProfile", "-NonInteractive", "-Command", script)
	cmd.Env = append(os.Environ(), "OC_APPNAME="+name)
	out, err := cmd.Output()
	if err != nil {
		// Surface the real reason (stderr) instead of assuming "no match".
		detail := ""
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			detail = strings.TrimSpace(string(ee.Stderr))
		}
		if detail == "" {
			detail = err.Error()
		}
		return "", fmt.Errorf("could not open %q: %s — call list_apps for exact names", name, detail)
	}
	return strings.TrimSpace(string(out)), nil
}

// RunInstaller runs an app-under-test installer silently. .msi goes through
// msiexec /qn; other files (.exe) are run with a silent flag (/S by default,
// covering NSIS/Inno). args overrides the flags for installers that differ.
func (winScreen) RunInstaller(path, args string) error {
	if strings.EqualFold(filepath.Ext(path), ".msi") {
		a := []string{"/i", path, "/qn", "/norestart"}
		if args != "" {
			a = append([]string{"/i", path}, strings.Fields(args)...)
		}
		return hidden("msiexec", a...).Run()
	}
	flags := "/S"
	if args != "" {
		flags = args
	}
	return hidden(path, strings.Fields(flags)...).Run()
}

func (winScreen) CurrentActivity() (string, error) {
	hwnd, _, _ := procGetForegroundWindow.Call()
	if hwnd == 0 {
		return "", nil
	}
	buf := make([]uint16, 512)
	procGetWindowTextW.Call(hwnd, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	return windows.UTF16ToString(buf), nil
}
