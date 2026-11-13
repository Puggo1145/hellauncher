//go:build windows

package sys

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"

	"helldriver/internal/catalog"
)

const (
	whKeyboardLL = 13
	whMouseLL    = 14

	wmClose           = 0x0010
	wmQuit            = 0x0012
	wmKeyDown         = 0x0100
	wmKeyUp           = 0x0101
	wmSysKeyDown      = 0x0104
	wmSysKeyUp        = 0x0105
	wmLButtonDown     = 0x0201
	wmRButtonDown     = 0x0204
	wmAppMouseCapture = 0x8001

	swpNoSize   = 0x0001
	swpNoZOrder = 0x0004

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

type winRect struct {
	Left, Top, Right, Bottom int32
}

type msLLHookStruct struct {
	Pt          winPoint
	MouseData   uint32
	Flags       uint32
	Time        uint32
	DwExtraInfo uintptr
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
	user32                    = syscall.NewLazyDLL("user32.dll")
	kernel32                  = syscall.NewLazyDLL("kernel32.dll")
	procSetWindowsHookExW     = user32.NewProc("SetWindowsHookExW")
	procCallNextHookEx        = user32.NewProc("CallNextHookEx")
	procUnhookWindowsHookEx   = user32.NewProc("UnhookWindowsHookEx")
	procGetMessageW           = user32.NewProc("GetMessageW")
	procPostThreadMessageW    = user32.NewProc("PostThreadMessageW")
	procGetCurrentThreadId    = kernel32.NewProc("GetCurrentThreadId")
	procGetForegroundWindow   = user32.NewProc("GetForegroundWindow")
	procGetDesktopWindow      = user32.NewProc("GetDesktopWindow")
	procGetShellWindow        = user32.NewProc("GetShellWindow")
	procGetWindowThreadProcId = user32.NewProc("GetWindowThreadProcessId")
	procGetClassNameW         = user32.NewProc("GetClassNameW")
	procIsWindowVisible       = user32.NewProc("IsWindowVisible")
	procGetWindowRect         = user32.NewProc("GetWindowRect")
	procSetWindowPos          = user32.NewProc("SetWindowPos")
	procPostMessageW          = user32.NewProc("PostMessageW")

	winHookOnce  sync.Once
	winKeyHookCB uintptr
	winMouseCB   uintptr
	activeWinRig atomic.Pointer[WindowsRig]

	winStateMu sync.Mutex
	winHeldVK  = map[uint16]bool{}
)

func CheckTrusted() bool { return true }

func GuidePermission(_ context.Context) (bool, error) { return true, nil }

func HasConflictingBundleRunning(_ string) bool { return false }

type WindowsRig struct {
	onEvent      func(InputEvent) bool
	hookThreadID atomic.Uint32
	mouseCapture atomic.Bool
}

func NewPlatformRig() Rig {
	return &WindowsRig{}
}

func (r *WindowsRig) StartInput(
	onEvent func(InputEvent) (swallow bool),
	onTrust func(trusted bool),
) (stop func()) {
	winHookOnce.Do(func() {
		winKeyHookCB = syscall.NewCallback(winKeyboardProc)
		winMouseCB = syscall.NewCallback(winMouseProc)
	})
	r.onEvent = onEvent
	activeWinRig.Store(r)
	if onTrust != nil {
		go onTrust(true)
	}

	var stopOnce sync.Once
	stopFn := func() {
		stopOnce.Do(func() {
			r.mouseCapture.Store(false)
			if tid := r.hookThreadID.Load(); tid != 0 {
				procPostThreadMessageW.Call(uintptr(tid), wmQuit, 0, 0)
			}
		})
	}

	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()

		tid, _, _ := procGetCurrentThreadId.Call()
		r.hookThreadID.Store(uint32(tid))

		keyHook, _, err := procSetWindowsHookExW.Call(whKeyboardLL, winKeyHookCB, 0, 0)
		if keyHook == 0 {
			slog.Error("windows SetWindowsHookExW failed", "err", err)
			return
		}
		defer procUnhookWindowsHookEx.Call(keyHook)

		var mouseHook uintptr
		defer func() {
			if mouseHook != 0 {
				procUnhookWindowsHookEx.Call(mouseHook)
			}
		}()

		syncMouseHook := func() {
			want := r.mouseCapture.Load()
			if want && mouseHook == 0 {
				mouseHook, _, _ = procSetWindowsHookExW.Call(whMouseLL, winMouseCB, 0, 0)
			} else if !want && mouseHook != 0 {
				procUnhookWindowsHookEx.Call(mouseHook)
				mouseHook = 0
			}
		}
		syncMouseHook()

		slog.Info("windows WH_KEYBOARD_LL active")
		var msg winMsg
		for {
			ret, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&msg)), 0, 0, 0)
			if int32(ret) <= 0 {
				break
			}
			if msg.Message == wmAppMouseCapture {
				syncMouseHook()
			}
		}
	}()

	return stopFn
}

func (r *WindowsRig) SetMouseCapture(enabled bool) {
	r.mouseCapture.Store(enabled)
	if tid := r.hookThreadID.Load(); tid != 0 {
		var flag uintptr
		if enabled {
			flag = 1
		}
		procPostThreadMessageW.Call(uintptr(tid), wmAppMouseCapture, flag, 0)
	}
}

func winKeyboardProc(nCode int, wParam uintptr, info *kbdLLHookStruct) uintptr {
	lParam := uintptr(unsafe.Pointer(info))
	if nCode < 0 || info == nil {
		ret, _, _ := procCallNextHookEx.Call(0, uintptr(nCode), wParam, lParam)
		return ret
	}

	r := activeWinRig.Load()
	if r == nil || r.onEvent == nil {
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
			r.onEvent(InputEvent{At: now, Kind: InputModifiers, Mods: mods})
		}
		ret, _, _ := procCallNextHookEx.Call(0, uintptr(nCode), wParam, lParam)
		return ret
	}

	if isDown {
		if r.onEvent(InputEvent{
			At:      now,
			Kind:    InputKeyDown,
			KeyCode: vk,
			Role:    classifyWinVK(vk),
			Repeat:  wasDown,
		}) {
			return 1
		}
	} else if isUp {
		if r.onEvent(InputEvent{At: now, Kind: InputKeyUp, KeyCode: vk}) {
			return 1
		}
	}

	ret, _, _ := procCallNextHookEx.Call(0, uintptr(nCode), wParam, lParam)
	return ret
}

func winMouseProc(nCode int, wParam uintptr, info *msLLHookStruct) uintptr {
	lParam := uintptr(unsafe.Pointer(info))
	if nCode < 0 || info == nil {
		ret, _, _ := procCallNextHookEx.Call(0, uintptr(nCode), wParam, lParam)
		return ret
	}

	r := activeWinRig.Load()
	if r == nil || r.onEvent == nil || !r.mouseCapture.Load() {
		ret, _, _ := procCallNextHookEx.Call(0, uintptr(nCode), wParam, lParam)
		return ret
	}

	pt := Point{X: int(info.Pt.X), Y: int(info.Pt.Y)}
	switch wParam {
	case wmLButtonDown:
		if r.onEvent(InputEvent{At: time.Now(), Kind: InputMouseLeftDown, Point: pt}) {
			return 1
		}
	case wmRButtonDown:
		if r.onEvent(InputEvent{At: time.Now(), Kind: InputMouseRightDown, Point: pt}) {
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

func (r *WindowsRig) Execute(ctx context.Context, step catalog.Step, beacon *Point) error {
	switch step.Kind {
	case catalog.StepLaunchApp:
		if err := launchTargetWin(ctx, step.Target); err != nil {
			return err
		}
		if beacon != nil {
			centerForegroundWin(*beacon)
		}
		return nil

	case catalog.StepCloseActiveWindow:
		return closeActiveWindowWin()

	case catalog.StepOpenTarget:
		if err := startHiddenWin(ctx, "explorer.exe", step.Target); err != nil {
			return err
		}
		if beacon != nil {
			centerForegroundWin(*beacon)
		}
		return nil

	case catalog.StepRunShortcut, catalog.StepShellCommand:
		cmd := exec.CommandContext(ctx, "powershell.exe", "-NoProfile", "-WindowStyle", "Hidden", "-Command", step.Target)
		cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
		return cmd.Run()

	default:
		return fmt.Errorf("sys: unsupported step kind %q", step.Kind)
	}
}

func startHiddenWin(ctx context.Context, bin string, args ...string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	cmd := exec.Command(bin, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() {
		_ = cmd.Wait()
	}()
	return nil
}

func foregroundHwnd() uintptr {
	hwnd, _, _ := procGetForegroundWindow.Call()
	return hwnd
}

func isAppWindowVisible(hwnd uintptr) bool {
	if hwnd == 0 {
		return false
	}
	vis, _, _ := procIsWindowVisible.Call(hwnd)
	if vis == 0 {
		return false
	}
	desk, _, _ := procGetDesktopWindow.Call()
	shell, _, _ := procGetShellWindow.Call()
	return hwnd != desk && hwnd != shell
}

func launchAndWaitWin(ctx context.Context, launchFn func() error, maxWait time.Duration) error {
	beforeHwnd := foregroundHwnd()
	if err := launchFn(); err != nil {
		return err
	}

	deadline := time.Now().Add(maxWait)
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(50 * time.Millisecond):
		}
		cur := foregroundHwnd()
		if cur != 0 && cur != beforeHwnd && isAppWindowVisible(cur) {
			return nil
		}
	}
	return nil
}

func launchTargetWin(ctx context.Context, target string) error {
	switch strings.ToLower(strings.TrimSpace(target)) {
	case "files", "explorer":
		return launchAndWaitWin(ctx, func() error {
			return startHiddenWin(ctx, "explorer.exe")
		}, 3500*time.Millisecond)

	case "chrome":
		if app, ok := findInstalledApp([]string{"Chrome"}, []string{"Google Chrome", "Microsoft Edge"}); ok && app.Path != "" {
			return launchAndWaitWin(ctx, func() error {
				return startHiddenWin(ctx, "explorer.exe", app.Path)
			}, 5000*time.Millisecond)
		}
		if err := launchAndWaitWin(ctx, func() error {
			return startHiddenWin(ctx, "cmd", "/c", "start", "", "chrome")
		}, 5000*time.Millisecond); err == nil {
			return nil
		}
		return startHiddenWin(ctx, "explorer.exe", "https://www.google.com")

	case "zed":
		if app, ok := findInstalledApp([]string{"Zed.Zed"}, []string{"Zed", "Cursor", "Visual Studio Code"}); ok && app.Path != "" {
			return launchAndWaitWin(ctx, func() error {
				return startHiddenWin(ctx, "explorer.exe", app.Path)
			}, 5500*time.Millisecond)
		}
		return launchAndWaitWin(ctx, func() error {
			return startHiddenWin(ctx, "cmd", "/c", "start", "", "zed")
		}, 5500*time.Millisecond)

	case "terminal":
		if app, ok := findInstalledApp(
			[]string{"Microsoft.WindowsTerminal_8wekyb3d8bbwe!App"},
			[]string{"Windows Terminal", "PowerShell"},
		); ok && app.Path != "" {
			return launchAndWaitWin(ctx, func() error {
				return startHiddenWin(ctx, "explorer.exe", app.Path)
			}, 4500*time.Millisecond)
		}
		return launchAndWaitWin(ctx, func() error {
			if err := startHiddenWin(ctx, "wt.exe"); err == nil {
				return nil
			}
			return startHiddenWin(ctx, "powershell.exe")
		}, 4500*time.Millisecond)

	default:
		if app, ok := findInstalledApp([]string{target}, []string{target}); ok && app.Path != "" {
			return launchAndWaitWin(ctx, func() error {
				return startHiddenWin(ctx, "explorer.exe", app.Path)
			}, 4500*time.Millisecond)
		}
		return launchAndWaitWin(ctx, func() error {
			return startHiddenWin(ctx, "cmd", "/c", "start", "", target)
		}, 4000*time.Millisecond)
	}
}

func centerForegroundWin(beacon Point) {
	hwnd := foregroundHwnd()
	if !isAppWindowVisible(hwnd) {
		return
	}
	var pid uint32
	procGetWindowThreadProcId.Call(hwnd, uintptr(unsafe.Pointer(&pid)))
	if pid == uint32(os.Getpid()) {
		return
	}
	var rc winRect
	ret, _, _ := procGetWindowRect.Call(hwnd, uintptr(unsafe.Pointer(&rc)))
	if ret == 0 {
		return
	}
	w := int(rc.Right - rc.Left)
	h := int(rc.Bottom - rc.Top)
	if w <= 0 || h <= 0 {
		return
	}
	x := max(0, beacon.X-w/2)
	y := max(0, beacon.Y-h/2)
	procSetWindowPos.Call(hwnd, 0, uintptr(x), uintptr(y), 0, 0, swpNoSize|swpNoZOrder)
}

func closeActiveWindowWin() error {
	hwnd := foregroundHwnd()
	if hwnd == 0 {
		return errors.New("no foreground window")
	}
	desk, _, _ := procGetDesktopWindow.Call()
	shell, _, _ := procGetShellWindow.Call()
	if hwnd == desk || hwnd == shell {
		return nil
	}

	var pid uint32
	procGetWindowThreadProcId.Call(hwnd, uintptr(unsafe.Pointer(&pid)))
	if pid == uint32(os.Getpid()) {
		return nil
	}

	var clsBuf [128]uint16
	n, _, _ := procGetClassNameW.Call(hwnd, uintptr(unsafe.Pointer(&clsBuf[0])), uintptr(len(clsBuf)))
	if n > 0 {
		cls := syscall.UTF16ToString(clsBuf[:n])
		switch cls {
		case "Progman", "WorkerW", "Shell_TrayWnd", "Shell_SecondaryTrayWnd":
			return nil
		}
	}

	procPostMessageW.Call(hwnd, wmClose, 0, 0)
	return nil
}
