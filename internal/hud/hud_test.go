package hud_test

import (
	"runtime"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"hellauncher/internal/catalog"
	"hellauncher/internal/engine"
	"hellauncher/internal/hud"
)

func TestHUDViewRendersAllPhasesCleanly(t *testing.T) {
	snap := engine.Snapshot{
		Phase:   engine.PhaseOpen,
		Trusted: true,
		Loadout: catalog.DefaultSnapshot(),
	}

	tt := ui.NewTester(func(c *ui.Context) {
		hud.View(c, snap)
	}, 460, 580)
	// Simulate inactive ShowInactive() panel!
	tt.SetWindowActive(false)
	tt.Frame()

	if !tt.HasText("战略配备") || !tt.HasText("STRATAGEMS") || !tt.HasText("指令序列:") {
		t.Fatalf("expected tactical visor header & input footer in PhaseOpen, texts=%v", tt.Texts())
	}
	if !tt.HasText("Google Chrome") || !tt.HasText("Zed Editor") {
		t.Fatalf("expected default stratagems in HUD, texts=%v", tt.Texts())
	}

	// Verify flat mode toggle works cleanly without stale state
	snap.Loadout.Settings.VisorCurved = false
	tt.Frame()
	snap.Loadout.Settings.VisorCurved = true
	tt.Frame()

	// Prefix filtering & step count
	snap.Typed = []catalog.Dir{catalog.DirUp, catalog.DirDown}
	snap.StepSeq = 2
	tt.Frame()
	if !tt.HasText("输入中 2") {
		t.Fatalf("expected typed count badge, texts=%v", tt.Texts())
	}

	// Error flash animation settles cleanly across frames
	snap.Typed = nil
	snap.ErrorSeq = 1
	tt.Frame()
	if !tt.HasText("指令错误") {
		t.Fatalf("expected error badge on ErrorSeq increment, texts=%v", tt.Texts())
	}
	tt.Advance(300 * time.Millisecond)

	// PhaseArmed beacon targeting banner
	snap.Phase = engine.PhaseArmed
	snap.MatchedID = "open-zed"
	tt.Frame()
	if !tt.HasText("信标已握持") {
		t.Fatalf("expected armed badge in PhaseArmed, texts=%v", tt.Texts())
	}

	// PhaseDeploying & PhaseDeployed
	snap.Phase = engine.PhaseDeploying
	tt.Frame()
	tt.Advance(120 * time.Millisecond)
	if !tt.HasText("空投部署中") {
		t.Fatalf("expected deploying badge, texts=%v", tt.Texts())
	}

	snap.Phase = engine.PhaseDeployed
	tt.Frame()
	if !tt.HasText("✓ 已就绪") {
		t.Fatalf("expected deployed badge, texts=%v", tt.Texts())
	}

	// Untrusted banner on macOS
	if runtime.GOOS == "darwin" {
		snap.Phase = engine.PhaseOpen
		snap.Trusted = false
		tt.Frame()
		if !tt.HasText("未获辅助功能权限") {
			t.Fatalf("expected untrusted accessibility banner on darwin, texts=%v", tt.Texts())
		}
	}

	// Settling into PhaseIdle stops rendering card text
	snap.Phase = engine.PhaseIdle
	tt.Frame()
	tt.Advance(250 * time.Millisecond)
	if tt.HasText("Google Chrome") {
		t.Fatalf("expected HUD to hide contents after settling into PhaseIdle, texts=%v", tt.Texts())
	}
}
