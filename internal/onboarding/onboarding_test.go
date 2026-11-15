package onboarding_test

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/shader"

	"hellauncher/internal/catalog"
	"hellauncher/internal/engine"
	"hellauncher/internal/onboarding"
	"hellauncher/internal/sfx"
)

func TestHDMontageHeroLogoAndImprintShader(t *testing.T) {
	n := onboarding.MontageVideoBytesLenForTest()
	if n < 2_000_000 {
		t.Fatalf("expected embedded HD Helldivers 2 MP4 montage (>= 2MB), got %d bytes", n)
	}
	res := onboarding.MontagePosterResolutionForTest()
	if res.Dx() < 2560 || res.Dy() < 1200 {
		t.Fatalf("expected >= 2560x1200 ultra-crisp poster/video resolution, got %dx%d", res.Dx(), res.Dy())
	}
	logo := onboarding.HeroLogoImageForTest()
	if logo == nil || logo.Bounds().Dx() < 1200 || logo.Bounds().Dy() < 360 {
		t.Fatalf("expected high-res authentic Hero Logo image, got %v", logo)
	}
	sh := onboarding.CompileImprintShaderForTest()
	if sh == nil {
		t.Fatal("expected imprintShader to compile cleanly")
	}

	cat, err := catalog.Open(t.TempDir())
	if err != nil {
		t.Fatalf("catalog.Open: %v", err)
	}
	mgr := onboarding.New(cat, sfx.NewRecordSpeaker(), onboarding.Hooks{})
	defer mgr.Close()

	tt := ui.NewTester(func(c *ui.Context) {
		_ = shader.Options{}
		mgr.View(c)
	}, 160, 90)
	if img := tt.Image(); img == nil || img.Bounds().Empty() {
		t.Fatal("expected non-empty rendered montage image")
	}
	for _, unexpected := range []string{"全频段强制插播", "还在像平民一样"} {
		if tt.HasText(unexpected) {
			t.Fatalf("broadcast video must be free of text clutter, found %q", unexpected)
		}
	}

	tt.Advance(4000 * time.Millisecond)
	tt.Advance(4000 * time.Millisecond)
	tt.Advance(4000 * time.Millisecond)
	if tt.HasText("按任意键入列") {
		t.Fatalf("expected montage still playing before Act IV ridge climax at t=12.0s, texts=%v", tt.Texts())
	}
	tt.Advance(4000 * time.Millisecond)
	if !tt.HasText("按任意键入列") {
		t.Fatalf("expected automatic dissolve into Hero Title CTA at t=16.0s, texts=%v", tt.Texts())
	}
}

func TestSingleCanvasOnboardingFlowAndLiveHUDReactivity(t *testing.T) {
	cat, err := catalog.Open(t.TempDir())
	if err != nil {
		t.Fatalf("catalog.Open: %v", err)
	}
	if cat.Snapshot().Settings.Onboarded {
		t.Fatal("fresh catalog should have Onboarded=false")
	}

	spk := sfx.NewRecordSpeaker()
	var (
		liveHUDActive  bool
		completedCalls int
	)

	mgr := onboarding.New(cat, spk, onboarding.Hooks{
		SetLiveDesktopHUD: func(active bool) {
			liveHUDActive = active
		},
		OnClose: func(completed bool) {
			if completed {
				completedCalls++
			}
		},
	})
	defer mgr.Close()

	mgr.StartBroadcast()
	if !slices.Contains(spk.Cues(), sfx.CueOnboardingIntro) {
		t.Fatalf("expected CueOnboardingIntro on StartBroadcast, got %v", spk.Cues())
	}

	tt := ui.NewTester(mgr.View, 1280, 800)

	// Stage 0A: Clean HD Broadcast controls + live EN/ZH toggle
	if !tt.HasText("跳过播片") || !tt.HasText("跳过") {
		t.Fatalf("expected minimal top-right controls on Stage 0A, texts=%v", tt.Texts())
	}
	if err := tt.Click("EN"); err != nil {
		t.Fatalf("Click EN: %v", err)
	}
	if !tt.HasText("SKIP INTRO") {
		t.Fatalf("expected English Stage 0A controls, texts=%v", tt.Texts())
	}
	if err := tt.Click("中文"); err != nil {
		t.Fatalf("Click 中文: %v", err)
	}

	// Stage 0B: Dissolve to authentic Hero Logo + angular CTA
	stopBefore := len(spk.StopFades())
	if err := tt.Click("跳过播片"); err != nil {
		t.Fatalf("Click 跳过播片: %v", err)
	}
	if len(spk.StopFades()) <= stopBefore {
		t.Fatal("expected StopMusic fade-out when revealing Hero Title")
	}
	if !tt.HasText("按任意键入列") {
		t.Fatalf("expected Hero Title CTA 按任意键入列, texts=%v", tt.Texts())
	}

	// Stage 1 (01 / 04): HUD Theme — single focus + live top-left HUD rendered in-canvas
	if err := tt.Click("按任意键入列"); err != nil {
		t.Fatalf("Click 按任意键入列: %v", err)
	}
	if !liveHUDActive {
		t.Fatal("expected SetLiveDesktopHUD(true) on Stage 1")
	}
	for _, want := range []string{"01 / 04", "面板主题", "原版", "战术", "呼叫文件补给箱"} {
		if !tt.HasText(want) {
			t.Fatalf("expected %q on Stage 1 (Theme + Live HUD), texts=%v", want, tt.Texts())
		}
	}
	if tt.HasText("上一步") {
		t.Fatalf("expected no Back button on onboarding step 01 / 04, texts=%v", tt.Texts())
	}
	if err := tt.Click("战术"); err != nil {
		t.Fatalf("Click 战术: %v", err)
	}
	if cat.Snapshot().Settings.HUDTheme != catalog.ThemeTactical || !tt.HasText("战略配备") {
		t.Fatalf("expected live Tactical HUD header on Stage 1, theme=%q, texts=%v", cat.Snapshot().Settings.HUDTheme, tt.Texts())
	}
	if err := tt.Click("原版"); err != nil {
		t.Fatalf("Click 原版: %v", err)
	}
	if cat.Snapshot().Settings.HUDTheme != catalog.ThemeClassic {
		t.Fatalf("expected live Classic HUD restored, got %q", cat.Snapshot().Settings.HUDTheme)
	}

	// Stage 2 (02 / 04): Summon Key — single focus
	if err := tt.Click("下一步"); err != nil {
		t.Fatalf("Click 下一步 to Stage 2: %v", err)
	}
	for _, want := range []string{"02 / 04", "上一步", "唤出热键", "双击按住 Ctrl", "按住左 Ctrl", "按住右 Ctrl"} {
		if !tt.HasText(want) {
			t.Fatalf("expected %q on Stage 2, texts=%v", want, tt.Texts())
		}
	}
	if err := tt.Click("按住左 Ctrl"); err != nil {
		t.Fatalf("Click 按住左 Ctrl: %v", err)
	}
	if cat.Snapshot().Settings.TriggerMode != catalog.ModeHoldLeftCtrl {
		t.Fatalf("expected live TriggerMode=left_ctrl, got %q", cat.Snapshot().Settings.TriggerMode)
	}

	// Stage 3 (03 / 04): Loadout — 1-click Stratagem Pod Grid on the SAME canvas (no Arsenal popup!)
	if err := tt.Click("下一步"); err != nil {
		t.Fatalf("Click 下一步 to Stage 3: %v", err)
	}
	if got := len(cat.Snapshot().Stratagems); got != 3 {
		t.Fatalf("expected untouched 3 default stratagems before toggling pods, got %d", got)
	}
	for _, pod := range []string{"窗口左 / 右分屏", "默认浏览器", "区域截图"} {
		if err := tt.Click(pod); err != nil {
			t.Fatalf("Click pod %q: %v", pod, err)
		}
	}
	if got := len(cat.Snapshot().Stratagems); got != 7 {
		t.Fatalf("expected 7 stratagems after equipping split(2) + browser + screenshot, got %d", got)
	}
	if !tt.HasText("快速侦察车：默认浏览器") || !tt.HasText("“飞鹰”扫射：窗口左半屏") {
		t.Fatalf("expected newly equipped pods to appear immediately in live top-left HUD, texts=%v", tt.Texts())
	}

	// Stage 4 (04 / 04): Call In Resupply — pure visual equation [L-CTRL] + [↓][↓][↑][→]
	if err := tt.Click("下一步"); err != nil {
		t.Fatalf("Click 下一步 to Stage 4: %v", err)
	}
	for _, want := range []string{"04 / 04", "呼叫文件补给箱", "L-CTRL"} {
		if !tt.HasText(want) {
			t.Fatalf("expected %q on Stage 4 Drill, texts=%v", want, tt.Texts())
		}
	}

	// Feed first 2 arrows via live global hotkey engine, then finish with W, D
	mgr.ObserveEngineSnapshot(engine.Snapshot{
		Phase: engine.PhaseOpen,
		Typed: []catalog.Dir{catalog.DirDown, catalog.DirDown},
	})
	tt.Frame()

	spk.Reset()
	tt.Key(0, ui.KeyW)
	tt.Key(0, ui.KeyD)
	if !slices.Contains(spk.Cues(), sfx.CueMissionComplete) {
		t.Fatalf("expected CueMissionComplete upon finishing drill, got %v", spk.Cues())
	}
	if !tt.HasText("✓ 准备就绪") || !tt.HasText("✓ 已就绪") {
		t.Fatalf("expected both terminal ready badge and live HUD deployed state, texts=%v", tt.Texts())
	}

	if err := tt.Click("开始执勤"); err != nil {
		t.Fatalf("Click 开始执勤: %v", err)
	}
	if completedCalls != 1 {
		t.Fatalf("expected OnClose(true) called once, got %d", completedCalls)
	}
	if liveHUDActive {
		t.Fatal("expected SetLiveDesktopHUD(false) after finishing onboarding")
	}

	finalSnap := cat.Snapshot()
	if !finalSnap.Settings.Onboarded {
		t.Fatal("expected Settings.Onboarded=true")
	}
	if !strings.HasPrefix(finalSnap.Settings.ShipName, "SES ") {
		t.Fatalf("expected saved SES ship name, got %q", finalSnap.Settings.ShipName)
	}
}

func TestOnboardingSkipAllMarksOnboardedCleanly(t *testing.T) {
	cat, err := catalog.Open(t.TempDir())
	if err != nil {
		t.Fatalf("catalog.Open: %v", err)
	}
	mgr := onboarding.New(cat, sfx.NewRecordSpeaker(), onboarding.Hooks{})
	defer mgr.Close()

	tt := ui.NewTester(mgr.View, 800, 500)

	if err := tt.Click("跳过"); err != nil {
		t.Fatalf("Click 跳过: %v", err)
	}
	if !cat.Snapshot().Settings.Onboarded {
		t.Fatal("expected Onboarded=true after clicking 跳过")
	}
}
