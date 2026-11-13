//go:build darwin

package input

import (
	"log"
	"runtime"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"

	"github.com/ebitengine/purego"
)

const (
	cgFrameworkPath  = "/System/Library/Frameworks/CoreGraphics.framework/CoreGraphics"
	cfFrameworkPath  = "/System/Library/Frameworks/CoreFoundation.framework/CoreFoundation"
	appServicesPath  = "/System/Library/Frameworks/ApplicationServices.framework/ApplicationServices"

	kCGSessionEventTap       = 1
	kCGHeadInsertEventTap    = 0
	kCGEventTapOptionDefault = 0

	kCGEventKeyDown              = 10
	kCGEventKeyUp                = 11
	kCGEventFlagsChanged         = 12
	kCGEventTapDisabledByTimeout = 0xFFFFFFFE
	kCGEventTapDisabledByInput   = 0xFFFFFFFF

	kCGKeyboardEventAutorepeat = 8
	kCGKeyboardEventKeycode    = 9

	kCGEventFlagMaskShift     = 0x00020000
	kCGEventFlagMaskControl   = 0x00040000
	kCGEventFlagMaskAlternate = 0x00080000
	kCGEventFlagMaskCommand   = 0x00100000

	nxDeviceLCtlMask = 0x00000001
	nxDeviceRCtlMask = 0x00002000
)

var (
	darwinOnce sync.Once
	darwinErr  error

	axIsProcessTrusted          func() bool
	cgEventTapCreate            func(tap, place, options uint32, mask uint64, cb uintptr, userInfo uintptr) uintptr
	cgEventTapEnable            func(tap uintptr, enable bool)
	cgEventGetFlags             func(event uintptr) uint64
	cgEventGetIntegerValueField func(event uintptr, field uint32) int64

	cfMachPortCreateRunLoopSource func(alloc uintptr, port uintptr, order int) uintptr
	cfRunLoopGetCurrent           func() uintptr
	cfRunLoopAddSource            func(rl uintptr, src uintptr, mode uintptr)
	cfRunLoopStop                 func(rl uintptr)
	cfRunLoopRun                  func()
	cfRelease                     func(cf uintptr)
	kCFRunLoopCommonModesVal      uintptr

	activeDarwinHook atomic.Pointer[Hook]
	activeTapPort    atomic.Uintptr
	darwinTapCB      uintptr
)

func initDarwinSymbols() error {
	darwinOnce.Do(func() {
		cg, err := purego.Dlopen(cgFrameworkPath, purego.RTLD_LAZY|purego.RTLD_GLOBAL)
		if err != nil {
			darwinErr = err
			return
		}
		cf, err := purego.Dlopen(cfFrameworkPath, purego.RTLD_LAZY|purego.RTLD_GLOBAL)
		if err != nil {
			darwinErr = err
			return
		}
		as, err := purego.Dlopen(appServicesPath, purego.RTLD_LAZY|purego.RTLD_GLOBAL)
		if err != nil {
			darwinErr = err
			return
		}

		bind := func(dst any, lib uintptr, sym string) bool {
			if darwinErr != nil {
				return false
			}
			p, e := purego.Dlsym(lib, sym)
			if e != nil {
				darwinErr = e
				return false
			}
			purego.RegisterFunc(dst, p)
			return true
		}

		bind(&axIsProcessTrusted, as, "AXIsProcessTrusted")
		bind(&cgEventTapCreate, cg, "CGEventTapCreate")
		bind(&cgEventTapEnable, cg, "CGEventTapEnable")
		bind(&cgEventGetFlags, cg, "CGEventGetFlags")
		bind(&cgEventGetIntegerValueField, cg, "CGEventGetIntegerValueField")

		bind(&cfMachPortCreateRunLoopSource, cf, "CFMachPortCreateRunLoopSource")
		bind(&cfRunLoopGetCurrent, cf, "CFRunLoopGetCurrent")
		bind(&cfRunLoopAddSource, cf, "CFRunLoopAddSource")
		bind(&cfRunLoopStop, cf, "CFRunLoopStop")
		bind(&cfRunLoopRun, cf, "CFRunLoopRun")
		bind(&cfRelease, cf, "CFRelease")

		if darwinErr == nil {
			modeSym, e := purego.Dlsym(cf, "kCFRunLoopCommonModes")
			if e != nil {
				darwinErr = e
				return
			}
			kCFRunLoopCommonModesVal = **(**uintptr)(unsafe.Pointer(&modeSym))
			darwinTapCB = purego.NewCallback(darwinEventTapCallback)
		}
	})
	return darwinErr
}

// CheckTrusted reports whether the process currently has macOS Accessibility / EventTap trust.
func CheckTrusted() bool {
	if err := initDarwinSymbols(); err != nil {
		return false
	}
	return axIsProcessTrusted()
}

func darwinEventTapCallback(_ uintptr, eventType uint32, event uintptr, _ uintptr) uintptr {
	h := activeDarwinHook.Load()
	if h == nil || event == 0 {
		return event
	}

	if eventType == kCGEventTapDisabledByTimeout || eventType == kCGEventTapDisabledByInput {
		if port := activeTapPort.Load(); port != 0 {
			cgEventTapEnable(port, true)
		}
		h.tracker.Reset()
		return event
	}

	now := time.Now()
	switch eventType {
	case kCGEventFlagsChanged:
		flags := cgEventGetFlags(event)
		keyCode := uint16(cgEventGetIntegerValueField(event, kCGKeyboardEventKeycode))
		anyCtrl := (flags & kCGEventFlagMaskControl) != 0

		var leftCtrl, rightCtrl bool
		if anyCtrl {
			rightCtrl = (flags&nxDeviceRCtlMask) != 0 || keyCode == 62
			leftCtrl = (flags&nxDeviceLCtlMask) != 0 || !rightCtrl
		}
		mods := Mods{
			LeftCtrl:  leftCtrl,
			RightCtrl: rightCtrl,
			Alt:       (flags & kCGEventFlagMaskAlternate) != 0,
			Shift:     (flags & kCGEventFlagMaskShift) != 0,
			Super:     (flags & kCGEventFlagMaskCommand) != 0,
		}
		h.tracker.OnModifierChange(now, mods)
		return event

	case kCGEventKeyDown:
		keyCode := uint16(cgEventGetIntegerValueField(event, kCGKeyboardEventKeycode))
		if isDarwinModifierKeycode(keyCode) {
			return event
		}
		isRepeat := cgEventGetIntegerValueField(event, kCGKeyboardEventAutorepeat) != 0
		role := classifyDarwinKeycode(keyCode)
		if h.tracker.OnKeyDown(now, keyCode, role, isRepeat) {
			return 0 // Swallow keystroke so foreground app never sees it
		}
		return event

	case kCGEventKeyUp:
		keyCode := uint16(cgEventGetIntegerValueField(event, kCGKeyboardEventKeycode))
		if h.tracker.OnKeyUp(keyCode) {
			return 0
		}
		return event
	}

	return event
}

func isDarwinModifierKeycode(kc uint16) bool {
	return kc >= 54 && kc <= 63
}

func classifyDarwinKeycode(kc uint16) KeyRole {
	switch kc {
	case 13, 126: // W, Up
		return KeyDirUp
	case 1, 125: // S, Down
		return KeyDirDown
	case 0, 123: // A, Left
		return KeyDirLeft
	case 2, 124: // D, Right
		return KeyDirRight
	case 53: // Escape
		return KeyEscape
	default:
		return KeyOther
	}
}

func (h *Hook) startPlatform() {
	if err := initDarwinSymbols(); err != nil {
		log.Printf("[input] failed initializing macOS CoreGraphics symbols: %v", err)
		return
	}

	activeDarwinHook.Store(h)
	var stopFlag atomic.Bool
	var runLoopRef atomic.Uintptr

	h.stopFn = func() {
		stopFlag.Store(true)
		if rl := runLoopRef.Load(); rl != 0 {
			cfRunLoopStop(rl)
		}
	}

	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()

		mask := uint64((1 << kCGEventKeyDown) | (1 << kCGEventKeyUp) | (1 << kCGEventFlagsChanged))

		for !stopFlag.Load() {
			trusted := axIsProcessTrusted()
			h.setTrusted(trusted)

			port := cgEventTapCreate(
				kCGSessionEventTap,
				kCGHeadInsertEventTap,
				kCGEventTapOptionDefault,
				mask,
				darwinTapCB,
				0,
			)
			if port == 0 {
				h.setTrusted(false)
				time.Sleep(1200 * time.Millisecond)
				continue
			}

			h.setTrusted(true)
			activeTapPort.Store(port)
			src := cfMachPortCreateRunLoopSource(0, port, 0)
			rl := cfRunLoopGetCurrent()
			runLoopRef.Store(rl)

			cfRunLoopAddSource(rl, src, kCFRunLoopCommonModesVal)
			cgEventTapEnable(port, true)
			log.Printf("[input] macOS CGEventTap active (mode=%s)", h.Mode())

			cfRunLoopRun()

			runLoopRef.Store(0)
			activeTapPort.Store(0)
			if src != 0 {
				cfRelease(src)
			}
			cfRelease(port)

			if !stopFlag.Load() {
				time.Sleep(500 * time.Millisecond)
			}
		}
	}()
}
