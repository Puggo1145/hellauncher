//go:build windows

package onboarding

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

	gwlStyle     = -16
	wsCaption    = 0x00C00000
	wsThickFrame = 0x00040000

	swpNoZOrder     = 0x0004
	swpNoActivate   = 0x0010
	swpFrameChanged = 0x0020

	monitorDefaultToPrimary = 1
)

type winRect struct {
	Left, Top, Right, Bottom int32
}

type winMonitorInfo struct {
	Size    uint32
	Monitor winRect
	Work    winRect
	Flags   uint32
}

var (
	onbKernel32               = syscall.NewLazyDLL("kernel32.dll")
	onbSysDir                 = onbSystemDirectory()
	onbUser32                 = syscall.NewLazyDLL(onbSysDir + `\user32.dll`)
	onbDwmapi                 = syscall.NewLazyDLL(onbSysDir + `\dwmapi.dll`)
	procDwmSetWindowAttribute = onbDwmapi.NewProc("DwmSetWindowAttribute")
	procGetWindowLongPtrW     = onbUser32.NewProc("GetWindowLongPtrW")
	procSetWindowLongPtrW     = onbUser32.NewProc("SetWindowLongPtrW")
	procGetWindowLongW        = onbUser32.NewProc("GetWindowLongW")
	procSetWindowLongW        = onbUser32.NewProc("SetWindowLongW")
	procSetWindowPos          = onbUser32.NewProc("SetWindowPos")
	procGetWindowRect         = onbUser32.NewProc("GetWindowRect")
	procMonitorFromPoint      = onbUser32.NewProc("MonitorFromPoint")
	procMonitorFromWindow     = onbUser32.NewProc("MonitorFromWindow")
	procGetMonitorInfoW       = onbUser32.NewProc("GetMonitorInfoW")
)

func onbSystemDirectory() string {
	buf := make([]uint16, syscall.MAX_PATH)
	n, _, _ := onbKernel32.NewProc("GetSystemDirectoryW").Call(
		uintptr(unsafe.Pointer(&buf[0])),
		uintptr(len(buf)),
	)
	if n == 0 || int(n) > len(buf) {
		return `C:\Windows\System32`
	}
	return syscall.UTF16ToString(buf[:n])
}

func getWindowLong(hwnd uintptr, index int32) uintptr {
	idx := uintptr(index)
	if procGetWindowLongPtrW.Find() == nil {
		v, _, _ := procGetWindowLongPtrW.Call(hwnd, idx)
		return v
	}
	v, _, _ := procGetWindowLongW.Call(hwnd, idx)
	return v
}

func setWindowLong(hwnd uintptr, index int32, val uintptr) {
	idx := uintptr(index)
	if procSetWindowLongPtrW.Find() == nil {
		_, _, _ = procSetWindowLongPtrW.Call(hwnd, idx, val)
		return
	}
	_, _, _ = procSetWindowLongW.Call(hwnd, idx, val)
}

func primaryMonitorPhysicalRect() winRect {
	mon, _, _ := procMonitorFromPoint.Call(0, monitorDefaultToPrimary)
	if mon == 0 {
		return winRect{}
	}
	var mi winMonitorInfo
	mi.Size = uint32(unsafe.Sizeof(mi))
	ok, _, _ := procGetMonitorInfoW.Call(mon, uintptr(unsafe.Pointer(&mi)))
	if ok == 0 {
		return winRect{}
	}
	return mi.Monitor
}

// configureTranslucentWindow strips Windows 11 DWM border outline and forced
// corner rounding on the frameless DirectComposition overlay while keeping
// normal focus and keyboard/pointer interactivity intact. When squareCorners is
// true (the fullscreen onboarding broadcast/wizard overlay), it also strips
// WS_CAPTION|WS_THICKFRAME and pins the HWND to the full monitor bounds in
// physical pixels so work-area centering never shifts the overlay upward.
func configureTranslucentWindow(win *mygo.Window, squareCorners bool) {
	if win == nil {
		return
	}
	hwnd := win.NativeHandle()
	if hwnd == 0 {
		return
	}
	if squareCorners {
		style := getWindowLong(hwnd, gwlStyle)
		stripped := style &^ uintptr(wsCaption|wsThickFrame)
		if stripped != style {
			setWindowLong(hwnd, gwlStyle, stripped)
		}
		mon, _, _ := procMonitorFromWindow.Call(hwnd, monitorDefaultToPrimary)
		var mi winMonitorInfo
		mi.Size = uint32(unsafe.Sizeof(mi))
		if mon != 0 {
			ok, _, _ := procGetMonitorInfoW.Call(mon, uintptr(unsafe.Pointer(&mi)))
			if ok != 0 && mi.Monitor.Right > mi.Monitor.Left && mi.Monitor.Bottom > mi.Monitor.Top {
				_, _, _ = procSetWindowPos.Call(
					hwnd,
					0,
					uintptr(mi.Monitor.Left),
					uintptr(mi.Monitor.Top),
					uintptr(mi.Monitor.Right-mi.Monitor.Left),
					uintptr(mi.Monitor.Bottom-mi.Monitor.Top),
					swpNoZOrder|swpNoActivate|swpFrameChanged,
				)
			}
		}

		cornerPref := int32(dwmwcpDoNotRound)
		_, _, _ = procDwmSetWindowAttribute.Call(
			hwnd,
			dwmwaWindowCornerPreference,
			uintptr(unsafe.Pointer(&cornerPref)),
			unsafe.Sizeof(cornerPref),
		)
	}
	borderNone := uint32(dwmwaColorNone)
	_, _, _ = procDwmSetWindowAttribute.Call(
		hwnd,
		dwmwaBorderColor,
		uintptr(unsafe.Pointer(&borderNone)),
		unsafe.Sizeof(borderNone),
	)
}

// WindowScreenRectForTest returns the actual Win32 window rectangle and the
// full physical monitor rectangle for automated fullscreen-coverage verification.
func (m *Manager) WindowScreenRectForTest() (winRectOut, monRectOut image.Rectangle, ok bool) {
	if m == nil || m.win == nil {
		return image.Rectangle{}, image.Rectangle{}, false
	}
	hwnd := m.win.NativeHandle()
	if hwnd == 0 {
		return image.Rectangle{}, image.Rectangle{}, false
	}
	var wr winRect
	r, _, _ := procGetWindowRect.Call(hwnd, uintptr(unsafe.Pointer(&wr)))
	if r == 0 {
		return image.Rectangle{}, image.Rectangle{}, false
	}
	mon, _, _ := procMonitorFromWindow.Call(hwnd, monitorDefaultToPrimary)
	var mi winMonitorInfo
	mi.Size = uint32(unsafe.Sizeof(mi))
	if mon == 0 {
		return image.Rectangle{}, image.Rectangle{}, false
	}
	mOk, _, _ := procGetMonitorInfoW.Call(mon, uintptr(unsafe.Pointer(&mi)))
	if mOk == 0 {
		return image.Rectangle{}, image.Rectangle{}, false
	}
	return image.Rect(int(wr.Left), int(wr.Top), int(wr.Right), int(wr.Bottom)),
		image.Rect(int(mi.Monitor.Left), int(mi.Monitor.Top), int(mi.Monitor.Right), int(mi.Monitor.Bottom)),
		true
}
