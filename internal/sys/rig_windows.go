//go:build windows

package sys

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"

	"hellauncher/internal/catalog"
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
	wmCommand         = 0x0111
	wmTimer           = 0x0113
	wmLButtonDown     = 0x0201
	wmRButtonDown     = 0x0204
	wmAppMouseCapture = 0x8001
	wmAppSyncModTimer = 0x8002

	toggleDesktopCmd = 419

	swShowNormal = 1
	swMaximize   = 3
	swMinimize   = 6
	swRestore    = 9

	swpNoSize     = 0x0001
	swpNoZOrder   = 0x0004
	swpShowWindow = 0x0040

	gwlStyle       = -16
	gwlExStyle     = -20
	wsCaption      = 0x00C00000
	wsExToolWindow = 0x00000080
	wsExAppWindow  = 0x00040000
	gwOwner        = 4

	dwmwaExtendedFrameBounds = 9
	dwmwaCloaked             = 14
	monitorDefaultToNearest  = 2
	keyeventfKeyUp           = 0x0002

	processQueryLimitedInformation = 0x1000
	coinitApartmentThreaded        = 0x2
	seeMaskNoAsync                 = 0x00000100
	seeMaskFlagNoUI                = 0x00000400
	assocstrExecutable             = 2
	assocstrAppID                  = 23

	modReconcileIntervalMs = 150

	vkShift          = 0x10
	vkControl        = 0x11
	vkMenu           = 0x12
	vkEscape         = 0x1B
	vkLeft           = 0x25
	vkUp             = 0x26
	vkRight          = 0x27
	vkDown           = 0x28
	vkA              = 0x41
	vkD              = 0x44
	vkS              = 0x53
	vkW              = 0x57
	vkLWin           = 0x5B
	vkRWin           = 0x5C
	vkLShift         = 0xA0
	vkRShift         = 0xA1
	vkLControl       = 0xA2
	vkRControl       = 0xA3
	vkLMenu          = 0xA4
	vkRMenu          = 0xA5
	vkVolumeMute     = 0xAD
	vkMediaPlayPause = 0xB3
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

type winMonitorInfo struct {
	Size    uint32
	Monitor winRect
	Work    winRect
	Flags   uint32
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

type shellExecuteInfoW struct {
	Size          uint32
	Mask          uint32
	Hwnd          uintptr
	Verb          *uint16
	File          *uint16
	Parameters    *uint16
	Directory     *uint16
	Show          int32
	InstApp       uintptr
	IDList        uintptr
	Class         *uint16
	KeyClass      uintptr
	HotKey        uint32
	IconOrMonitor uintptr
	Process       uintptr
}

func winSystemDirectory() string {
	buf := make([]uint16, syscall.MAX_PATH)
	n, _, _ := syscall.NewLazyDLL("kernel32.dll").NewProc("GetSystemDirectoryW").Call(
		uintptr(unsafe.Pointer(&buf[0])),
		uintptr(len(buf)),
	)
	if n == 0 || int(n) > len(buf) {
		return `C:\Windows\System32`
	}
	return syscall.UTF16ToString(buf[:n])
}

var (
	winSysDir = winSystemDirectory()
	kernel32  = syscall.NewLazyDLL("kernel32.dll")
	user32    = syscall.NewLazyDLL(winSysDir + `\user32.dll`)
	shell32   = syscall.NewLazyDLL(winSysDir + `\shell32.dll`)
	shlwapi   = syscall.NewLazyDLL(winSysDir + `\shlwapi.dll`)
	dwmapi    = syscall.NewLazyDLL(winSysDir + `\dwmapi.dll`)
	ole32     = syscall.NewLazyDLL(winSysDir + `\ole32.dll`)

	procGetCurrentThreadId         = kernel32.NewProc("GetCurrentThreadId")
	procOpenProcess                = kernel32.NewProc("OpenProcess")
	procCloseHandle                = kernel32.NewProc("CloseHandle")
	procQueryFullProcessImageNameW = kernel32.NewProc("QueryFullProcessImageNameW")

	procSetWindowsHookExW     = user32.NewProc("SetWindowsHookExW")
	procCallNextHookEx        = user32.NewProc("CallNextHookEx")
	procUnhookWindowsHookEx   = user32.NewProc("UnhookWindowsHookEx")
	procGetAsyncKeyState      = user32.NewProc("GetAsyncKeyState")
	procGetMessageW           = user32.NewProc("GetMessageW")
	procPostThreadMessageW    = user32.NewProc("PostThreadMessageW")
	procSetTimer              = user32.NewProc("SetTimer")
	procKillTimer             = user32.NewProc("KillTimer")
	procGetForegroundWindow   = user32.NewProc("GetForegroundWindow")
	procSetForegroundWindow   = user32.NewProc("SetForegroundWindow")
	procAllowSetForegroundWin = user32.NewProc("AllowSetForegroundWindow")
	procBringWindowToTop      = user32.NewProc("BringWindowToTop")
	procShowWindow            = user32.NewProc("ShowWindow")
	procIsIconic              = user32.NewProc("IsIconic")
	procIsZoomed              = user32.NewProc("IsZoomed")
	procAttachThreadInput     = user32.NewProc("AttachThreadInput")
	procKeybdEvent            = user32.NewProc("keybd_event")
	procEnumWindows           = user32.NewProc("EnumWindows")
	procGetWindow             = user32.NewProc("GetWindow")
	procGetWindowLongPtrW     = user32.NewProc("GetWindowLongPtrW")
	procGetWindowLongW        = user32.NewProc("GetWindowLongW")
	procGetDesktopWindow      = user32.NewProc("GetDesktopWindow")
	procGetShellWindow        = user32.NewProc("GetShellWindow")
	procGetWindowThreadProcId = user32.NewProc("GetWindowThreadProcessId")
	procGetClassNameW         = user32.NewProc("GetClassNameW")
	procIsWindowVisible       = user32.NewProc("IsWindowVisible")
	procGetWindowRect         = user32.NewProc("GetWindowRect")
	procSetWindowPos          = user32.NewProc("SetWindowPos")
	procPostMessageW          = user32.NewProc("PostMessageW")
	procFindWindowW           = user32.NewProc("FindWindowW")
	procLockWorkStation       = user32.NewProc("LockWorkStation")
	procMonitorFromRect       = user32.NewProc("MonitorFromRect")
	procMonitorFromWindow     = user32.NewProc("MonitorFromWindow")
	procGetMonitorInfoW       = user32.NewProc("GetMonitorInfoW")

	procShellExecuteExW    = shell32.NewProc("ShellExecuteExW")
	procSHEmptyRecycleBinW = shell32.NewProc("SHEmptyRecycleBinW")
	procAssocQueryStringW  = shlwapi.NewProc("AssocQueryStringW")
	procDwmGetWindowAttrib = dwmapi.NewProc("DwmGetWindowAttribute")
	procCoInitializeEx     = ole32.NewProc("CoInitializeEx")
	procCoUninitialize     = ole32.NewProc("CoUninitialize")

	winHookOnce  sync.Once
	winKeyHookCB uintptr
	winMouseCB   uintptr
	winEnumOnce  sync.Once
	winEnumCB    uintptr
	winEnumMu    sync.Mutex
	winEnumVisit func(hwnd uintptr) bool

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
	modGuardOn   atomic.Bool
	stopped      atomic.Bool
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

	winStateMu.Lock()
	clear(winHeldVK)
	winStateMu.Unlock()

	r.stopped.Store(false)
	r.modGuardOn.Store(false)
	r.onEvent = onEvent
	activeWinRig.Store(r)
	if onTrust != nil {
		go onTrust(true)
	}

	var stopOnce sync.Once
	stopFn := func() {
		stopOnce.Do(func() {
			r.stopped.Store(true)
			r.mouseCapture.Store(false)
			if tid := r.hookThreadID.Load(); tid != 0 {
				_, _, _ = procPostThreadMessageW.Call(uintptr(tid), wmQuit, 0, 0)
			}
		})
	}

	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()

		tid, _, _ := procGetCurrentThreadId.Call()
		r.hookThreadID.Store(uint32(tid))
		defer func() {
			r.hookThreadID.Store(0)
			r.modGuardOn.Store(false)
			winStateMu.Lock()
			clear(winHeldVK)
			winStateMu.Unlock()
		}()

		if r.stopped.Load() {
			return
		}

		keyHook, _, err := procSetWindowsHookExW.Call(whKeyboardLL, winKeyHookCB, 0, 0)
		if keyHook == 0 {
			slog.Error("windows SetWindowsHookExW failed", "err", err)
			return
		}
		defer procUnhookWindowsHookEx.Call(keyHook)

		var mouseHook uintptr
		defer func() {
			if mouseHook != 0 {
				_, _, _ = procUnhookWindowsHookEx.Call(mouseHook)
			}
		}()

		var modTimerID uintptr
		stopModTimer := func() {
			if modTimerID != 0 {
				_, _, _ = procKillTimer.Call(0, modTimerID)
				modTimerID = 0
			}
			r.modGuardOn.Store(false)
		}
		defer stopModTimer()

		syncModTimer := func() {
			winStateMu.Lock()
			want := modsFromHeldLocked(winHeldVK) != (Mods{})
			winStateMu.Unlock()
			if want && modTimerID == 0 {
				id, _, _ := procSetTimer.Call(0, 0, modReconcileIntervalMs, 0)
				modTimerID = id
				r.modGuardOn.Store(id != 0)
			} else if !want && modTimerID != 0 {
				stopModTimer()
			}
		}

		syncMouseHook := func() {
			want := r.mouseCapture.Load()
			if want && mouseHook == 0 {
				mouseHook, _, _ = procSetWindowsHookExW.Call(whMouseLL, winMouseCB, 0, 0)
			} else if !want && mouseHook != 0 {
				_, _, _ = procUnhookWindowsHookEx.Call(mouseHook)
				mouseHook = 0
			}
		}
		syncMouseHook()

		if r.stopped.Load() {
			return
		}

		slog.Info("windows WH_KEYBOARD_LL active")
		var msg winMsg
		for {
			ret, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&msg)), 0, 0, 0)
			if int32(ret) <= 0 {
				break
			}
			switch msg.Message {
			case wmAppMouseCapture:
				syncMouseHook()
			case wmAppSyncModTimer:
				syncModTimer()
			case wmTimer:
				var (
					emitMods  bool
					nextMods  Mods
					keepTimer bool
				)
				winStateMu.Lock()
				before := modsFromHeldLocked(winHeldVK)
				if reconcileHeldVKLocked(winHeldVK, isAsyncKeyDown) {
					after := modsFromHeldLocked(winHeldVK)
					emitMods = after != before
					nextMods = after
				}
				keepTimer = modsFromHeldLocked(winHeldVK) != (Mods{})
				winStateMu.Unlock()

				if !keepTimer {
					stopModTimer()
				}
				if emitMods && r.onEvent != nil {
					r.onEvent(InputEvent{At: time.Now(), Kind: InputModifiers, Mods: nextMods})
				}
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
		_, _, _ = procPostThreadMessageW.Call(uintptr(tid), wmAppMouseCapture, flag, 0)
	}
}

func isAsyncKeyDown(vk uint16) bool {
	state, _, _ := procGetAsyncKeyState.Call(uintptr(vk))
	return uint16(state)&0x8000 != 0
}

func reconcileHeldVKLocked(held map[uint16]bool, isDownFn func(uint16) bool) bool {
	changed := false
	for k := range held {
		if !isDownFn(k) {
			delete(held, k)
			changed = true
		}
	}
	return changed
}

func reconcileOtherHeldVKLocked(held map[uint16]bool, activeVK uint16, isDownFn func(uint16) bool) {
	for k := range held {
		if k != activeVK && !isDownFn(k) {
			delete(held, k)
		}
	}
}

func modsFromHeldLocked(held map[uint16]bool) Mods {
	return Mods{
		LeftCtrl:  held[vkLControl] || (held[vkControl] && !held[vkRControl]),
		RightCtrl: held[vkRControl],
		Alt:       held[vkLMenu] || held[vkRMenu] || held[vkMenu],
		Shift:     held[vkLShift] || held[vkRShift] || held[vkShift],
		Super:     held[vkLWin] || held[vkRWin],
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
	reconcileOtherHeldVKLocked(winHeldVK, vk, isAsyncKeyDown)
	wasDown := winHeldVK[vk]
	if isDown {
		winHeldVK[vk] = true
	} else if isUp {
		delete(winHeldVK, vk)
	}
	mods := modsFromHeldLocked(winHeldVK)
	winStateMu.Unlock()

	hasMods := mods != (Mods{})
	if hasMods != r.modGuardOn.Load() {
		if tid := r.hookThreadID.Load(); tid != 0 {
			_, _, _ = procPostThreadMessageW.Call(uintptr(tid), wmAppSyncModTimer, 0, 0)
		}
	}

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
		target := expandWinTarget(ExpandDynamicTokens(step.Target, true))
		if target == "" {
			return errors.New("empty open target")
		}
		before := foregroundHwnd()
		if err := shellExecuteNoUI("open", target, ""); err != nil {
			return err
		}
		if beacon != nil {
			waitForForegroundChangeWin(ctx, before, 1400*time.Millisecond)
			centerForegroundWin(*beacon)
		}
		return nil

	case catalog.StepRunShortcut:
		target := expandWinTarget(ExpandDynamicTokens(step.Target, false))
		ext := strings.ToLower(filepath.Ext(target))
		if ext == ".lnk" || ext == ".url" {
			if _, err := os.Stat(target); err == nil {
				return shellExecuteNoUI("open", target, "")
			}
		}
		return runPowerShellHidden(ctx, ExpandDynamicTokens(step.Target, false))

	case catalog.StepShellCommand:
		return runPowerShellHidden(ctx, ExpandDynamicTokens(step.Target, false))

	case catalog.StepSystemAction:
		return executeSystemActionWin(catalog.SystemAction(strings.TrimSpace(step.Target)))

	case catalog.StepLaunchHelldivers:
		if activateExistingByExes(ctx, "helldivers2.exe") {
			return nil
		}
		st := DetectHelldivers(step.Target)
		if !st.Found {
			return ErrHelldiversMissing
		}
		if st.LaunchURI != "" {
			if err := shellExecuteNoUI("open", st.LaunchURI, ""); err == nil {
				return nil
			}
		}
		if st.Path != "" {
			return shellExecuteNoUI("open", st.Path, "")
		}
		return ErrHelldiversMissing

	default:
		return fmt.Errorf("sys: unsupported step kind %q", step.Kind)
	}
}

func runPowerShellHidden(ctx context.Context, script string) error {
	shellBin := "powershell.exe"
	if pwsh, err := exec.LookPath("pwsh.exe"); err == nil && pwsh != "" {
		shellBin = pwsh
	}
	cmd := exec.CommandContext(
		ctx,
		shellBin,
		"-NoProfile",
		"-NonInteractive",
		"-WindowStyle",
		"Hidden",
		"-Command",
		script,
	)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	return cmd.Run()
}

func expandWinTarget(raw string) string {
	s := strings.TrimSpace(raw)
	if s == "" {
		return ""
	}
	if s == "~" || strings.HasPrefix(s, "~/") || strings.HasPrefix(s, `~\`) {
		if home, err := os.UserHomeDir(); err == nil && home != "" {
			if s == "~" {
				s = home
			} else {
				s = filepath.Join(home, s[2:])
			}
		}
	}
	s = expandPercentEnv(s, os.Getenv)
	s = os.ExpandEnv(s)
	return s
}

func expandPercentEnv(s string, lookup func(string) string) string {
	if !strings.Contains(s, "%") {
		return s
	}
	var out strings.Builder
	out.Grow(len(s))
	for i := 0; i < len(s); {
		if s[i] == '%' {
			end := strings.IndexByte(s[i+1:], '%')
			if end > 0 {
				name := s[i+1 : i+1+end]
				if val := lookup(name); val != "" {
					out.WriteString(val)
					i += end + 2
					continue
				}
			}
		}
		out.WriteByte(s[i])
		i++
	}
	return out.String()
}

func shellExecuteNoUI(verb, file, params string) error {
	if strings.TrimSpace(file) == "" {
		return errors.New("sys: empty target path")
	}
	filePtr, err := syscall.UTF16PtrFromString(file)
	if err != nil {
		return err
	}
	var verbPtr, paramsPtr *uint16
	if verb != "" {
		verbPtr, err = syscall.UTF16PtrFromString(verb)
		if err != nil {
			return err
		}
	}
	if params != "" {
		paramsPtr, err = syscall.UTF16PtrFromString(params)
		if err != nil {
			return err
		}
	}

	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	hr, _, _ := procCoInitializeEx.Call(0, coinitApartmentThreaded)
	if int32(hr) >= 0 {
		defer procCoUninitialize.Call()
	}

	sei := shellExecuteInfoW{
		Mask:       seeMaskFlagNoUI | seeMaskNoAsync,
		Verb:       verbPtr,
		File:       filePtr,
		Parameters: paramsPtr,
		Show:       swShowNormal,
	}
	sei.Size = uint32(unsafe.Sizeof(sei))

	ret, _, callErr := procShellExecuteExW.Call(uintptr(unsafe.Pointer(&sei)))
	if ret == 0 {
		if callErr != nil && callErr != syscall.Errno(0) {
			return fmt.Errorf("ShellExecuteExW %q: %w", file, callErr)
		}
		return fmt.Errorf("ShellExecuteExW %q failed", file)
	}
	return nil
}

func listExplorerWindowsWin() map[uintptr]struct{} {
	out := map[uintptr]struct{}{}
	enumTopWindows(func(hwnd uintptr) bool {
		if !isAppWindowVisible(hwnd) {
			return false
		}
		cls := winClassName(hwnd)
		if cls == "CabinetWClass" || cls == "ExploreWClass" {
			out[hwnd] = struct{}{}
		}
		return false
	})
	return out
}

func openWindowsExplorer(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	before := listExplorerWindowsWin()

	const asfwAny = ^uintptr(0)
	_, _, _ = procAllowSetForegroundWin.Call(asfwAny)

	explorerBin := filepath.Join(filepath.Dir(winSysDir), "explorer.exe")
	if _, err := os.Stat(explorerBin); err != nil {
		explorerBin = "explorer.exe"
	}

	// Never set SysProcAttr.HideWindow on explorer.exe: HideWindow sets
	// STARTUPINFO.wShowWindow = SW_HIDE, which causes Windows Explorer to spawn
	// an invisible (Visible=False) shell window instead of showing File Explorer.
	cmd := exec.Command(explorerBin)
	if err := cmd.Start(); err != nil {
		if fallbackErr := shellExecuteNoUI("open", explorerBin, ""); fallbackErr != nil {
			return err
		}
	} else {
		go func() {
			_ = cmd.Wait()
		}()
	}

	deadline := time.Now().Add(2600 * time.Millisecond)
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(40 * time.Millisecond):
		}
		now := listExplorerWindowsWin()
		for hwnd := range now {
			if _, existed := before[hwnd]; !existed {
				activateWindowWin(hwnd)
				return nil
			}
		}
	}

	// Fallback if Windows reused an existing Explorer window.
	for hwnd := range listExplorerWindowsWin() {
		activateWindowWin(hwnd)
		break
	}
	return nil
}

func foregroundHwnd() uintptr {
	hwnd, _, _ := procGetForegroundWindow.Call()
	return hwnd
}

func getWinExStyle(hwnd uintptr) uintptr {
	index := int32(gwlExStyle)
	idx := uintptr(index)
	if procGetWindowLongPtrW.Find() == nil {
		v, _, _ := procGetWindowLongPtrW.Call(hwnd, idx)
		return v
	}
	v, _, _ := procGetWindowLongW.Call(hwnd, idx)
	return v
}

func winClassName(hwnd uintptr) string {
	var clsBuf [128]uint16
	n, _, _ := procGetClassNameW.Call(hwnd, uintptr(unsafe.Pointer(&clsBuf[0])), uintptr(len(clsBuf)))
	if n == 0 {
		return ""
	}
	return syscall.UTF16ToString(clsBuf[:n])
}

func isShellWindowClass(cls string) bool {
	switch cls {
	case "Progman",
		"WorkerW",
		"Shell_TrayWnd",
		"Shell_SecondaryTrayWnd",
		"Windows.UI.Core.CoreWindow",
		"XamlExplorerHostIslandWindow",
		"MultitaskingViewFrame",
		"ForegroundStaging":
		return true
	default:
		return false
	}
}

func isWindowCloaked(hwnd uintptr) bool {
	if procDwmGetWindowAttrib.Find() != nil {
		return false
	}
	var cloaked uint32
	hr, _, _ := procDwmGetWindowAttrib.Call(
		hwnd,
		dwmwaCloaked,
		uintptr(unsafe.Pointer(&cloaked)),
		unsafe.Sizeof(cloaked),
	)
	return int32(hr) >= 0 && cloaked != 0
}

func isAppWindowVisible(hwnd uintptr) bool {
	if hwnd == 0 {
		return false
	}
	vis, _, _ := procIsWindowVisible.Call(hwnd)
	if vis == 0 || isWindowCloaked(hwnd) {
		return false
	}
	desk, _, _ := procGetDesktopWindow.Call()
	shell, _, _ := procGetShellWindow.Call()
	if hwnd == desk || hwnd == shell {
		return false
	}
	return !isShellWindowClass(winClassName(hwnd))
}

func isEligibleSwitchWindow(hwnd uintptr) bool {
	if !isAppWindowVisible(hwnd) {
		return false
	}
	ex := getWinExStyle(hwnd)
	if ex&wsExToolWindow != 0 && ex&wsExAppWindow == 0 {
		return false
	}
	owner, _, _ := procGetWindow.Call(hwnd, gwOwner)
	if owner != 0 && ex&wsExAppWindow == 0 {
		return false
	}
	iconic, _, _ := procIsIconic.Call(hwnd)
	if iconic == 0 {
		var rc winRect
		if ret, _, _ := procGetWindowRect.Call(hwnd, uintptr(unsafe.Pointer(&rc))); ret == 0 {
			return false
		}
		if (rc.Right-rc.Left) < 120 || (rc.Bottom-rc.Top) < 80 {
			return false
		}
	}
	return true
}

func processExeBase(pid uint32) string {
	if pid == 0 {
		return ""
	}
	h, _, _ := procOpenProcess.Call(processQueryLimitedInformation, 0, uintptr(pid))
	if h == 0 {
		return ""
	}
	defer procCloseHandle.Call(h)

	var buf [1024]uint16
	sz := uint32(len(buf))
	ret, _, _ := procQueryFullProcessImageNameW.Call(
		h,
		0,
		uintptr(unsafe.Pointer(&buf[0])),
		uintptr(unsafe.Pointer(&sz)),
	)
	if ret == 0 || sz == 0 {
		return ""
	}
	return filepath.Base(syscall.UTF16ToString(buf[:sz]))
}

func enumTopWindows(visit func(hwnd uintptr) (stop bool)) {
	winEnumOnce.Do(func() {
		winEnumCB = syscall.NewCallback(func(hwnd, _ uintptr) uintptr {
			if winEnumVisit != nil && winEnumVisit(hwnd) {
				return 0
			}
			return 1
		})
	})

	winEnumMu.Lock()
	defer winEnumMu.Unlock()
	winEnumVisit = visit
	defer func() { winEnumVisit = nil }()
	_, _, _ = procEnumWindows.Call(winEnumCB, 0)
}

func findAppWindowByExes(exeNames []string) uintptr {
	if len(exeNames) == 0 {
		return 0
	}
	selfPID := uint32(os.Getpid())
	byExe := make(map[string]uintptr, len(exeNames))

	enumTopWindows(func(hwnd uintptr) bool {
		if !isEligibleSwitchWindow(hwnd) {
			return false
		}
		var pid uint32
		_, _, _ = procGetWindowThreadProcId.Call(hwnd, uintptr(unsafe.Pointer(&pid)))
		if pid == 0 || pid == selfPID {
			return false
		}
		exe := strings.ToLower(processExeBase(pid))
		if exe == "" {
			return false
		}
		for _, want := range exeNames {
			key := strings.ToLower(want)
			if exe == key {
				if _, exists := byExe[key]; !exists {
					byExe[key] = hwnd
				}
			}
		}
		return false
	})

	for _, want := range exeNames {
		if hwnd := byExe[strings.ToLower(want)]; hwnd != 0 {
			return hwnd
		}
	}
	return 0
}

func activateWindowWin(hwnd uintptr) bool {
	if hwnd == 0 {
		return false
	}
	iconic, _, _ := procIsIconic.Call(hwnd)
	if iconic != 0 {
		_, _, _ = procShowWindow.Call(hwnd, swRestore)
	}
	fg := foregroundHwnd()
	if fg == hwnd && iconic == 0 {
		return true
	}

	curTID, _, _ := procGetCurrentThreadId.Call()
	var fgTID uintptr
	if fg != 0 {
		fgTID, _, _ = procGetWindowThreadProcId.Call(fg, 0)
	}
	attached := false
	if fgTID != 0 && fgTID != curTID {
		ret, _, _ := procAttachThreadInput.Call(curTID, fgTID, 1)
		attached = ret != 0
	}
	_, _, _ = procBringWindowToTop.Call(hwnd)
	_, _, _ = procSetForegroundWindow.Call(hwnd)
	if attached {
		_, _, _ = procAttachThreadInput.Call(curTID, fgTID, 0)
	}

	if foregroundHwnd() != hwnd {
		_, _, _ = procKeybdEvent.Call(0, 0, 0, 0)
		_, _, _ = procSetForegroundWindow.Call(hwnd)
	}
	return true
}

func activateExistingByExes(ctx context.Context, exeNames ...string) bool {
	hwnd := findAppWindowByExes(exeNames)
	if hwnd == 0 {
		return false
	}
	if !activateWindowWin(hwnd) {
		return false
	}
	t := time.NewTimer(50 * time.Millisecond)
	defer t.Stop()
	select {
	case <-ctx.Done():
	case <-t.C:
	}
	return true
}

func waitForForegroundChangeWin(ctx context.Context, beforeHwnd uintptr, maxWait time.Duration) {
	deadline := time.Now().Add(maxWait)
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return
		case <-time.After(45 * time.Millisecond):
		}
		cur := foregroundHwnd()
		if cur != 0 && cur != beforeHwnd && isAppWindowVisible(cur) {
			return
		}
	}
}

func launchAndWaitWin(ctx context.Context, launchFn func() error, maxWait time.Duration) error {
	beforeHwnd := foregroundHwnd()
	if err := launchFn(); err != nil {
		return err
	}
	waitForForegroundChangeWin(ctx, beforeHwnd, maxWait)
	return ctx.Err()
}

func assocQueryWin(str uint32, assoc string) string {
	assocPtr, err := syscall.UTF16PtrFromString(assoc)
	if err != nil {
		return ""
	}
	var buf [1024]uint16
	n := uint32(len(buf))
	hr, _, _ := procAssocQueryStringW.Call(
		0,
		uintptr(str),
		uintptr(unsafe.Pointer(assocPtr)),
		0,
		uintptr(unsafe.Pointer(&buf[0])),
		uintptr(unsafe.Pointer(&n)),
	)
	if hr != 0 || n == 0 {
		return ""
	}
	return strings.TrimRight(syscall.UTF16ToString(buf[:n]), "\x00")
}

func defaultHTTPSBrowserWin() (launchPath string, exeBase string) {
	exe := assocQueryWin(assocstrExecutable, "https")
	if exe != "" && !strings.EqualFold(filepath.Base(exe), "OpenWith.exe") {
		return exe, filepath.Base(exe)
	}
	if aumid := assocQueryWin(assocstrAppID, "https"); aumid != "" {
		return `shell:AppsFolder\` + aumid, ""
	}
	return "", ""
}

func launchTargetWin(ctx context.Context, target string) error {
	switch strings.ToLower(strings.TrimSpace(target)) {
	case "files", "finder", "explorer", "explorer.exe", "windows explorer", "文件资源管理器":
		return openWindowsExplorer(ctx)

	case "browser":
		if browserPath, browserExe := defaultHTTPSBrowserWin(); browserPath != "" {
			if browserExe != "" && activateExistingByExes(ctx, browserExe) {
				return nil
			}
			if err := launchAndWaitWin(ctx, func() error {
				return shellExecuteNoUI("", browserPath, "")
			}, 5000*time.Millisecond); err == nil {
				return nil
			}
		}
		if activateExistingByExes(ctx, "chrome.exe", "msedge.exe", "brave.exe", "firefox.exe") {
			return nil
		}
		return shellExecuteNoUI("open", "https://www.bing.com", "")

	case "chrome":
		if activateExistingByExes(ctx, "chrome.exe") {
			return nil
		}
		if app, ok := findInstalledApp([]string{"Chrome"}, []string{"Google Chrome"}); ok && app.Path != "" {
			return launchAndWaitWin(ctx, func() error {
				return shellExecuteNoUI("", app.Path, "")
			}, 5000*time.Millisecond)
		}
		if browserPath, browserExe := defaultHTTPSBrowserWin(); browserPath != "" {
			if browserExe != "" && activateExistingByExes(ctx, browserExe) {
				return nil
			}
			if err := launchAndWaitWin(ctx, func() error {
				return shellExecuteNoUI("", browserPath, "")
			}, 5000*time.Millisecond); err == nil {
				return nil
			}
		}
		if activateExistingByExes(ctx, "msedge.exe", "brave.exe", "firefox.exe") {
			return nil
		}
		if app, ok := findInstalledApp([]string{"MSEdge"}, []string{"Microsoft Edge"}); ok && app.Path != "" {
			return launchAndWaitWin(ctx, func() error {
				return shellExecuteNoUI("", app.Path, "")
			}, 5000*time.Millisecond)
		}
		return shellExecuteNoUI("open", "https://www.bing.com", "")

	case "zed":
		type editorSpec struct {
			ids  []string
			name string
			exe  string
			cli  string
		}
		editors := []editorSpec{
			{ids: []string{"Zed.Zed"}, name: "Zed", exe: "Zed.exe", cli: "zed.exe"},
			{ids: []string{"Google.Antigravity"}, name: "Antigravity", exe: "Antigravity.exe", cli: "Antigravity.exe"},
			{ids: []string{"Anysphere.Cursor"}, name: "Cursor", exe: "Cursor.exe", cli: "Cursor.exe"},
			{ids: []string{"Microsoft.VisualStudioCode"}, name: "Visual Studio Code", exe: "Code.exe", cli: "Code.exe"},
		}
		for _, ed := range editors {
			if app, ok := findInstalledApp(ed.ids, []string{ed.name}); ok && app.Path != "" {
				if activateExistingByExes(ctx, ed.exe) {
					return nil
				}
				return launchAndWaitWin(ctx, func() error {
					return shellExecuteNoUI("", app.Path, "")
				}, 5500*time.Millisecond)
			}
		}
		if activateExistingByExes(ctx, "Zed.exe", "Antigravity.exe", "Cursor.exe", "Code.exe") {
			return nil
		}
		for _, cli := range []string{"zed.exe", "Antigravity.exe", "Cursor.exe", "Code.exe"} {
			if p, err := exec.LookPath(cli); err == nil && p != "" {
				return launchAndWaitWin(ctx, func() error {
					return shellExecuteNoUI("", p, "")
				}, 5500*time.Millisecond)
			}
		}
		return errors.New("未找到已安装的代码编辑器（Zed / Antigravity / Cursor / VS Code）")

	case "terminal":
		if activateExistingByExes(ctx, "WindowsTerminal.exe") {
			return nil
		}
		if app, ok := findInstalledApp(
			[]string{
				"Microsoft.WindowsTerminal_8wekyb3d8bbwe!App",
				"Microsoft.WindowsTerminalPreview_8wekyb3d8bbwe!App",
			},
			[]string{"Windows Terminal", "终端", "Windows Terminal Preview", "PowerShell", "Windows PowerShell"},
		); ok && app.Path != "" {
			return launchAndWaitWin(ctx, func() error {
				return shellExecuteNoUI("", app.Path, "")
			}, 4500*time.Millisecond)
		}
		for _, bin := range []string{"wt.exe", "pwsh.exe", "powershell.exe"} {
			if p, err := exec.LookPath(bin); err == nil && p != "" {
				return launchAndWaitWin(ctx, func() error {
					return shellExecuteNoUI("", p, "")
				}, 4500*time.Millisecond)
			}
		}
		return launchAndWaitWin(ctx, func() error {
			return shellExecuteNoUI("", "powershell.exe", "")
		}, 4500*time.Millisecond)

	default:
		expanded := expandWinTarget(target)
		if expanded == "" {
			return errors.New("empty launch target")
		}
		if strings.HasSuffix(strings.ToLower(expanded), ".exe") {
			if activateExistingByExes(ctx, filepath.Base(expanded)) {
				return nil
			}
		}
		if app, ok := findInstalledApp([]string{target, expanded}, []string{target}); ok && app.Path != "" {
			if strings.HasSuffix(strings.ToLower(app.ID), ".exe") {
				if activateExistingByExes(ctx, filepath.Base(app.ID)) {
					return nil
				}
			}
			return launchAndWaitWin(ctx, func() error {
				return shellExecuteNoUI("", app.Path, "")
			}, 4500*time.Millisecond)
		}
		if p, err := exec.LookPath(expanded); err == nil && p != "" {
			if strings.HasSuffix(strings.ToLower(p), ".exe") && activateExistingByExes(ctx, filepath.Base(p)) {
				return nil
			}
			return launchAndWaitWin(ctx, func() error {
				return shellExecuteNoUI("", p, "")
			}, 4500*time.Millisecond)
		}
		return launchAndWaitWin(ctx, func() error {
			return shellExecuteNoUI("open", expanded, "")
		}, 4000*time.Millisecond)
	}
}

func clampWindowToWorkArea(beacon Point, winW, winH int, work winRect) (int, int) {
	x := beacon.X - winW/2
	y := beacon.Y - winH/2

	workW := int(work.Right - work.Left)
	workH := int(work.Bottom - work.Top)
	if workW <= 0 || workH <= 0 {
		return x, y
	}

	minX := int(work.Left)
	maxX := int(work.Right) - winW
	if maxX < minX {
		x = minX
	} else {
		x = min(max(x, minX), maxX)
	}

	minY := int(work.Top)
	maxY := int(work.Bottom) - winH
	if maxY < minY {
		y = minY
	} else {
		y = min(max(y, minY), maxY)
	}

	return x, y
}

func workAreaNearPointWin(pt Point) (winRect, bool) {
	probe := winRect{
		Left:   int32(pt.X),
		Top:    int32(pt.Y),
		Right:  int32(pt.X + 1),
		Bottom: int32(pt.Y + 1),
	}
	mon, _, _ := procMonitorFromRect.Call(uintptr(unsafe.Pointer(&probe)), monitorDefaultToNearest)
	if mon == 0 {
		return winRect{}, false
	}
	var mi winMonitorInfo
	mi.Size = uint32(unsafe.Sizeof(mi))
	ret, _, _ := procGetMonitorInfoW.Call(mon, uintptr(unsafe.Pointer(&mi)))
	if ret == 0 {
		return winRect{}, false
	}
	return mi.Work, true
}

func centerForegroundWin(beacon Point) {
	hwnd := foregroundHwnd()
	if !isAppWindowVisible(hwnd) {
		return
	}
	var pid uint32
	_, _, _ = procGetWindowThreadProcId.Call(hwnd, uintptr(unsafe.Pointer(&pid)))
	if pid == uint32(os.Getpid()) {
		return
	}

	if zoomed, _, _ := procIsZoomed.Call(hwnd); zoomed != 0 {
		_, _, _ = procShowWindow.Call(hwnd, swRestore)
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

	x := beacon.X - w/2
	y := beacon.Y - h/2
	if work, ok := workAreaNearPointWin(beacon); ok {
		x, y = clampWindowToWorkArea(beacon, w, h, work)
	}
	_, _, _ = procSetWindowPos.Call(hwnd, 0, uintptr(x), uintptr(y), 0, 0, swpNoSize|swpNoZOrder)
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
	_, _, _ = procGetWindowThreadProcId.Call(hwnd, uintptr(unsafe.Pointer(&pid)))
	if pid == uint32(os.Getpid()) {
		return nil
	}

	if isShellWindowClass(winClassName(hwnd)) {
		return nil
	}

	_, _, _ = procPostMessageW.Call(hwnd, wmClose, 0, 0)
	return nil
}

func getWinStyle(hwnd uintptr) uintptr {
	index := int32(gwlStyle)
	idx := uintptr(index)
	if procGetWindowLongPtrW.Find() == nil {
		v, _, _ := procGetWindowLongPtrW.Call(hwnd, idx)
		return v
	}
	v, _, _ := procGetWindowLongW.Call(hwnd, idx)
	return v
}

// ShouldBypassFullscreenOrGame reports whether the active foreground window is
// HELLDIVERS 2 or a borderless/exclusive fullscreen application where global
// Ctrl+WASD stratagem hotkeys should stay silent.
func ShouldBypassFullscreenOrGame() bool {
	hwnd := foregroundHwnd()
	if hwnd == 0 || !isAppWindowVisible(hwnd) {
		return false
	}
	var pid uint32
	_, _, _ = procGetWindowThreadProcId.Call(hwnd, uintptr(unsafe.Pointer(&pid)))
	if pid == 0 || pid == uint32(os.Getpid()) {
		return false
	}
	exe := strings.ToLower(processExeBase(pid))
	if exe == "helldivers2.exe" {
		return true
	}
	if exe == "explorer.exe" {
		return false
	}
	if getWinStyle(hwnd)&wsCaption != 0 {
		return false
	}
	var rc winRect
	if ret, _, _ := procGetWindowRect.Call(hwnd, uintptr(unsafe.Pointer(&rc))); ret == 0 {
		return false
	}
	mon, _, _ := procMonitorFromWindow.Call(hwnd, monitorDefaultToNearest)
	if mon == 0 {
		return false
	}
	var mi winMonitorInfo
	mi.Size = uint32(unsafe.Sizeof(mi))
	if ret, _, _ := procGetMonitorInfoW.Call(mon, uintptr(unsafe.Pointer(&mi))); ret == 0 {
		return false
	}
	return rc.Left <= mi.Monitor.Left &&
		rc.Top <= mi.Monitor.Top &&
		rc.Right >= mi.Monitor.Right &&
		rc.Bottom >= mi.Monitor.Bottom
}

func eligibleForegroundWindowWin() (uintptr, winRect, bool) {
	hwnd := foregroundHwnd()
	if hwnd == 0 || !isAppWindowVisible(hwnd) {
		return 0, winRect{}, false
	}
	var pid uint32
	_, _, _ = procGetWindowThreadProcId.Call(hwnd, uintptr(unsafe.Pointer(&pid)))
	if pid == uint32(os.Getpid()) {
		return 0, winRect{}, false
	}
	mon, _, _ := procMonitorFromWindow.Call(hwnd, monitorDefaultToNearest)
	if mon == 0 {
		return hwnd, winRect{}, false
	}
	var mi winMonitorInfo
	mi.Size = uint32(unsafe.Sizeof(mi))
	if ret, _, _ := procGetMonitorInfoW.Call(mon, uintptr(unsafe.Pointer(&mi))); ret == 0 {
		return hwnd, winRect{}, false
	}
	return hwnd, mi.Work, true
}

func placeWindowCompensatedWin(hwnd uintptr, visX, visY, visW, visH int32) {
	if zoomed, _, _ := procIsZoomed.Call(hwnd); zoomed != 0 {
		_, _, _ = procShowWindow.Call(hwnd, swRestore)
	}
	var rc, ext winRect
	var padL, padT, padR, padB int32
	if ret, _, _ := procGetWindowRect.Call(hwnd, uintptr(unsafe.Pointer(&rc))); ret != 0 && procDwmGetWindowAttrib.Find() == nil {
		hr, _, _ := procDwmGetWindowAttrib.Call(
			hwnd,
			dwmwaExtendedFrameBounds,
			uintptr(unsafe.Pointer(&ext)),
			unsafe.Sizeof(ext),
		)
		if int32(hr) >= 0 {
			padL = max(0, ext.Left-rc.Left)
			padT = max(0, ext.Top-rc.Top)
			padR = max(0, rc.Right-ext.Right)
			padB = max(0, rc.Bottom-ext.Bottom)
		}
	}
	x := int(visX - padL)
	y := int(visY - padT)
	w := int(visW + padL + padR)
	h := int(visH + padT + padB)
	_, _, _ = procSetWindowPos.Call(hwnd, 0, uintptr(x), uintptr(y), uintptr(w), uintptr(h), swpNoZOrder|swpShowWindow)
}

func tapVirtualKeyWin(vk byte) {
	_, _, _ = procKeybdEvent.Call(uintptr(vk), 0, 0, 0)
	_, _, _ = procKeybdEvent.Call(uintptr(vk), 0, keyeventfKeyUp, 0)
}

func executeSystemActionWin(act catalog.SystemAction) error {
	switch act {
	case catalog.SysWinLeft:
		hwnd, work, ok := eligibleForegroundWindowWin()
		if !ok {
			return nil
		}
		workW := work.Right - work.Left
		workH := work.Bottom - work.Top
		placeWindowCompensatedWin(hwnd, work.Left, work.Top, workW/2, workH)
		return nil

	case catalog.SysWinRight:
		hwnd, work, ok := eligibleForegroundWindowWin()
		if !ok {
			return nil
		}
		workW := work.Right - work.Left
		workH := work.Bottom - work.Top
		half := workW / 2
		placeWindowCompensatedWin(hwnd, work.Left+half, work.Top, workW-half, workH)
		return nil

	case catalog.SysWinMaximize:
		hwnd, _, ok := eligibleForegroundWindowWin()
		if !ok {
			return nil
		}
		if zoomed, _, _ := procIsZoomed.Call(hwnd); zoomed != 0 {
			_, _, _ = procShowWindow.Call(hwnd, swRestore)
		} else {
			_, _, _ = procShowWindow.Call(hwnd, swMaximize)
		}
		return nil

	case catalog.SysWinCenter:
		hwnd, work, ok := eligibleForegroundWindowWin()
		if !ok {
			return nil
		}
		workW := work.Right - work.Left
		workH := work.Bottom - work.Top
		w := workW * 74 / 100
		h := workH * 78 / 100
		placeWindowCompensatedWin(hwnd, work.Left+(workW-w)/2, work.Top+(workH-h)/2, w, h)
		return nil

	case catalog.SysWinMinimize:
		hwnd, _, ok := eligibleForegroundWindowWin()
		if !ok {
			return nil
		}
		_, _, _ = procShowWindow.Call(hwnd, swMinimize)
		return nil

	case catalog.SysShowDesktop:
		if cls, err := syscall.UTF16PtrFromString("Shell_TrayWnd"); err == nil {
			if tray, _, _ := procFindWindowW.Call(uintptr(unsafe.Pointer(cls)), 0); tray != 0 {
				_, _, _ = procPostMessageW.Call(tray, wmCommand, toggleDesktopCmd, 0)
			}
		}
		return nil

	case catalog.SysLockScreen:
		_, _, _ = procLockWorkStation.Call()
		return nil

	case catalog.SysScreenshot:
		return shellExecuteNoUI("open", "ms-screenclip:", "")

	case catalog.SysMediaPlayPause:
		tapVirtualKeyWin(vkMediaPlayPause)
		return nil

	case catalog.SysVolumeMute:
		tapVirtualKeyWin(vkVolumeMute)
		return nil

	case catalog.SysTaskManager:
		return shellExecuteNoUI("open", "taskmgr.exe", "")

	case catalog.SysEmptyTrash:
		_, _, _ = procSHEmptyRecycleBinW.Call(0, 0, 0)
		return nil

	default:
		return fmt.Errorf("sys: unknown system action %q", act)
	}
}
