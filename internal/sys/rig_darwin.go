//go:build darwin

package sys

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"

	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"
	"github.com/egoist/mygo/plugins/permissionflow"

	"hellauncher/internal/catalog"
)

const (
	darwinAppServices = "/System/Library/Frameworks/ApplicationServices.framework/ApplicationServices"
	darwinCoreGraph   = "/System/Library/Frameworks/CoreGraphics.framework/CoreGraphics"
	darwinCoreFound   = "/System/Library/Frameworks/CoreFoundation.framework/CoreFoundation"
	darwinAppKit      = "/System/Library/Frameworks/AppKit.framework/AppKit"
	darwinLibObjC     = "/usr/lib/libobjc.A.dylib"

	kCFStringUTF8                       = 0x08000100
	kCGWindowListOptionOnScreenOnly     = 1 << 0
	kCGWindowListExcludeDesktopElements = 1 << 4

	kCGSessionEventTap       = 1
	kCGHeadInsertEventTap    = 0
	kCGEventTapOptionDefault = 0

	kCGEventLeftMouseDown        = 1
	kCGEventRightMouseDown       = 3
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

	kAXValueCGPointType = 1
	kAXValueCGSizeType  = 2
)

type cgPoint struct {
	X float64
	Y float64
}

var (
	darwinOnce sync.Once
	darwinErr  error

	axIsProcessTrusted   func() bool
	axCreateSystemWide   func() uintptr
	axCreateApplication  func(pid int32) uintptr
	axCopyAttributeValue func(el uintptr, attr uintptr, val *uintptr) int32
	axSetAttributeValue  func(el uintptr, attr uintptr, val uintptr) int32
	axGetPid             func(el uintptr, pid *int32) int32
	axPerformAction      func(el uintptr, action uintptr) int32
	axValueCreate        func(theType uint32, valuePtr *cgPoint) uintptr
	axValueGetValue      func(value uintptr, theType uint32, valuePtr *cgPoint) bool

	cgEventTapCreate            func(tap, place, options uint32, mask uint64, cb uintptr, userInfo uintptr) uintptr
	cgEventTapEnable            func(tap uintptr, enable bool)
	cgEventGetFlags             func(event uintptr) uint64
	cgEventGetIntegerValueField func(event uintptr, field uint32) int64
	cgEventGetLocation          func(event uintptr) cgPoint
	cgWindowListCopyWindowInfo  func(option, relativeTo uint32) objc.ID

	cfMachPortCreateRunLoopSource func(alloc uintptr, port uintptr, order int) uintptr
	cfRunLoopGetCurrent           func() uintptr
	cfRunLoopAddSource            func(rl uintptr, src uintptr, mode uintptr)
	cfRunLoopStop                 func(rl uintptr)
	cfRunLoopRun                  func()
	cfStrCreateWithCStr           func(alloc uintptr, cstr *byte, enc uint32) uintptr
	cfReleaseRef                  func(cf uintptr)
	kCFRunLoopCommonModesVal      uintptr

	sendUint  func(obj objc.ID, s objc.SEL) uint
	sendInt   func(obj objc.ID, s objc.SEL) int
	sendFloat func(obj objc.ID, s objc.SEL) float64
	poolPush  uintptr
	poolPop   uintptr

	clsNSString             objc.ID
	clsNSRunningApplication objc.ID
	clsNSWorkspace          objc.ID

	selStringWithUTF8String    objc.SEL
	selRunningAppsWithBundleID objc.SEL
	selSharedWorkspace         objc.SEL
	selFrontmostApp            objc.SEL
	selProcessIdentifier       objc.SEL
	selCount                   objc.SEL
	selObjectAtIndex           objc.SEL
	selObjectForKey            objc.SEL
	selIntegerValue            objc.SEL
	selDoubleValue             objc.SEL

	activeDarwinRig atomic.Pointer[DarwinRig]
	darwinKeyTapCB  uintptr
	darwinMouseCB   uintptr
)

func initDarwin() error {
	darwinOnce.Do(func() {
		as, err := purego.Dlopen(darwinAppServices, purego.RTLD_LAZY|purego.RTLD_GLOBAL)
		if err != nil {
			darwinErr = err
			return
		}
		cg, err := purego.Dlopen(darwinCoreGraph, purego.RTLD_LAZY|purego.RTLD_GLOBAL)
		if err != nil {
			darwinErr = err
			return
		}
		cf, err := purego.Dlopen(darwinCoreFound, purego.RTLD_LAZY|purego.RTLD_GLOBAL)
		if err != nil {
			darwinErr = err
			return
		}
		if _, err := purego.Dlopen(darwinAppKit, purego.RTLD_LAZY|purego.RTLD_GLOBAL); err != nil {
			darwinErr = err
			return
		}
		libobjc, err := purego.Dlopen(darwinLibObjC, purego.RTLD_LAZY|purego.RTLD_GLOBAL)
		if err != nil {
			darwinErr = err
			return
		}

		bind := func(dst any, lib uintptr, sym string) {
			if darwinErr != nil {
				return
			}
			p, e := purego.Dlsym(lib, sym)
			if e != nil {
				darwinErr = e
				return
			}
			purego.RegisterFunc(dst, p)
		}

		bind(&axIsProcessTrusted, as, "AXIsProcessTrusted")
		bind(&axCreateSystemWide, as, "AXUIElementCreateSystemWide")
		bind(&axCreateApplication, as, "AXUIElementCreateApplication")
		bind(&axCopyAttributeValue, as, "AXUIElementCopyAttributeValue")
		bind(&axSetAttributeValue, as, "AXUIElementSetAttributeValue")
		bind(&axGetPid, as, "AXUIElementGetPid")
		bind(&axPerformAction, as, "AXUIElementPerformAction")
		bind(&axValueCreate, as, "AXValueCreate")
		bind(&axValueGetValue, as, "AXValueGetValue")

		bind(&cgEventTapCreate, cg, "CGEventTapCreate")
		bind(&cgEventTapEnable, cg, "CGEventTapEnable")
		bind(&cgEventGetFlags, cg, "CGEventGetFlags")
		bind(&cgEventGetIntegerValueField, cg, "CGEventGetIntegerValueField")
		bind(&cgEventGetLocation, cg, "CGEventGetLocation")
		bind(&cgWindowListCopyWindowInfo, cg, "CGWindowListCopyWindowInfo")

		bind(&cfMachPortCreateRunLoopSource, cf, "CFMachPortCreateRunLoopSource")
		bind(&cfRunLoopGetCurrent, cf, "CFRunLoopGetCurrent")
		bind(&cfRunLoopAddSource, cf, "CFRunLoopAddSource")
		bind(&cfRunLoopStop, cf, "CFRunLoopStop")
		bind(&cfRunLoopRun, cf, "CFRunLoopRun")
		bind(&cfStrCreateWithCStr, cf, "CFStringCreateWithCString")
		bind(&cfReleaseRef, cf, "CFRelease")

		if darwinErr != nil {
			return
		}

		modeSym, e := purego.Dlsym(cf, "kCFRunLoopCommonModes")
		if e != nil {
			darwinErr = e
			return
		}
		kCFRunLoopCommonModesVal = **(**uintptr)(unsafe.Pointer(&modeSym))

		msgSend, e := purego.Dlsym(libobjc, "objc_msgSend")
		if e != nil {
			darwinErr = e
			return
		}
		purego.RegisterFunc(&sendUint, msgSend)
		purego.RegisterFunc(&sendInt, msgSend)
		purego.RegisterFunc(&sendFloat, msgSend)
		poolPush, _ = purego.Dlsym(libobjc, "objc_autoreleasePoolPush")
		poolPop, _ = purego.Dlsym(libobjc, "objc_autoreleasePoolPop")

		clsNSString = objc.ID(objc.GetClass("NSString"))
		clsNSRunningApplication = objc.ID(objc.GetClass("NSRunningApplication"))
		clsNSWorkspace = objc.ID(objc.GetClass("NSWorkspace"))

		selStringWithUTF8String = objc.RegisterName("stringWithUTF8String:")
		selRunningAppsWithBundleID = objc.RegisterName("runningApplicationsWithBundleIdentifier:")
		selSharedWorkspace = objc.RegisterName("sharedWorkspace")
		selFrontmostApp = objc.RegisterName("frontmostApplication")
		selProcessIdentifier = objc.RegisterName("processIdentifier")
		selCount = objc.RegisterName("count")
		selObjectAtIndex = objc.RegisterName("objectAtIndex:")
		selObjectForKey = objc.RegisterName("objectForKey:")
		selIntegerValue = objc.RegisterName("integerValue")
		selDoubleValue = objc.RegisterName("doubleValue")

		darwinKeyTapCB = purego.NewCallback(darwinKeyTapCallback)
		darwinMouseCB = purego.NewCallback(darwinMouseTapCallback)
	})
	return darwinErr
}

func withPool(fn func()) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if poolPush != 0 && poolPop != 0 {
		p, _, _ := purego.SyscallN(poolPush)
		defer purego.SyscallN(poolPop, p)
	}
	fn()
}

func nsString(s string) objc.ID {
	buf := append([]byte(s), 0)
	return clsNSString.Send(selStringWithUTF8String, &buf[0])
}

func makeCFString(s string) uintptr {
	buf := append([]byte(s), 0)
	return cfStrCreateWithCStr(0, &buf[0], kCFStringUTF8)
}

// CheckTrusted reports whether macOS Accessibility trust is currently granted.
func CheckTrusted() bool {
	if err := initDarwin(); err != nil {
		return false
	}
	return axIsProcessTrusted()
}

// GuidePermission launches MyGo's native macOS Accessibility drag-and-drop guide.
func GuidePermission(ctx context.Context) (bool, error) {
	st, err := permissionflow.Guide(ctx, permissionflow.Accessibility, permissionflow.GuideOptions{})
	if err != nil {
		return false, err
	}
	return st == permissionflow.Granted, nil
}

// HasConflictingBundleRunning checks whether another HelLauncher bundle (Prod vs Dev) is running.
func HasConflictingBundleRunning(otherBundleID string) bool {
	if err := initDarwin(); err != nil {
		return false
	}
	snap := queryDarwinAppSnap([]string{otherBundleID})
	return snap.running
}

// DarwinRig is the macOS production Rig Adapter.
type DarwinRig struct {
	onEvent      func(InputEvent) bool
	onTrust      func(bool)
	trusted      atomic.Bool
	mouseCapture atomic.Bool
	keyTapPort   atomic.Uintptr
	mouseTapPort atomic.Uintptr
}

func NewPlatformRig() Rig {
	return &DarwinRig{}
}

func (r *DarwinRig) setTrusted(v bool) {
	prev := r.trusted.Swap(v)
	if prev != v && r.onTrust != nil {
		go r.onTrust(v)
	}
}

func (r *DarwinRig) StartInput(
	onEvent func(InputEvent) (swallow bool),
	onTrust func(trusted bool),
) (stop func()) {
	r.onEvent = onEvent
	r.onTrust = onTrust

	if err := initDarwin(); err != nil {
		slog.Error("darwin init symbols failed", "err", err)
		return func() {}
	}

	activeDarwinRig.Store(r)
	var stopFlag atomic.Bool
	var runLoopRef atomic.Uintptr
	var stopOnce sync.Once

	stopFn := func() {
		stopOnce.Do(func() {
			stopFlag.Store(true)
			r.SetMouseCapture(false)
			if rl := runLoopRef.Load(); rl != 0 {
				cfRunLoopStop(rl)
			}
		})
	}

	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()

		keyMask := uint64((1 << kCGEventKeyDown) | (1 << kCGEventKeyUp) | (1 << kCGEventFlagsChanged))
		mouseMask := uint64((1 << kCGEventLeftMouseDown) | (1 << kCGEventRightMouseDown))

		for !stopFlag.Load() {
			trusted := axIsProcessTrusted()
			r.setTrusted(trusted)

			keyPort := cgEventTapCreate(
				kCGSessionEventTap,
				kCGHeadInsertEventTap,
				kCGEventTapOptionDefault,
				keyMask,
				darwinKeyTapCB,
				0,
			)
			if keyPort == 0 {
				r.setTrusted(false)
				time.Sleep(1200 * time.Millisecond)
				continue
			}

			// Create a separate mouse click tap that stays DISABLED at WindowServer level
			// unless SetMouseCapture(true) is called during PhaseArmed!
			mousePort := cgEventTapCreate(
				kCGSessionEventTap,
				kCGHeadInsertEventTap,
				kCGEventTapOptionDefault,
				mouseMask,
				darwinMouseCB,
				0,
			)

			r.setTrusted(true)
			r.keyTapPort.Store(keyPort)
			r.mouseTapPort.Store(mousePort)

			rl := cfRunLoopGetCurrent()
			runLoopRef.Store(rl)

			keySrc := cfMachPortCreateRunLoopSource(0, keyPort, 0)
			cfRunLoopAddSource(rl, keySrc, kCFRunLoopCommonModesVal)
			cgEventTapEnable(keyPort, true)

			var mouseSrc uintptr
			if mousePort != 0 {
				mouseSrc = cfMachPortCreateRunLoopSource(0, mousePort, 0)
				cfRunLoopAddSource(rl, mouseSrc, kCFRunLoopCommonModesVal)
				cgEventTapEnable(mousePort, r.mouseCapture.Load())
			}

			slog.Info("darwin CGEventTap active")
			cfRunLoopRun()

			runLoopRef.Store(0)
			r.keyTapPort.Store(0)
			r.mouseTapPort.Store(0)
			if mouseSrc != 0 {
				cfReleaseRef(mouseSrc)
			}
			if mousePort != 0 {
				cfReleaseRef(mousePort)
			}
			if keySrc != 0 {
				cfReleaseRef(keySrc)
			}
			cfReleaseRef(keyPort)

			if !stopFlag.Load() {
				time.Sleep(500 * time.Millisecond)
			}
		}
	}()

	return stopFn
}

func (r *DarwinRig) SetMouseCapture(enabled bool) {
	r.mouseCapture.Store(enabled)
	if port := r.mouseTapPort.Load(); port != 0 {
		cgEventTapEnable(port, enabled)
	}
}

func darwinKeyTapCallback(_ uintptr, eventType uint32, event uintptr, _ uintptr) uintptr {
	r := activeDarwinRig.Load()
	if r == nil || event == 0 || r.onEvent == nil {
		return event
	}

	if eventType == kCGEventTapDisabledByTimeout || eventType == kCGEventTapDisabledByInput {
		if port := r.keyTapPort.Load(); port != 0 {
			cgEventTapEnable(port, true)
		}
		r.onEvent(InputEvent{At: time.Now(), Kind: InputReset})
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
		r.onEvent(InputEvent{
			At:   now,
			Kind: InputModifiers,
			Mods: Mods{
				LeftCtrl:  leftCtrl,
				RightCtrl: rightCtrl,
				Alt:       (flags & kCGEventFlagMaskAlternate) != 0,
				Shift:     (flags & kCGEventFlagMaskShift) != 0,
				Super:     (flags & kCGEventFlagMaskCommand) != 0,
			},
		})
		return event

	case kCGEventKeyDown:
		keyCode := uint16(cgEventGetIntegerValueField(event, kCGKeyboardEventKeycode))
		if keyCode >= 54 && keyCode <= 63 {
			return event
		}
		isRepeat := cgEventGetIntegerValueField(event, kCGKeyboardEventAutorepeat) != 0
		if r.onEvent(InputEvent{
			At:      now,
			Kind:    InputKeyDown,
			KeyCode: keyCode,
			Role:    classifyDarwinKeycode(keyCode),
			Repeat:  isRepeat,
		}) {
			return 0
		}
		return event

	case kCGEventKeyUp:
		keyCode := uint16(cgEventGetIntegerValueField(event, kCGKeyboardEventKeycode))
		if r.onEvent(InputEvent{
			At:      now,
			Kind:    InputKeyUp,
			KeyCode: keyCode,
		}) {
			return 0
		}
		return event
	}
	return event
}

func darwinMouseTapCallback(_ uintptr, eventType uint32, event uintptr, _ uintptr) uintptr {
	r := activeDarwinRig.Load()
	if r == nil || event == 0 || r.onEvent == nil || !r.mouseCapture.Load() {
		return event
	}

	if eventType == kCGEventTapDisabledByTimeout || eventType == kCGEventTapDisabledByInput {
		if port := r.mouseTapPort.Load(); port != 0 && r.mouseCapture.Load() {
			cgEventTapEnable(port, true)
		}
		return event
	}

	loc := cgEventGetLocation(event)
	pt := Point{X: int(math.Round(loc.X)), Y: int(math.Round(loc.Y))}

	switch eventType {
	case kCGEventLeftMouseDown:
		if r.onEvent(InputEvent{At: time.Now(), Kind: InputMouseLeftDown, Point: pt}) {
			return 0
		}
	case kCGEventRightMouseDown:
		if r.onEvent(InputEvent{At: time.Now(), Kind: InputMouseRightDown, Point: pt}) {
			return 0
		}
	}
	return event
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

func (r *DarwinRig) Execute(ctx context.Context, step catalog.Step, beacon *Point) error {
	if err := initDarwin(); err != nil {
		return err
	}
	switch step.Kind {
	case catalog.StepLaunchApp:
		if err := launchTargetDarwin(ctx, step.Target); err != nil {
			return err
		}
		if beacon != nil {
			centerFrontmostWindowDarwin(*beacon)
		}
		return nil

	case catalog.StepCloseActiveWindow:
		return closeActiveWindowDarwin()

	case catalog.StepOpenTarget:
		if err := startDetachedContext(ctx, "/usr/bin/open", step.Target); err != nil {
			return err
		}
		if beacon != nil {
			sleepCtx(ctx, 220*time.Millisecond)
			centerFrontmostWindowDarwin(*beacon)
		}
		return nil

	case catalog.StepRunShortcut:
		cmd := exec.CommandContext(ctx, "/usr/bin/shortcuts", "run", step.Target)
		return cmd.Run()

	case catalog.StepShellCommand:
		cmd := exec.CommandContext(ctx, "/bin/zsh", "-lc", step.Target)
		return cmd.Run()

	default:
		return fmt.Errorf("sys: unsupported step kind %q", step.Kind)
	}
}

func sleepCtx(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}

func startDetachedContext(ctx context.Context, bin string, args ...string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	cmd := exec.Command(bin, args...)
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() {
		_ = cmd.Wait()
	}()
	return nil
}

type darwinAppSnap struct {
	running   bool
	frontmost bool
	winCount  int
	frontPID  int32
}

func queryDarwinAppSnap(bundleIDs []string) darwinAppSnap {
	var snap darwinAppSnap
	withPool(func() {
		if clsNSWorkspace != 0 {
			if shared := clsNSWorkspace.Send(selSharedWorkspace); shared != 0 {
				if front := shared.Send(selFrontmostApp); front != 0 {
					snap.frontPID = int32(sendInt(front, selProcessIdentifier))
				}
			}
		}

		if len(bundleIDs) == 0 || clsNSRunningApplication == 0 {
			return
		}

		pids := make(map[int32]bool, 4)
		for _, bid := range bundleIDs {
			if bid == "" || strings.HasPrefix(bid, "/") {
				continue
			}
			arr := clsNSRunningApplication.Send(selRunningAppsWithBundleID, nsString(bid))
			if arr == 0 {
				continue
			}
			n := sendUint(arr, selCount)
			for i := range n {
				app := arr.Send(selObjectAtIndex, i)
				if app == 0 {
					continue
				}
				pid := int32(sendInt(app, selProcessIdentifier))
				if pid > 0 {
					pids[pid] = true
					snap.running = true
					if pid == snap.frontPID {
						snap.frontmost = true
					}
				}
			}
		}

		if len(pids) == 0 {
			return
		}

		list := cgWindowListCopyWindowInfo(kCGWindowListOptionOnScreenOnly|kCGWindowListExcludeDesktopElements, 0)
		if list == 0 {
			return
		}
		defer cfReleaseRef(uintptr(list))

		kOwnerPID := nsString("kCGWindowOwnerPID")
		kLayer := nsString("kCGWindowLayer")
		kAlpha := nsString("kCGWindowAlpha")
		kBounds := nsString("kCGWindowBounds")
		kWidth := nsString("Width")
		kHeight := nsString("Height")

		count := sendUint(list, selCount)
		for i := range count {
			w := list.Send(selObjectAtIndex, i)
			if w == 0 {
				continue
			}
			vPID := w.Send(selObjectForKey, kOwnerPID)
			if vPID == 0 {
				continue
			}
			ownerPID := int32(sendInt(vPID, selIntegerValue))
			if !pids[ownerPID] {
				continue
			}

			layer := 0
			if vLayer := w.Send(selObjectForKey, kLayer); vLayer != 0 {
				layer = sendInt(vLayer, selIntegerValue)
			}
			if layer < 0 || layer >= 20 {
				continue
			}

			alpha := 1.0
			if vAlpha := w.Send(selObjectForKey, kAlpha); vAlpha != 0 {
				alpha = sendFloat(vAlpha, selDoubleValue)
			}
			if alpha <= 0.01 {
				continue
			}

			b := w.Send(selObjectForKey, kBounds)
			if b == 0 {
				continue
			}
			var width, height float64
			if vw := b.Send(selObjectForKey, kWidth); vw != 0 {
				width = sendFloat(vw, selDoubleValue)
			}
			if vh := b.Send(selObjectForKey, kHeight); vh != 0 {
				height = sendFloat(vh, selDoubleValue)
			}
			if width < 120 || height < 80 {
				continue
			}
			snap.winCount++
		}
	})
	return snap
}

func launchAndWaitDarwin(ctx context.Context, openArgs []string, bundleIDs []string, expectNewWin bool) error {
	before := queryDarwinAppSnap(bundleIDs)
	if err := startDetachedContext(ctx, "/usr/bin/open", openArgs...); err != nil {
		return err
	}

	var maxWait time.Duration
	switch {
	case len(bundleIDs) == 0:
		maxWait = 2500 * time.Millisecond
	case !before.running:
		maxWait = 6500 * time.Millisecond
	case before.winCount == 0:
		maxWait = 4500 * time.Millisecond
	case !before.frontmost:
		maxWait = 2000 * time.Millisecond
	case expectNewWin:
		maxWait = 450 * time.Millisecond
	default:
		sleepCtx(ctx, 60*time.Millisecond)
		return nil
	}

	deadline := time.Now().Add(maxWait)
	var frontSince time.Time

	for time.Now().Before(deadline) {
		if !sleepCtx(ctx, 50*time.Millisecond) {
			return ctx.Err()
		}
		cur := queryDarwinAppSnap(bundleIDs)

		if len(bundleIDs) == 0 {
			if cur.frontPID > 0 && cur.frontPID != before.frontPID {
				return nil
			}
			continue
		}

		if cur.frontmost && frontSince.IsZero() {
			frontSince = time.Now()
		}

		switch {
		case !before.running || before.winCount == 0:
			if cur.running && cur.winCount > 0 {
				return nil
			}
			if !frontSince.IsZero() && time.Since(frontSince) >= 2200*time.Millisecond {
				return nil
			}
		case !before.frontmost:
			if cur.frontmost && cur.winCount > 0 {
				return nil
			}
		case expectNewWin:
			if cur.winCount > before.winCount {
				return nil
			}
		}
	}
	return nil
}

func bundleSliceForInstalled(id string, fallbacks []string) []string {
	if id != "" && !strings.HasPrefix(id, "/") {
		return []string{id}
	}
	return fallbacks
}

func launchTargetDarwin(ctx context.Context, target string) error {
	switch strings.ToLower(strings.TrimSpace(target)) {
	case "files", "finder":
		home, err := os.UserHomeDir()
		if err != nil || home == "" {
			home = "/"
		}
		return launchAndWaitDarwin(ctx, []string{home}, []string{"com.apple.finder"}, true)

	case "chrome":
		candidates := []string{"com.google.Chrome", "company.thebrowser.Browser", "com.apple.Safari"}
		if app, ok := findInstalledApp(candidates, []string{"Google Chrome", "Arc", "Safari"}); ok && app.Path != "" {
			return launchAndWaitDarwin(ctx, []string{app.Path}, bundleSliceForInstalled(app.ID, candidates), false)
		}
		if err := launchAndWaitDarwin(ctx, []string{"-a", "Google Chrome"}, candidates, false); err == nil {
			return nil
		}
		return startDetachedContext(ctx, "/usr/bin/open", "https://www.google.com")

	case "zed":
		candidates := []string{"dev.zed.Zed", "dev.zed.Zed-Preview", "com.todesktop.230313mzl4w4u92", "com.microsoft.VSCode"}
		if app, ok := findInstalledApp(candidates, []string{"Zed", "Cursor", "Visual Studio Code"}); ok && app.Path != "" {
			return launchAndWaitDarwin(ctx, []string{app.Path}, bundleSliceForInstalled(app.ID, candidates), false)
		}
		return launchAndWaitDarwin(ctx, []string{"-a", "Zed"}, candidates, false)

	case "terminal":
		candidates := []string{"com.mitchellh.ghostty", "com.googlecode.iterm2", "com.apple.Terminal"}
		if app, ok := findInstalledApp(candidates, []string{"Ghostty", "iTerm", "Terminal"}); ok && app.Path != "" {
			return launchAndWaitDarwin(ctx, []string{app.Path}, bundleSliceForInstalled(app.ID, candidates), false)
		}
		return launchAndWaitDarwin(ctx, []string{"-a", "Terminal"}, candidates, false)

	default:
		if strings.HasPrefix(target, "/") {
			return launchAndWaitDarwin(ctx, []string{target}, nil, false)
		}
		if app, ok := findInstalledApp([]string{target}, []string{target}); ok && app.Path != "" {
			return launchAndWaitDarwin(ctx, []string{app.Path}, bundleSliceForInstalled(app.ID, nil), false)
		}
		return launchAndWaitDarwin(ctx, []string{"-a", target}, []string{target}, false)
	}
}

func frontmostAppPIDDarwin() int32 {
	var pid int32
	withPool(func() {
		if clsNSWorkspace == 0 {
			return
		}
		shared := clsNSWorkspace.Send(selSharedWorkspace)
		if shared == 0 {
			return
		}
		front := shared.Send(selFrontmostApp)
		if front == 0 {
			return
		}
		pid = int32(sendInt(front, selProcessIdentifier))
	})
	return pid
}

func acquireFocusedWindowAXDarwin() (uintptr, error) {
	selfPID := int32(os.Getpid())
	var appEl uintptr

	sysWide := axCreateSystemWide()
	if sysWide != 0 {
		defer cfReleaseRef(sysWide)
		attrFocusedApp := makeCFString("AXFocusedApplication")
		defer cfReleaseRef(attrFocusedApp)
		if axCopyAttributeValue(sysWide, attrFocusedApp, &appEl) != 0 {
			appEl = 0
		}
	}

	if appEl == 0 {
		pid := frontmostAppPIDDarwin()
		if pid <= 0 || pid == selfPID {
			return 0, errors.New("no eligible frontmost application window")
		}
		appEl = axCreateApplication(pid)
	}
	if appEl == 0 {
		return 0, errors.New("unable to acquire frontmost application AX element")
	}
	defer cfReleaseRef(appEl)

	var targetPID int32
	if axGetPid(appEl, &targetPID) == 0 && targetPID == selfPID {
		return 0, errors.New("frontmost window belongs to hellauncher")
	}

	var winEl uintptr
	attrFocusedWin := makeCFString("AXFocusedWindow")
	defer cfReleaseRef(attrFocusedWin)
	if axCopyAttributeValue(appEl, attrFocusedWin, &winEl) != 0 || winEl == 0 {
		attrMainWin := makeCFString("AXMainWindow")
		defer cfReleaseRef(attrMainWin)
		_ = axCopyAttributeValue(appEl, attrMainWin, &winEl)
	}
	if winEl == 0 {
		return 0, errors.New("frontmost application has no open window")
	}
	return winEl, nil
}

func centerFrontmostWindowDarwin(beacon Point) {
	winEl, err := acquireFocusedWindowAXDarwin()
	if err != nil || winEl == 0 {
		return
	}
	defer cfReleaseRef(winEl)

	w, h := 960.0, 680.0
	var sizeVal uintptr
	attrSize := makeCFString("AXSize")
	defer cfReleaseRef(attrSize)
	if axCopyAttributeValue(winEl, attrSize, &sizeVal) == 0 && sizeVal != 0 {
		var sz cgPoint
		if axValueGetValue(sizeVal, kAXValueCGSizeType, &sz) && sz.X > 80 && sz.Y > 60 {
			w, h = sz.X, sz.Y
		}
		cfReleaseRef(sizeVal)
	}

	pos := cgPoint{
		X: max(0, float64(beacon.X)-w/2),
		Y: max(28, float64(beacon.Y)-h/2),
	}
	posVal := axValueCreate(kAXValueCGPointType, &pos)
	if posVal == 0 {
		return
	}
	defer cfReleaseRef(posVal)

	attrPos := makeCFString("AXPosition")
	defer cfReleaseRef(attrPos)
	_ = axSetAttributeValue(winEl, attrPos, posVal)
}

func closeActiveWindowDarwin() error {
	winEl, err := acquireFocusedWindowAXDarwin()
	if err != nil {
		if strings.Contains(err.Error(), "belongs to hellauncher") {
			return nil
		}
		return err
	}
	defer cfReleaseRef(winEl)

	var closeBtn uintptr
	attrCloseBtn := makeCFString("AXCloseButton")
	defer cfReleaseRef(attrCloseBtn)
	if axCopyAttributeValue(winEl, attrCloseBtn, &closeBtn) != 0 || closeBtn == 0 {
		return errors.New("focused window has no close button")
	}
	defer cfReleaseRef(closeBtn)

	actPress := makeCFString("AXPress")
	defer cfReleaseRef(actPress)
	if rc := axPerformAction(closeBtn, actPress); rc != 0 {
		return fmt.Errorf("AXPress on close button returned %d", rc)
	}
	return nil
}
