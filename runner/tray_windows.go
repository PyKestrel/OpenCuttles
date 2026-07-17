//go:build windows

package main

import (
	"log"
	"runtime"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	shell32  = windows.NewLazySystemDLL("shell32.dll")
	kernel32 = windows.NewLazySystemDLL("kernel32.dll")

	procGetModuleHandleW = kernel32.NewProc("GetModuleHandleW")

	procRegisterClassExW  = user32.NewProc("RegisterClassExW")
	procCreateWindowExW   = user32.NewProc("CreateWindowExW")
	procDefWindowProcW    = user32.NewProc("DefWindowProcW")
	procDestroyWindow     = user32.NewProc("DestroyWindow")
	procGetMessageW       = user32.NewProc("GetMessageW")
	procTranslateMessage  = user32.NewProc("TranslateMessage")
	procDispatchMessageW  = user32.NewProc("DispatchMessageW")
	procPostQuitMessage   = user32.NewProc("PostQuitMessage")
	procPostMessageW      = user32.NewProc("PostMessageW")
	procLoadIconW         = user32.NewProc("LoadIconW")
	procLoadCursorW       = user32.NewProc("LoadCursorW")
	procCreatePopupMenu   = user32.NewProc("CreatePopupMenu")
	procAppendMenuW       = user32.NewProc("AppendMenuW")
	procTrackPopupMenu    = user32.NewProc("TrackPopupMenu")
	procDestroyMenu       = user32.NewProc("DestroyMenu")
	procSetForegroundWin  = user32.NewProc("SetForegroundWindow")
	procGetCursorPos      = user32.NewProc("GetCursorPos")

	procShellNotifyIconW = shell32.NewProc("Shell_NotifyIconW")
	procShellExecuteW    = shell32.NewProc("ShellExecuteW")
)

const (
	// Custom window messages (WM_APP range is reserved for app use).
	wmTrayCallback = 0x8000 + 1 // WM_APP+1: mouse activity on the tray icon
	wmTrayRefresh  = 0x8000 + 2 // WM_APP+2: connection status changed

	wmDestroy      = 0x0002
	wmCommand      = 0x0111
	wmNull         = 0x0000
	wmRButtonUp    = 0x0205
	wmLButtonDblc  = 0x0203
	wmContextMenu  = 0x007B

	nimAdd    = 0x0000
	nimModify = 0x0001
	nimDelete = 0x0002
	nifMessage = 0x0001
	nifIcon    = 0x0002
	nifTip     = 0x0004

	mfString    = 0x0000
	mfSeparator = 0x0800
	mfChecked   = 0x0008
	mfGrayed    = 0x0001

	tpmRightButton = 0x0002
	tpmLeftAlign   = 0x0000

	idiApplication = 32512
	idcArrow       = 32512
	colorWindow    = 5

	cwUseDefault = 0x80000000
	wsOverlapped = 0x00000000

	// Menu command ids.
	idOpenDashboard = 101
	idViewLog       = 102
	idReconnect     = 103
	idAutostart     = 104
	idQuit          = 105
)

type wndClassExW struct {
	cbSize        uint32
	style         uint32
	lpfnWndProc   uintptr
	cbClsExtra    int32
	cbWndExtra    int32
	hInstance     uintptr
	hIcon         uintptr
	hCursor       uintptr
	hbrBackground uintptr
	lpszMenuName  *uint16
	lpszClassName *uint16
	hIconSm       uintptr
}

type pointT struct{ x, y int32 }

type msgT struct {
	hwnd    uintptr
	message uint32
	wParam  uintptr
	lParam  uintptr
	time    uint32
	pt      pointT
}

// notifyIconData is NOTIFYICONDATAW. cbSize is set to the full struct size,
// which modern Windows accepts.
type notifyIconData struct {
	cbSize            uint32
	hWnd              uintptr
	uID               uint32
	uFlags            uint32
	uCallbackMessage  uint32
	hIcon             uintptr
	szTip             [128]uint16
	dwState           uint32
	dwStateMask       uint32
	szInfo            [256]uint16
	uVersionOrTimeout uint32
	szInfoTitle       [64]uint16
	dwInfoFlags       uint32
	guidItem          [16]byte
	hBalloonIcon      uintptr
}

// trayApp owns the hidden window + notification icon. There is one per process.
type trayApp struct {
	st        *agentState
	appliance string
	token     string
	hwnd      uintptr
	nid       notifyIconData
}

var tray *trayApp

// runAgentUI (windows) shows the system tray and runs the tunnel loop beside the
// message pump. If the tray can't be created it falls back to running headless,
// so the agent still works.
func runAgentUI(appliance, token string, st *agentState) {
	runtime.LockOSThread() // the window + message loop must stay on one thread

	t := &trayApp{st: st, appliance: appliance, token: token}
	tray = t
	if err := t.create(); err != nil {
		log.Printf("system tray unavailable (%v) — running headless", err)
		runAgentLoop(appliance, token, st)
		return
	}
	defer t.remove()

	// Reflect connection status in the tray by posting a refresh to the UI thread.
	st.setOnChange(func(bool) { t.postRefresh() })
	go runAgentLoop(appliance, token, st)

	t.messageLoop()
}

var trayClassName = utf16("OpenCuttlesRunnerTray")

func (t *trayApp) create() error {
	hInst, _, _ := procGetModuleHandleW.Call(0)
	icon, _, _ := procLoadIconW.Call(0, uintptr(idiApplication))
	cursor, _, _ := procLoadCursorW.Call(0, uintptr(idcArrow))

	wc := wndClassExW{
		cbSize:        uint32(unsafe.Sizeof(wndClassExW{})),
		lpfnWndProc:   syscall.NewCallback(wndProc),
		hInstance:     hInst,
		hIcon:         icon,
		hCursor:       cursor,
		hbrBackground: colorWindow + 1,
		lpszClassName: &trayClassName[0],
	}
	if atom, _, err := procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc))); atom == 0 {
		return err
	}

	// A normal top-level window that is never shown. (A message-only window can't
	// become foreground, which TrackPopupMenu needs to dismiss correctly.)
	title := utf16("OpenCuttles Runner")
	hwnd, _, err := procCreateWindowExW.Call(
		0,
		uintptr(unsafe.Pointer(&trayClassName[0])),
		uintptr(unsafe.Pointer(&title[0])),
		wsOverlapped,
		cwUseDefault, cwUseDefault, 0, 0,
		0, 0, hInst, 0,
	)
	if hwnd == 0 {
		return err
	}
	t.hwnd = hwnd

	t.nid = notifyIconData{
		cbSize:           uint32(unsafe.Sizeof(notifyIconData{})),
		hWnd:             hwnd,
		uID:              1,
		uFlags:           nifMessage | nifIcon | nifTip,
		uCallbackMessage: wmTrayCallback,
		hIcon:            icon,
	}
	t.setTip("OpenCuttles Runner — connecting…")
	if ret, _, err := procShellNotifyIconW.Call(nimAdd, uintptr(unsafe.Pointer(&t.nid))); ret == 0 {
		procDestroyWindow.Call(hwnd)
		return err
	}
	return nil
}

func (t *trayApp) setTip(s string) {
	src := utf16(s)
	for i := range t.nid.szTip {
		t.nid.szTip[i] = 0
	}
	copy(t.nid.szTip[:len(t.nid.szTip)-1], src)
}

func (t *trayApp) postRefresh() {
	procPostMessageW.Call(t.hwnd, wmTrayRefresh, 0, 0)
}

func (t *trayApp) refresh() {
	tip := "OpenCuttles Runner — reconnecting…"
	if t.st.isConnected() {
		tip = "OpenCuttles Runner — connected"
	}
	t.setTip(tip)
	procShellNotifyIconW.Call(nimModify, uintptr(unsafe.Pointer(&t.nid)))
}

func (t *trayApp) remove() {
	procShellNotifyIconW.Call(nimDelete, uintptr(unsafe.Pointer(&t.nid)))
	procDestroyWindow.Call(t.hwnd)
}

func (t *trayApp) messageLoop() {
	var msg msgT
	for {
		ret, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&msg)), 0, 0, 0)
		if int32(ret) <= 0 { // 0 = WM_QUIT, -1 = error
			return
		}
		procTranslateMessage.Call(uintptr(unsafe.Pointer(&msg)))
		procDispatchMessageW.Call(uintptr(unsafe.Pointer(&msg)))
	}
}

// wndProc is the window procedure. It dispatches on the singleton tray so the
// callback stays a plain function (no per-window state pointer needed).
func wndProc(hwnd, msg, wParam, lParam uintptr) uintptr {
	t := tray
	switch msg {
	case wmTrayCallback:
		if t == nil {
			return 0
		}
		switch lParam {
		case wmRButtonUp, wmContextMenu:
			t.showMenu()
		case wmLButtonDblc:
			t.openDashboard()
		}
		return 0
	case wmTrayRefresh:
		if t != nil {
			t.refresh()
		}
		return 0
	case wmCommand:
		if t != nil {
			t.onCommand(uint16(wParam & 0xFFFF))
		}
		return 0
	case wmDestroy:
		procPostQuitMessage.Call(0)
		return 0
	}
	ret, _, _ := procDefWindowProcW.Call(hwnd, msg, wParam, lParam)
	return ret
}

func (t *trayApp) showMenu() {
	menu, _, _ := procCreatePopupMenu.Call()
	if menu == 0 {
		return
	}
	defer procDestroyMenu.Call(menu)

	status := "Reconnecting…"
	if t.st.isConnected() {
		status = "Connected"
	}
	appendMenu(menu, mfString|mfGrayed, 0, "OpenCuttles Runner — "+status)
	appendMenu(menu, mfSeparator, 0, "")
	appendMenu(menu, mfString, idOpenDashboard, "Open dashboard")
	appendMenu(menu, mfString, idViewLog, "View log")
	appendMenu(menu, mfString, idReconnect, "Reconnect")
	appendMenu(menu, mfSeparator, 0, "")
	autoFlag := uintptr(mfString)
	if autostartEnabled() {
		autoFlag |= mfChecked
	}
	appendMenu(menu, autoFlag, idAutostart, "Start at login")
	appendMenu(menu, mfSeparator, 0, "")
	appendMenu(menu, mfString, idQuit, "Quit")

	var pt pointT
	procGetCursorPos.Call(uintptr(unsafe.Pointer(&pt)))
	// Required so the menu dismisses when the user clicks elsewhere.
	procSetForegroundWin.Call(t.hwnd)
	procTrackPopupMenu.Call(menu, tpmRightButton|tpmLeftAlign, uintptr(pt.x), uintptr(pt.y), 0, t.hwnd, 0)
	procPostMessageW.Call(t.hwnd, wmNull, 0, 0)
}

func appendMenu(menu uintptr, flags, id uintptr, text string) {
	var p *uint16
	if text != "" {
		s := utf16(text)
		p = &s[0]
	}
	procAppendMenuW.Call(menu, flags, id, uintptr(unsafe.Pointer(p)))
}

func (t *trayApp) onCommand(id uint16) {
	switch id {
	case idOpenDashboard:
		t.openDashboard()
	case idViewLog:
		t.openLog()
	case idReconnect:
		t.st.forceReconnect()
	case idAutostart:
		t.toggleAutostart()
	case idQuit:
		procDestroyWindow.Call(t.hwnd) // -> WM_DESTROY -> PostQuitMessage
	}
}

func (t *trayApp) openDashboard() {
	if t.appliance != "" {
		shellOpen(t.appliance)
	}
}

func (t *trayApp) openLog() {
	if t.st.logPath != "" {
		shellOpen(t.st.logPath)
	}
}

// toggleAutostart enables/disables the login autostart without spawning a second
// runner (this one keeps running). Enabling first ensures the binary exists at
// its stable install path so the autostart entry points somewhere durable.
func (t *trayApp) toggleAutostart() {
	if autostartEnabled() {
		if err := autostartUnregister(); err != nil {
			log.Printf("disable autostart: %v", err)
		}
		return
	}
	binPath, err := installBinPath()
	if err != nil {
		log.Printf("autostart: %v", err)
		return
	}
	if err := copySelf(binPath); err != nil {
		log.Printf("autostart copy: %v", err)
		return
	}
	if err := autostartRegister(binPath, t.appliance, t.token); err != nil {
		log.Printf("enable autostart: %v", err)
	}
}

func shellOpen(target string) {
	verb := utf16("open")
	t := utf16(target)
	procShellExecuteW.Call(0, uintptr(unsafe.Pointer(&verb[0])), uintptr(unsafe.Pointer(&t[0])), 0, 0, 1 /*SW_SHOWNORMAL*/)
}

// utf16 returns a NUL-terminated UTF-16 slice for Win32 wide-string APIs.
func utf16(s string) []uint16 {
	p, _ := windows.UTF16FromString(s)
	return p
}
