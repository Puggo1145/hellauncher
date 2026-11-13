//go:build darwin

package actions

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"
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
)

var (
	axOnce sync.Once
	axErr  error

	axCreateSystemWide         func() uintptr
	axCreateApplication        func(pid int32) uintptr
	axCopyAttributeValue       func(el uintptr, attr uintptr, val *uintptr) int32
	axGetPid                   func(el uintptr, pid *int32) int32
	axPerformAction            func(el uintptr, action uintptr) int32
	cgWindowListCopyWindowInfo func(option, relativeTo uint32) objc.ID
	cfStrCreateWithCStr        func(alloc uintptr, cstr *byte, enc uint32) uintptr
	cfReleaseRef               func(cf uintptr)

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
)

func initDarwinAX() error {
	axOnce.Do(func() {
		as, err := purego.Dlopen(darwinAppServices, purego.RTLD_LAZY|purego.RTLD_GLOBAL)
		if err != nil {
			axErr = err
			return
		}
		cg, err := purego.Dlopen(darwinCoreGraph, purego.RTLD_LAZY|purego.RTLD_GLOBAL)
		if err != nil {
			axErr = err
			return
		}
		cf, err := purego.Dlopen(darwinCoreFound, purego.RTLD_LAZY|purego.RTLD_GLOBAL)
		if err != nil {
			axErr = err
			return
		}
		if _, err := purego.Dlopen(darwinAppKit, purego.RTLD_LAZY|purego.RTLD_GLOBAL); err != nil {
			axErr = err
			return
		}
		libobjc, err := purego.Dlopen(darwinLibObjC, purego.RTLD_LAZY|purego.RTLD_GLOBAL)
		if err != nil {
			axErr = err
			return
		}

		bind := func(dst any, lib uintptr, sym string) {
			if axErr != nil {
				return
			}
			p, e := purego.Dlsym(lib, sym)
			if e != nil {
				axErr = e
				return
			}
			purego.RegisterFunc(dst, p)
		}

		bind(&axCreateSystemWide, as, "AXUIElementCreateSystemWide")
		bind(&axCreateApplication, as, "AXUIElementCreateApplication")
		bind(&axCopyAttributeValue, as, "AXUIElementCopyAttributeValue")
		bind(&axGetPid, as, "AXUIElementGetPid")
		bind(&axPerformAction, as, "AXUIElementPerformAction")
		bind(&cgWindowListCopyWindowInfo, cg, "CGWindowListCopyWindowInfo")
		bind(&cfStrCreateWithCStr, cf, "CFStringCreateWithCString")
		bind(&cfReleaseRef, cf, "CFRelease")

		if axErr != nil {
			return
		}
		msgSend, e := purego.Dlsym(libobjc, "objc_msgSend")
		if e != nil {
			axErr = e
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
	})
	return axErr
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

func frontmostAppPID() int32 {
	if err := initDarwinAX(); err != nil {
		return 0
	}
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

type darwinAppSnap struct {
	running   bool
	frontmost bool
	winCount  int
	frontPID  int32
}

// queryDarwinAppSnap inspects NSRunningApplication + CGWindowListCopyWindowInfo
// directly from a background goroutine (never touching the main UI thread).
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
			// Include normal windows (0) and app utility/floating windows (<20),
			// while excluding Menu Bar / Status Bar items (>=24) and Desktop (<0).
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

func startDetached(bin string, args ...string) error {
	cmd := exec.Command(bin, args...)
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() {
		_ = cmd.Wait()
	}()
	return nil
}

// launchAndWaitDarwin spawns `/usr/bin/open` asynchronously (~1ms, zero main-thread blocking)
// and polls the macOS WindowServer on the background goroutine until the target app's
// window actually appears on screen (or becomes frontmost).
func launchAndWaitDarwin(openArgs []string, bundleIDs []string, expectNewWin bool) error {
	if err := initDarwinAX(); err != nil {
		return startDetached("/usr/bin/open", openArgs...)
	}

	before := queryDarwinAppSnap(bundleIDs)
	if err := startDetached("/usr/bin/open", openArgs...); err != nil {
		return err
	}

	// Determine wait budget based on whether the target app is cold-starting,
	// reopening a closed window, or simply switching focus.
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
		time.Sleep(60 * time.Millisecond)
		return nil
	}

	deadline := time.Now().Add(maxWait)
	var frontSince time.Time

	for time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
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
			// Cold start or reopening window from Dock: wait until an actual window renders!
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

func (e *Executor) openTargetPlatform(target string) error {
	switch target {
	case "files":
		home, err := os.UserHomeDir()
		if err != nil || home == "" {
			home = "/"
		}
		return launchAndWaitDarwin([]string{home}, []string{"com.apple.finder"}, true)

	case "chrome":
		candidates := []string{"com.google.Chrome", "company.thebrowser.Browser", "com.apple.Safari"}
		if app, ok := e.findInstalledApp(
			candidates,
			[]string{"Google Chrome", "Arc", "Safari"},
		); ok && app.Path != "" {
			return launchAndWaitDarwin([]string{app.Path}, bundleSliceForInstalled(app.ID, candidates), false)
		}
		if err := launchAndWaitDarwin([]string{"-a", "Google Chrome"}, candidates, false); err == nil {
			return nil
		}
		return startDetached("/usr/bin/open", "https://www.google.com")

	case "zed":
		candidates := []string{"dev.zed.Zed", "dev.zed.Zed-Preview", "com.todesktop.230313mzl4w4u92", "com.microsoft.VSCode"}
		if app, ok := e.findInstalledApp(
			candidates,
			[]string{"Zed", "Cursor", "Visual Studio Code"},
		); ok && app.Path != "" {
			return launchAndWaitDarwin([]string{app.Path}, bundleSliceForInstalled(app.ID, candidates), false)
		}
		return launchAndWaitDarwin([]string{"-a", "Zed"}, candidates, false)

	case "terminal":
		candidates := []string{"com.mitchellh.ghostty", "com.googlecode.iterm2", "com.apple.Terminal"}
		if app, ok := e.findInstalledApp(
			candidates,
			[]string{"Ghostty", "iTerm", "Terminal"},
		); ok && app.Path != "" {
			return launchAndWaitDarwin([]string{app.Path}, bundleSliceForInstalled(app.ID, candidates), false)
		}
		return launchAndWaitDarwin([]string{"-a", "Terminal"}, candidates, false)

	default:
		return launchAndWaitDarwin([]string{"-a", target}, nil, false)
	}
}

func closeActiveWindowPlatform() error {
	if err := initDarwinAX(); err != nil {
		return err
	}

	selfPID := int32(os.Getpid())

	// Resolve focused or frontmost application AXUIElement
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
		pid := frontmostAppPID()
		if pid <= 0 || pid == selfPID {
			return errors.New("no eligible frontmost application window")
		}
		appEl = axCreateApplication(pid)
	}
	if appEl == 0 {
		return errors.New("unable to acquire frontmost application AX element")
	}
	defer cfReleaseRef(appEl)

	var targetPID int32
	if axGetPid(appEl, &targetPID) == 0 && targetPID == selfPID {
		return nil
	}

	// Obtain AXFocusedWindow (fallback to AXMainWindow)
	var winEl uintptr
	attrFocusedWin := makeCFString("AXFocusedWindow")
	defer cfReleaseRef(attrFocusedWin)
	if axCopyAttributeValue(appEl, attrFocusedWin, &winEl) != 0 || winEl == 0 {
		attrMainWin := makeCFString("AXMainWindow")
		defer cfReleaseRef(attrMainWin)
		_ = axCopyAttributeValue(appEl, attrMainWin, &winEl)
	}
	if winEl == 0 {
		return errors.New("frontmost application has no open window to close")
	}
	defer cfReleaseRef(winEl)

	// Press the native AXCloseButton of that window
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
		return fmt.Errorf("AXPress on window close button returned %d", rc)
	}
	return nil
}
