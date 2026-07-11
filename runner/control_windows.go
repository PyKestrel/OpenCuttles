//go:build windows

package main

import (
	"bytes"
	"fmt"
	"image"
	"image/png"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	user32 = windows.NewLazySystemDLL("user32.dll")
	gdi32  = windows.NewLazySystemDLL("gdi32.dll")

	procGetSystemMetrics = user32.NewProc("GetSystemMetrics")
	procGetDC            = user32.NewProc("GetDC")
	procReleaseDC        = user32.NewProc("ReleaseDC")
	procSetCursorPos     = user32.NewProc("SetCursorPos")
	procMouseEvent       = user32.NewProc("mouse_event")
	procKeybdEvent       = user32.NewProc("keybd_event")
	procVkKeyScanW       = user32.NewProc("VkKeyScanW")

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
