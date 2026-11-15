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

func TestClassicHUDViewAndPreview(t *testing.T) {
	snap := engine.Snapshot{
		Phase:           engine.PhaseOpen,
		Trusted:         true,
		HelldiversReady: false,
		Loadout:         catalog.DefaultSnapshot(),
	}
	snap.Loadout.Settings.HUDTheme = catalog.ThemeClassic
	snap.Loadout.Settings.Language = catalog.LangZH

	tt := ui.NewTester(func(c *ui.Context) {
		hud.View(c, snap)
	}, 320, 320)
	tt.SetWindowActive(false)
	tt.Frame()

	// Classic theme is borderless & chrome-free: shows the 3 default Stratagems (2 shortcuts + 1 system).
	if !tt.HasText("轨道精准攻击：关闭窗口") || !tt.HasText("呼叫文件补给箱") || !tt.HasText("加入绝地潜兵") {
		t.Fatalf("expected default stratagems in Classic HUD, texts=%v", tt.Texts())
	}
	if !tt.HasText("未定位发射井") {
		t.Fatalf("expected offline silo hint when HelldiversReady=false, texts=%v", tt.Texts())
	}
	if tt.HasText("指令:") || tt.HasText("STRATAGEMS") {
		t.Fatalf("Classic HUD should not render tactical header/footer chrome, texts=%v", tt.Texts())
	}

	// When HelldiversReady=true, offline hint disappears and arrows render cleanly
	snap.HelldiversReady = true
	tt.Frame()
	if tt.HasText("未定位发射井") {
		t.Fatalf("did not expect offline hint when HelldiversReady=true, texts=%v", tt.Texts())
	}

	// Deploying status line in ZH & EN
	snap.Phase = engine.PhaseDeploying
	snap.MatchedID = catalog.SystemJoinHelldiversID
	tt.Frame()
	if !tt.HasText("空投舱发射中") {
		t.Fatalf("expected ZH hellpod launching status in Classic HUD, texts=%v", tt.Texts())
	}

	snap.Loadout.Settings.Language = catalog.LangEN
	tt.Frame()
	if !tt.HasText("HELLPOD LAUNCHING") {
		t.Fatalf("expected EN hellpod launching status in Classic HUD, texts=%v", tt.Texts())
	}

	// PhaseIdle plays exit animation first, then settles to empty after ~165ms
	snap.Phase = engine.PhaseIdle
	tt.Frame()
	tt.Advance(60 * time.Millisecond)
	if !tt.HasText("加入绝地潜兵") {
		t.Fatalf("expected Classic HUD to remain mounted mid-exit-animation at 60ms, texts=%v", tt.Texts())
	}
	tt.Advance(220 * time.Millisecond)
	if tt.HasText("加入绝地潜兵") {
		t.Fatalf("expected Classic HUD to settle empty after exit animation completes, texts=%v", tt.Texts())
	}

	// Re-opening from PhaseIdle animates smoothly back in
	snap.Phase = engine.PhaseOpen
	snap.Loadout.Settings.Language = catalog.LangZH
	snap.Typed = []catalog.Dir{catalog.DirDown, catalog.DirDown}
	snap.StepSeq = 2
	tt.Frame()
	tt.Advance(200 * time.Millisecond)
	if !tt.HasText("呼叫文件补给箱") {
		t.Fatalf("expected Classic HUD to animate back in on PhaseOpen, texts=%v", tt.Texts())
	}
	snap.Phase = engine.PhaseIdle

	// Preview() renders cleanly even when snapshot Phase is PhaseIdle (with glass.Blur over backdrop)
	pt := ui.NewTester(func(c *ui.Context) {
		ui.Box(c).Size(360, 320).Background(ui.Hex("#243447")).Children(func() {
			hud.Preview(c, snap)
		})
	}, 360, 320)
	pt.Frame()
	if !pt.HasText("加入绝地潜兵") {
		t.Fatalf("expected hud.Preview to show active stratagems over backdrop, texts=%v", pt.Texts())
	}
}

func TestTacticalHUDViewRendersAllPhasesAndI18n(t *testing.T) {
	snap := engine.Snapshot{
		Phase:           engine.PhaseOpen,
		Trusted:         true,
		HelldiversReady: false,
		Loadout:         catalog.DefaultSnapshot(),
	}
	snap.Loadout.Settings.HUDTheme = catalog.ThemeTactical
	snap.Loadout.Settings.Language = catalog.LangZH

	tt := ui.NewTester(func(c *ui.Context) {
		hud.View(c, snap)
	}, 460, 580)
	tt.SetWindowActive(false)
	tt.Frame()

	if !tt.HasText("战略配备") || !tt.HasText("指令:") {
		t.Fatalf("expected tactical visor header & input footer in PhaseOpen, texts=%v", tt.Texts())
	}
	if !tt.HasText("轨道精准攻击：关闭窗口") || !tt.HasText("呼叫文件补给箱") || !tt.HasText("加入绝地潜兵") {
		t.Fatalf("expected default stratagems in HUD, texts=%v", tt.Texts())
	}
	if !tt.HasText("待部署") {
		t.Fatalf("expected offline badge on Tactical HUD when HelldiversReady=false, texts=%v", tt.Texts())
	}

	// Verify flat mode toggle works cleanly without stale state
	snap.Loadout.Settings.VisorCurved = false
	tt.Frame()
	snap.Loadout.Settings.VisorCurved = true
	tt.Frame()

	// Prefix filtering & step count
	snap.Typed = []catalog.Dir{catalog.DirDown, catalog.DirDown}
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

	// PhaseArmed beacon targeting banner (ZH & EN)
	snap.Phase = engine.PhaseArmed
	snap.MatchedID = "resupply-files"
	tt.Frame()
	if !tt.HasText("信标已握持") {
		t.Fatalf("expected armed badge in PhaseArmed, texts=%v", tt.Texts())
	}
	snap.Loadout.Settings.Language = catalog.LangEN
	tt.Frame()
	if !tt.HasText("BEACON ARMED") || !tt.HasText("STRATAGEMS") {
		t.Fatalf("expected EN armed badge & header, texts=%v", tt.Texts())
	}

	// PhaseDeploying & PhaseDeployed
	snap.Loadout.Settings.Language = catalog.LangZH
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

	if runtime.GOOS == "darwin" {
		snap.Phase = engine.PhaseOpen
		snap.Trusted = false
		tt.Frame()
		if !tt.HasText("需要辅助功能权限") {
			t.Fatalf("expected untrusted accessibility banner on darwin, texts=%v", tt.Texts())
		}
	}

	// Settling into PhaseIdle stops rendering card text
	snap.Phase = engine.PhaseIdle
	tt.Frame()
	tt.Advance(250 * time.Millisecond)
	if tt.HasText("加入绝地潜兵") {
		t.Fatalf("expected HUD to hide contents after settling into PhaseIdle, texts=%v", tt.Texts())
	}
}
