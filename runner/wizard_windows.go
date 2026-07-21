//go:build windows

package main

import (
	"log"
	"strings"
	"syscall"
	"unsafe"
)

var (
	procShowWindow     = user32.NewProc("ShowWindow")
	procUpdateWindow   = user32.NewProc("UpdateWindow")
	procSendMessageW   = user32.NewProc("SendMessageW")
	procMessageBoxW    = user32.NewProc("MessageBoxW")
	procGetStockObject = gdi32.NewProc("GetStockObject")
)

const (
	wsChild        = 0x40000000
	wsVisible      = 0x10000000
	wsBorder       = 0x00800000
	wsTabstop      = 0x00010000
	wsSysMenu      = 0x00080000
	wsCaption      = 0x00C00000
	esAutoHScroll  = 0x0080
	esPassword     = 0x0020
	swShow         = 5
	wmSetFont      = 0x0030
	wmGetTextLen   = 0x000E
	bsDefPushButton = 0x0001

	defaultGUIFont = 17
	smCxScreenIdx  = 0
	smCyScreenIdx  = 1

	mbOK          = 0x0000
	mbIconError   = 0x0010
	mbIconInfo    = 0x0040

	// Wizard control ids.
	idEditAppliance = 201
	idEditToken     = 202
	idEditPin       = 203
	idButtonInstall = 203
	idButtonCancel  = 204
)

// wizardState holds the controls the window proc needs to read on Install.
type wizardState struct {
	hwnd          uintptr
	editAppliance uintptr
	editToken     uintptr
	editPin       uintptr
	done          bool
}

var wizard *wizardState
var wizardClassName = utf16("OpenCuttlesRunnerWizard")

// maybeShowWizard (windows) opens the install dialog and returns true. Used when
// the runner is launched with no credentials (e.g. double-clicked from Explorer).
func maybeShowWizard() bool {
	if err := showWizard(); err != nil {
		log.Printf("install wizard unavailable: %v", err)
		return false
	}
	return true
}

func showWizard() error {
	hInst, _, _ := procGetModuleHandleW.Call(0)
	icon, _, _ := procLoadIconW.Call(0, uintptr(idiApplication))
	cursor, _, _ := procLoadCursorW.Call(0, uintptr(idcArrow))

	wc := wndClassExW{
		cbSize:        uint32(unsafe.Sizeof(wndClassExW{})),
		lpfnWndProc:   syscall.NewCallback(wizardProc),
		hInstance:     hInst,
		hIcon:         icon,
		hCursor:       cursor,
		hbrBackground: colorWindow + 1,
		lpszClassName: &wizardClassName[0],
	}
	if atom, _, err := procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc))); atom == 0 {
		return err
	}

	const w, h = 440, 300
	cx, _, _ := procGetSystemMetrics.Call(smCxScreenIdx)
	cy, _, _ := procGetSystemMetrics.Call(smCyScreenIdx)
	x := (int32(cx) - w) / 2
	y := (int32(cy) - h) / 2

	title := utf16("Install OpenCuttles Runner")
	hwnd, _, err := procCreateWindowExW.Call(
		0,
		uintptr(unsafe.Pointer(&wizardClassName[0])),
		uintptr(unsafe.Pointer(&title[0])),
		wsCaption|wsSysMenu, // fixed dialog frame, no resize
		uintptr(x), uintptr(y), w, h,
		0, 0, hInst, 0,
	)
	if hwnd == 0 {
		return err
	}

	ws := &wizardState{hwnd: hwnd}
	wizard = ws
	buildWizardControls(ws, hInst)

	procShowWindow.Call(hwnd, swShow)
	procUpdateWindow.Call(hwnd)

	// Modal-ish: run a message loop until the window is destroyed.
	var msg msgT
	for {
		ret, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&msg)), 0, 0, 0)
		if int32(ret) <= 0 {
			break
		}
		procTranslateMessage.Call(uintptr(unsafe.Pointer(&msg)))
		procDispatchMessageW.Call(uintptr(unsafe.Pointer(&msg)))
	}
	return nil
}

func buildWizardControls(ws *wizardState, hInst uintptr) {
	font, _, _ := procGetStockObject.Call(defaultGUIFont)
	static := utf16("STATIC")
	edit := utf16("EDIT")
	button := utf16("BUTTON")

	mk := func(class []uint16, text string, style uintptr, x, y, w, h int, id uintptr) uintptr {
		var tp *uint16
		if text != "" {
			t := utf16(text)
			tp = &t[0]
		}
		hwnd, _, _ := procCreateWindowExW.Call(
			0,
			uintptr(unsafe.Pointer(&class[0])),
			uintptr(unsafe.Pointer(tp)),
			wsChild|wsVisible|style,
			uintptr(x), uintptr(y), uintptr(w), uintptr(h),
			ws.hwnd, id, hInst, 0,
		)
		procSendMessageW.Call(hwnd, wmSetFont, font, 1)
		return hwnd
	}

	mk(static, "Appliance URL", 0, 20, 18, 400, 18, 0)
	// Prefilled https, not http: the runner refuses plaintext, and the old
	// "http://" prefill actively steered operators into the insecure setup.
	ws.editAppliance = mk(edit, "https://", wsBorder|wsTabstop|esAutoHScroll, 20, 38, 400, 24, idEditAppliance)

	mk(static, "Enrollment token", 0, 20, 76, 400, 18, 0)
	ws.editToken = mk(edit, "", wsBorder|wsTabstop|esAutoHScroll, 20, 96, 400, 24, idEditToken)

	mk(static, "Certificate pin (leave blank if the appliance has a public certificate)", 0, 20, 134, 400, 18, 0)
	ws.editPin = mk(edit, "", wsBorder|wsTabstop|esAutoHScroll, 20, 154, 400, 24, idEditPin)

	mk(static, "Copy these from the dashboard when you add this device.", 0, 20, 188, 400, 18, 0)

	mk(button, "Install", wsTabstop|bsDefPushButton, 250, 220, 80, 30, idButtonInstall)
	mk(button, "Cancel", wsTabstop, 340, 220, 80, 30, idButtonCancel)
}

func wizardProc(hwnd, msg, wParam, lParam uintptr) uintptr {
	switch msg {
	case wmCommand:
		if wizard != nil {
			switch uint16(wParam & 0xFFFF) {
			case idButtonInstall:
				wizard.onInstall()
			case idButtonCancel:
				procDestroyWindow.Call(hwnd)
			}
		}
		return 0
	case wmDestroy:
		procPostQuitMessage.Call(0)
		return 0
	}
	ret, _, _ := procDefWindowProcW.Call(hwnd, msg, wParam, lParam)
	return ret
}

func (ws *wizardState) onInstall() {
	rawAppliance := strings.TrimSpace(controlText(ws.editAppliance))
	token := strings.TrimSpace(controlText(ws.editToken))
	if rawAppliance == "" || token == "" {
		messageBox(ws.hwnd, "Enter both the appliance URL and the enrollment token.", "Missing details", mbIconError)
		return
	}
	appliance, err := normalizeAppliance(rawAppliance, false)
	if err != nil {
		messageBox(ws.hwnd, err.Error(), "Check the appliance URL", mbIconError)
		return
	}
	pin := strings.TrimSpace(controlText(ws.editPin))
	pinBytes, err := parsePin(pin)
	if err != nil {
		messageBox(ws.hwnd, "That certificate pin isn't valid:\n\n"+err.Error(), "Check the pin", mbIconError)
		return
	}
	// The install starts the runner immediately, so TLS has to be configured
	// before it dials home.
	configureTLS(pinBytes, false)

	e := enrollment{Appliance: appliance, Token: token, Pin: pin}
	if err := runInstall(e); err != nil {
		messageBox(ws.hwnd, "Install failed:\n\n"+err.Error(), "OpenCuttles Runner", mbIconError)
		return
	}
	messageBox(ws.hwnd,
		"Installed. The runner will start at login and is connecting now — the device should show online in the dashboard shortly. You can find it in the system tray.",
		"OpenCuttles Runner", mbIconInfo)
	procDestroyWindow.Call(ws.hwnd)
}

// controlText reads an edit control's text via WM_GETTEXT.
func controlText(hwnd uintptr) string {
	n, _, _ := procSendMessageW.Call(hwnd, wmGetTextLen, 0, 0)
	if n == 0 {
		return ""
	}
	buf := make([]uint16, n+1)
	procGetWindowTextW.Call(hwnd, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	return syscall.UTF16ToString(buf)
}

func messageBox(parent uintptr, text, title string, flags uintptr) {
	tp := utf16(text)
	cp := utf16(title)
	procMessageBoxW.Call(parent, uintptr(unsafe.Pointer(&tp[0])), uintptr(unsafe.Pointer(&cp[0])), mbOK|flags)
}
