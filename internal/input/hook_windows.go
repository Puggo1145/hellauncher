//go:build windows

package input

import (
	"log"
	"runtime"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"
)

const (
	whKeyboardLL = 13

	wmKeyDown    = 0x0100
	wmKeyUp      = 0x0101
	wmSysKeyDown = 0x0104
	wmSysKeyUp   = 0x0105
	wmQuit       = 0x0012

	vkShift    = 0x10
	vkControl  = 0x11
	vkMenu     = 0x12
	vkEscape   = 0x1B
	vkLeft     = 0x25
	vkUp       = 0x26
	vkRight    = 0x27
	vkDown     = 0x28
	vkA        = 0x41
	vkD        = 0x44
	vkS        = 0x53
	vkW        = 0x57
	vkLWin     = 0x5B
	vkRWin     = 0x5C
	vkLShift   = 0xA0
	vkRShift   = 0xA1
	vkLControl = 0xA2
	vkRControl = 0xA3
	vkLMenu    = 0xA4
	vkRMenu    = 0xA5
)

type kbdLLHookStruct struct {
	VkCode      uint32
	ScanCode    uint32
	Flags       uint32
	Time        uint32
	DwExtraInfo uintptr
}

type winPoint struct {
	X, Y int32
}

type winMsg struct {
	Hwnd    uintptr
	Message uint32
	WParam  uintptr
	LParam  uintptr
	Time    uint32
	Pt      winPoint
}

var (
	user32                  = syscall.NewLazyDLL("user32.dll")
	kernel32                = syscall.NewLazyDLL("kernel32.dll")
	procSetWindowsHookExW   = user32.NewProc("SetWindowsHookExW")
	procCallNextHookEx      = user32.NewProc("CallNextHookEx")
	procUnhookWindowsHookEx = user32.NewProc("UnhookWindowsHookEx")
	procGetMessageW         = user32.NewProc("GetMessageW")
	procPostThreadMessageW  = user32.NewProc("PostThreadMessageW")
	procGetCurrentThreadId  = kernel32.NewProc("GetCurrentThreadId")

	winHookOnce  sync.Once
	winHookCB    uintptr
	activeWinPtr atomic.Pointer[Hook]

	winStateMu sync.Mutex
	winHeldVK  = map[uint16]bool{}
)

// CheckTrusted always returns true on Windows since WH_KEYBOARD_LL requires no TCC prompt.
func CheckTrusted() bool {
	return true
}

func winKeyboardProc(nCode int, wParam uintptr, info *kbdLLHookStruct) uintptr {
	lParam := uintptr(unsafe.Pointer(info))
	if nCode < 0 || info == nil {
		ret, _, _ := procCallNextHookEx.Call(0, uintptr(nCode), wParam, lParam)
		return ret
	}

	h := activeWinPtr.Load()
	if h == nil {
		ret, _, _ := procCallNextHookEx.Call(0, uintptr(nCode), wParam, lParam)
		return ret
	}

	vk := uint16(info.VkCode)
	isDown := wParam == wmKeyDown || wParam == wmSysKeyDown
	isUp := wParam == wmKeyUp || wParam == wmSysKeyUp
	now := time.Now()

	winStateMu.Lock()
	wasDown := winHeldVK[vk]
	if isDown {
		winHeldVK[vk] = true
	} else if isUp {
		delete(winHeldVK, vk)
	}
	mods := Mods{
		LeftCtrl:  winHeldVK[vkLControl] || (winHeldVK[vkControl] && !winHeldVK[vkRControl]),
		RightCtrl: winHeldVK[vkRControl],
		Alt:       winHeldVK[vkLMenu] || winHeldVK[vkRMenu] || winHeldVK[vkMenu],
		Shift:     winHeldVK[vkLShift] || winHeldVK[vkRShift] || winHeldVK[vkShift],
		Super:     winHeldVK[vkLWin] || winHeldVK[vkRWin],
	}
	winStateMu.Unlock()

	if isWinModifierVK(vk) {
		if !isDown || !wasDown {
			h.tracker.OnModifierChange(now, mods)
		}
		ret, _, _ := procCallNextHookEx.Call(0, uintptr(nCode), wParam, lParam)
		return ret
	}

	if isDown {
		role := classifyWinVK(vk)
		if h.tracker.OnKeyDown(now, vk, role, wasDown) {
			return 1 // Swallow keystroke before foreground Windows app sees Ctrl+W/S/A/D!
		}
	} else if isUp {
		if h.tracker.OnKeyUp(vk) {
			return 1
		}
	}

	ret, _, _ := procCallNextHookEx.Call(0, uintptr(nCode), wParam, lParam)
	return ret
}

func isWinModifierVK(vk uint16) bool {
	switch vk {
	case vkShift, vkControl, vkMenu, vkLShift, vkRShift, vkLControl, vkRControl, vkLMenu, vkRMenu, vkLWin, vkRWin:
		return true
	default:
		return false
	}
}

func classifyWinVK(vk uint16) KeyRole {
	switch vk {
	case vkW, vkUp:
		return KeyDirUp
	case vkS, vkDown:
		return KeyDirDown
	case vkA, vkLeft:
		return KeyDirLeft
	case vkD, vkRight:
		return KeyDirRight
	case vkEscape:
		return KeyEscape
	default:
		return KeyOther
	}
}

func (h *Hook) startPlatform() {
	winHookOnce.Do(func() {
		winHookCB = syscall.NewCallback(winKeyboardProc)
	})
	activeWinPtr.Store(h)
	h.setTrusted(true)

	var hookThreadID atomic.Uint32
	h.stopFn = func() {
		if tid := hookThreadID.Load(); tid != 0 {
			procPostThreadMessageW.Call(uintptr(tid), wmQuit, 0, 0)
		}
	}

	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()

		tid, _, _ := procGetCurrentThreadId.Call()
		hookThreadID.Store(uint32(tid))

		hhk, _, err := procSetWindowsHookExW.Call(whKeyboardLL, winHookCB, 0, 0)
		if hhk == 0 {
			log.Printf("[input] SetWindowsHookExW failed: %v", err)
			return
		}
		defer procUnhookWindowsHookEx.Call(hhk)
		log.Printf("[input] Windows WH_KEYBOARD_LL active (mode=%s)", h.Mode())

		var msg winMsg
		for {
			ret, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&msg)), 0, 0, 0)
			if int32(ret) <= 0 {
				break
			}
		}
	}()
}
