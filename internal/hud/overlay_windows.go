//go:build windows

package hud

import (
	"image"
	"syscall"
	"unsafe"

	"github.com/egoist/mygo"
)

const (
	dwmwaWindowCornerPreference = 33
	dwmwcpDoNotRound            = 1
	dwmwaBorderColor            = 34
	dwmwaColorNone              = 0xFFFFFFFE

	gwlExStyle     = -20
	wsExNoActivate = 0x08000000

	monitorDefaultToNearest = 2
	ropSrcCopy              = 0x00CC0020
)

type winRect struct {
	Left, Top, Right, Bottom int32
}

type winPoint struct {
	X, Y int32
}

type winMonitorInfo struct {
	Size    uint32
	Monitor winRect
	Work    winRect
	Flags   uint32
}

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
	Colors [3]uint32
}

var (
	hudKernel32 = syscall.NewLazyDLL("kernel32.dll")
	hudSysDir   = hudSystemDirectory()
	hudUser32   = syscall.NewLazyDLL(hudSysDir + `\user32.dll`)
	hudDwmapi   = syscall.NewLazyDLL(hudSysDir + `\dwmapi.dll`)
	hudGdi32    = syscall.NewLazyDLL(hudSysDir + `\gdi32.dll`)

	procDwmSetWindowAttribute = hudDwmapi.NewProc("DwmSetWindowAttribute")
	procGetWindowLongPtrW     = hudUser32.NewProc("GetWindowLongPtrW")
	procSetWindowLongPtrW     = hudUser32.NewProc("SetWindowLongPtrW")
	procGetWindowLongW        = hudUser32.NewProc("GetWindowLongW")
	procSetWindowLongW        = hudUser32.NewProc("SetWindowLongW")
	procGetClientRect         = hudUser32.NewProc("GetClientRect")
	procClientToScreen        = hudUser32.NewProc("ClientToScreen")
	procGetDC                 = hudUser32.NewProc("GetDC")
	procReleaseDC             = hudUser32.NewProc("ReleaseDC")
	procMonitorFromWindow     = hudUser32.NewProc("MonitorFromWindow")
	procGetMonitorInfoW       = hudUser32.NewProc("GetMonitorInfoW")
	procGetDpiForWindow       = hudUser32.NewProc("GetDpiForWindow")

	procCreateCompatibleDC     = hudGdi32.NewProc("CreateCompatibleDC")
	procDeleteDC               = hudGdi32.NewProc("DeleteDC")
	procCreateCompatibleBitmap = hudGdi32.NewProc("CreateCompatibleBitmap")
	procGetDIBits              = hudGdi32.NewProc("GetDIBits")
	procSelectObject           = hudGdi32.NewProc("SelectObject")
	procDeleteObject           = hudGdi32.NewProc("DeleteObject")
	procBitBlt                 = hudGdi32.NewProc("BitBlt")
)

func hudSystemDirectory() string {
	buf := make([]uint16, syscall.MAX_PATH)
	n, _, _ := hudKernel32.NewProc("GetSystemDirectoryW").Call(
		uintptr(unsafe.Pointer(&buf[0])),
		uintptr(len(buf)),
	)
	if n == 0 || int(n) > len(buf) {
		return `C:\Windows\System32`
	}
	return syscall.UTF16ToString(buf[:n])
}

// configureOverlayWindow removes Windows 11 DWM panel corner rounding, outline
// border, and drop shadow around the transparent DirectComposition HWND, and
// marks the window WS_EX_NOACTIVATE so showing it never steals keyboard focus.
func configureOverlayWindow(win *mygo.Window) {
	if win == nil {
		return
	}
	hwnd := win.NativeHandle()
	if hwnd == 0 {
		return
	}

	cornerPref := int32(dwmwcpDoNotRound)
	_, _, _ = procDwmSetWindowAttribute.Call(
		hwnd,
		dwmwaWindowCornerPreference,
		uintptr(unsafe.Pointer(&cornerPref)),
		unsafe.Sizeof(cornerPref),
	)

	borderNone := uint32(dwmwaColorNone)
	_, _, _ = procDwmSetWindowAttribute.Call(
		hwnd,
		dwmwaBorderColor,
		uintptr(unsafe.Pointer(&borderNone)),
		unsafe.Sizeof(borderNone),
	)

	index := int32(gwlExStyle)
	idx := uintptr(index)
	if procGetWindowLongPtrW.Find() == nil && procSetWindowLongPtrW.Find() == nil {
		ex, _, _ := procGetWindowLongPtrW.Call(hwnd, idx)
		_, _, _ = procSetWindowLongPtrW.Call(hwnd, idx, ex|wsExNoActivate)
	} else {
		ex, _, _ := procGetWindowLongW.Call(hwnd, idx)
		_, _, _ = procSetWindowLongW.Call(hwnd, idx, ex|wsExNoActivate)
	}
}

// setOverlayBackdrop intentionally leaves the HWND free of DWM Acrylic/Mica
// policies on Windows 11: DWM replaces Acrylic/BlurBehind on inactive
// (WS_EX_NOACTIVATE) windows with an opaque dark-gray fallback (#2C2C2C).
// Instead, HelLauncher captures and Gaussian-blurs the real desktop region
// onto its DirectComposition GPU surface so translucency + blur stay active
// even while unfocused and during slide-in/slide-out animations.
func setOverlayBackdrop(*mygo.Window, bool) {}

// captureWindowBackdrop captures the live desktop region directly behind win
// (while win is hidden right before ShowInactive) and returns a pre-blurred
// RGBA backdrop aligned to win's client DIP coordinate space.
func captureWindowBackdrop(win *mygo.Window, minHeightDIP int) *backdropCapture {
	if win == nil {
		return nil
	}
	hwnd := win.NativeHandle()
	if hwnd == 0 {
		return nil
	}

	dpi := 96
	if procGetDpiForWindow.Find() == nil {
		if d, _, _ := procGetDpiForWindow.Call(hwnd); d > 0 {
			dpi = int(d)
		}
	}
	scale := float32(dpi) / 96.0

	var cr winRect
	if ok, _, _ := procGetClientRect.Call(hwnd, uintptr(unsafe.Pointer(&cr))); ok == 0 {
		return nil
	}
	var origin winPoint
	if ok, _, _ := procClientToScreen.Call(hwnd, uintptr(unsafe.Pointer(&origin))); ok == 0 {
		return nil
	}

	clientW := int(cr.Right - cr.Left)
	clientH := int(cr.Bottom - cr.Top)
	wantMinPxH := int(float32(minHeightDIP)*scale + 0.5)
	targetH := max(clientH, wantMinPxH)
	if clientW <= 0 || targetH <= 0 {
		return nil
	}

	marginPx := int(36.0*scale + 0.5)
	capL := int(origin.X) - marginPx
	capT := int(origin.Y) - marginPx
	capR := int(origin.X) + clientW + marginPx
	capB := int(origin.Y) + targetH + marginPx

	if mon, _, _ := procMonitorFromWindow.Call(hwnd, monitorDefaultToNearest); mon != 0 {
		var mi winMonitorInfo
		mi.Size = uint32(unsafe.Sizeof(mi))
		if ok, _, _ := procGetMonitorInfoW.Call(mon, uintptr(unsafe.Pointer(&mi))); ok != 0 {
			capL = max(capL, int(mi.Monitor.Left))
			capT = max(capT, int(mi.Monitor.Top))
			capR = min(capR, int(mi.Monitor.Right))
			capB = min(capB, int(mi.Monitor.Bottom))
		}
	}

	capW := capR - capL
	capH := capB - capT
	if capW <= 4 || capH <= 4 {
		return nil
	}

	hdcScreen, _, _ := procGetDC.Call(0)
	if hdcScreen == 0 {
		return nil
	}
	defer procReleaseDC.Call(0, hdcScreen)

	img := captureFromDC(hdcScreen, capL, capT, capW, capH, scale)
	if img == nil {
		return nil
	}

	return &backdropCapture{
		img:     img,
		offsetX: float32(capL-int(origin.X)) / scale,
		offsetY: float32(capT-int(origin.Y)) / scale,
		width:   float32(capW) / scale,
		height:  float32(capH) / scale,
	}
}

func captureFromDC(hdcSrc uintptr, srcX, srcY, capW, capH int, scale float32) *image.RGBA {
	if hdcSrc == 0 || capW <= 0 || capH <= 0 {
		return nil
	}
	hdcMem, _, _ := procCreateCompatibleDC.Call(hdcSrc)
	if hdcMem == 0 {
		return nil
	}
	defer procDeleteDC.Call(hdcMem)

	hbmp, _, _ := procCreateCompatibleBitmap.Call(hdcSrc, uintptr(capW), uintptr(capH))
	if hbmp == 0 {
		return nil
	}
	defer procDeleteObject.Call(hbmp)

	oldObj, _, _ := procSelectObject.Call(hdcMem, hbmp)
	okBlt, _, _ := procBitBlt.Call(
		hdcMem,
		0, 0,
		uintptr(capW), uintptr(capH),
		hdcSrc,
		uintptr(int(srcX)), uintptr(int(srcY)),
		ropSrcCopy,
	)
	_, _, _ = procSelectObject.Call(hdcMem, oldObj)
	if okBlt == 0 {
		return nil
	}

	bmi := bitmapInfo{
		Header: bitmapInfoHeader{
			Size:     uint32(unsafe.Sizeof(bitmapInfoHeader{})),
			Width:    int32(capW),
			Height:   -int32(capH), // negative = top-down DIB
			Planes:   1,
			BitCount: 32,
		},
	}
	rawBGRA := make([]byte, capW*capH*4)
	lines, _, _ := procGetDIBits.Call(
		hdcMem,
		hbmp,
		0,
		uintptr(capH),
		uintptr(unsafe.Pointer(&rawBGRA[0])),
		uintptr(unsafe.Pointer(&bmi)),
		0,
	)
	if lines == 0 {
		return nil
	}
	return downsampleAndBlurBGRA(rawBGRA, capW, capH, scale)
}
