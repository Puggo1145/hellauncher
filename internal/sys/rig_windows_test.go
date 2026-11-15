//go:build windows

package sys

import (
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestClassifyWinVKAndModifiers(t *testing.T) {
	cases := []struct {
		vk   uint16
		want KeyRole
	}{
		{vkW, KeyDirUp},
		{vkUp, KeyDirUp},
		{vkS, KeyDirDown},
		{vkDown, KeyDirDown},
		{vkA, KeyDirLeft},
		{vkLeft, KeyDirLeft},
		{vkD, KeyDirRight},
		{vkRight, KeyDirRight},
		{vkEscape, KeyEscape},
		{0x42, KeyOther},
	}
	for _, tc := range cases {
		if got := classifyWinVK(tc.vk); got != tc.want {
			t.Fatalf("classifyWinVK(0x%X) = %v, want %v", tc.vk, got, tc.want)
		}
	}

	for _, modVK := range []uint16{vkShift, vkControl, vkMenu, vkLShift, vkRShift, vkLControl, vkRControl, vkLMenu, vkRMenu, vkLWin, vkRWin} {
		if !isWinModifierVK(modVK) {
			t.Fatalf("expected 0x%X to be recognized as modifier VK", modVK)
		}
	}
	if isWinModifierVK(vkW) {
		t.Fatal("vkW must not be classified as modifier VK")
	}
}

func TestModifierReconciliationClearsStuckKeys(t *testing.T) {
	// Simulate Win+L leaving vkLWin stuck in held map after session unlock.
	held := map[uint16]bool{
		vkLWin:     true,
		vkLControl: true,
	}
	physicalDown := map[uint16]bool{
		vkLWin:     false, // Released during lock screen
		vkLControl: true,  // Freshly pressed by user
	}

	reconcileOtherHeldVKLocked(held, vkLControl, func(k uint16) bool {
		return physicalDown[k]
	})
	if held[vkLWin] {
		t.Fatal("expected stuck vkLWin to be cleared on next key event")
	}
	mods := modsFromHeldLocked(held)
	if !mods.LeftCtrl || mods.Super {
		t.Fatalf("unexpected mods after reconciliation: %+v", mods)
	}

	// Simulate releasing LeftCtrl inside an elevated UIPI window (missed WM_KEYUP).
	physicalDown[vkLControl] = false
	if !reconcileHeldVKLocked(held, func(k uint16) bool { return physicalDown[k] }) {
		t.Fatal("expected reconcileHeldVKLocked to report change when stuck LeftCtrl cleared")
	}
	if modsFromHeldLocked(held) != (Mods{}) {
		t.Fatalf("expected all modifiers cleared, got %+v", modsFromHeldLocked(held))
	}
}

func TestExpandWinTarget(t *testing.T) {
	t.Setenv("HELLAUNCHER_TEST_DIR", `D:\tactical`)

	gotPct := expandWinTarget(`%HELLAUNCHER_TEST_DIR%\drop`)
	if gotPct != `D:\tactical\drop` {
		t.Fatalf("expandWinTarget percent env = %q, want %q", gotPct, `D:\tactical\drop`)
	}

	gotHome := expandWinTarget("~/Downloads")
	if strings.HasPrefix(gotHome, "~") || !strings.HasSuffix(gotHome, filepath.Join("Downloads")) {
		t.Fatalf("expandWinTarget tilde was not expanded: %q", gotHome)
	}
}

func TestClampWindowToWorkAreaMultiMonitor(t *testing.T) {
	// Secondary monitor positioned to the left of primary monitor (negative X coordinates!)
	leftMonitorWork := winRect{
		Left:   -1920,
		Top:    -120,
		Right:  0,
		Bottom: 920,
	}

	// Beacon near center of secondary monitor -> centered cleanly with negative X
	x, y := clampWindowToWorkArea(Point{X: -960, Y: 400}, 800, 600, leftMonitorWork)
	if x != -1360 || y != 100 {
		t.Fatalf("centered negative-monitor coords = (%d, %d), want (-1360, 100)", x, y)
	}

	// Beacon right at far-left edge -> clamped to Left=-1920, Top=-120
	x, y = clampWindowToWorkArea(Point{X: -1910, Y: -115}, 800, 600, leftMonitorWork)
	if x != -1920 || y != -120 {
		t.Fatalf("clamped top-left coords = (%d, %d), want (-1920, -120)", x, y)
	}

	// Beacon near bottom-right corner of secondary monitor -> clamped inside Right=0, Bottom=920
	x, y = clampWindowToWorkArea(Point{X: -20, Y: 910}, 800, 600, leftMonitorWork)
	if x != -800 || y != 320 {
		t.Fatalf("clamped bottom-right coords = (%d, %d), want (-800, 320)", x, y)
	}

	// Window larger than work area -> pinned to work area top-left
	x, y = clampWindowToWorkArea(Point{X: -500, Y: 300}, 2400, 1400, leftMonitorWork)
	if x != -1920 || y != -120 {
		t.Fatalf("oversized window coords = (%d, %d), want (-1920, -120)", x, y)
	}
}

func TestWindowsRigHookLifecycleAndNoUIShellError(t *testing.T) {
	rig := NewPlatformRig()
	trustedCh := make(chan bool, 1)
	stop := rig.StartInput(
		func(InputEvent) bool { return false },
		func(ok bool) {
			select {
			case trustedCh <- ok:
			default:
			}
		},
	)

	select {
	case ok := <-trustedCh:
		if !ok {
			t.Fatal("expected WindowsRig onTrust(true)")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for onTrust callback")
	}

	rig.SetMouseCapture(true)
	time.Sleep(30 * time.Millisecond)
	rig.SetMouseCapture(false)
	stop()

	// Verify ShellExecuteExW with SEE_MASK_FLAG_NO_UI fails fast without modal dialog.
	if err := shellExecuteNoUI("open", `__nonexistent_hellauncher_binary_12345__.exe`, ""); err == nil {
		t.Fatal("expected error for nonexistent executable")
	}
}
