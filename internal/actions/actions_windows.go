//go:build windows

package actions

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
	"time"
	"unsafe"
)

const wmClose = 0x0010

var (
	winUser32                 = syscall.NewLazyDLL("user32.dll")
	procGetForegroundWindow   = winUser32.NewProc("GetForegroundWindow")
	procGetDesktopWindow      = winUser32.NewProc("GetDesktopWindow")
	procGetShellWindow        = winUser32.NewProc("GetShellWindow")
	procGetWindowThreadProcId = winUser32.NewProc("GetWindowThreadProcessId")
	procGetClassNameW         = winUser32.NewProc("GetClassNameW")
	procIsWindowVisible       = winUser32.NewProc("IsWindowVisible")
	procPostMessageW          = winUser32.NewProc("PostMessageW")
)

func startHiddenWin(bin string, args ...string) error {
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

// launchAndWaitWin launches an executable or `shell:AppsFolder\...` target completely
// off the main UI thread and polls until the new foreground window becomes visible.
func launchAndWaitWin(launchFn func() error, maxWait time.Duration) error {
	beforeHwnd := foregroundHwnd()
	if err := launchFn(); err != nil {
		return err
	}

	deadline := time.Now().Add(maxWait)
	for time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
		cur := foregroundHwnd()
		if cur != 0 && cur != beforeHwnd && isAppWindowVisible(cur) {
			return nil
		}
	}
	return nil
}

func (e *Executor) openTargetPlatform(target string) error {
	switch target {
	case "files":
		return launchAndWaitWin(func() error {
			return startHiddenWin("explorer.exe")
		}, 3500*time.Millisecond)

	case "chrome":
		if app, ok := e.findInstalledApp(
			[]string{"Chrome"},
			[]string{"Google Chrome", "Microsoft Edge"},
		); ok && app.Path != "" {
			return launchAndWaitWin(func() error {
				return startHiddenWin("explorer.exe", app.Path)
			}, 5000*time.Millisecond)
		}
		if err := launchAndWaitWin(func() error {
			return startHiddenWin("cmd", "/c", "start", "", "chrome")
		}, 5000*time.Millisecond); err == nil {
			return nil
		}
		return startHiddenWin("explorer.exe", "https://www.google.com")

	case "zed":
		if app, ok := e.findInstalledApp(
			[]string{"Zed.Zed"},
			[]string{"Zed", "Cursor", "Visual Studio Code"},
		); ok && app.Path != "" {
			return launchAndWaitWin(func() error {
				return startHiddenWin("explorer.exe", app.Path)
			}, 5500*time.Millisecond)
		}
		return launchAndWaitWin(func() error {
			return startHiddenWin("cmd", "/c", "start", "", "zed")
		}, 5500*time.Millisecond)

	case "terminal":
		if app, ok := e.findInstalledApp(
			[]string{"Microsoft.WindowsTerminal_8wekyb3d8bbwe!App"},
			[]string{"Windows Terminal", "PowerShell"},
		); ok && app.Path != "" {
			return launchAndWaitWin(func() error {
				return startHiddenWin("explorer.exe", app.Path)
			}, 4500*time.Millisecond)
		}
		return launchAndWaitWin(func() error {
			if err := startHiddenWin("wt.exe"); err == nil {
				return nil
			}
			return startHiddenWin("powershell.exe")
		}, 4500*time.Millisecond)

	default:
		return launchAndWaitWin(func() error {
			return startHiddenWin("cmd", "/c", "start", "", target)
		}, 4000*time.Millisecond)
	}
}

func closeActiveWindowPlatform() error {
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

	// Never send WM_CLOSE to desktop shell classes (avoids Windows Shutdown dialog!)
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
