package arsenal_test

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"hellauncher/internal/arsenal"
	"hellauncher/internal/catalog"
	"hellauncher/internal/sys"
)

func TestArsenalTopNavigationAppearanceSettingsAndEnlistmentTerminal(t *testing.T) {
	cat, err := catalog.Open(t.TempDir())
	if err != nil {
		t.Fatalf("catalog.Open: %v", err)
	}

	fakeExe := filepath.Join(t.TempDir(), "helldivers2.exe")
	if err := os.WriteFile(fakeExe, []byte("MZ"), 0o644); err != nil {
		t.Fatalf("WriteFile fakeExe: %v", err)
	}

	var (
		previewCalls    int
		openLogsCalls   int
		enlistURLCalls  int
		diveCalls       int
		steamAutoExists bool
	)

	mgr := arsenal.New(cat, func() []arsenal.AppOption {
		return []arsenal.AppOption{
			{ID: "com.mitchellh.ghostty", Name: "Ghostty", Path: "/Applications/Ghostty.app"},
		}
	})
	mgr.SetHooks(arsenal.Hooks{
		PreviewHUD: func() { previewCalls++ },
		OpenLogs:   func() { openLogsCalls++ },
		IsTrusted:  func() bool { return true },
		DetectHelldivers: func(customPath string) sys.HelldiversInstall {
			if strings.TrimSpace(customPath) != "" {
				return sys.DetectHelldivers(customPath)
			}
			if steamAutoExists {
				return sys.HelldiversInstall{
					Found:     true,
					Source:    "steam",
					Path:      fakeExe,
					LaunchURI: sys.HelldiversSteamRunURI,
				}
			}
			return sys.HelldiversInstall{Found: false, Source: "none"}
		},
		BrowseHelldivers: func(_ *mygo.Window, _ string, onPicked func(string)) {
			onPicked(fakeExe)
		},
		LaunchHelldivers: func() { diveCalls++ },
		OpenEnlistURL:    func() { enlistURLCalls++ },
	})
	defer mgr.Close()

	tt := ui.NewTester(mgr.View, 1090, 650)
	tt.Frame()

	// 1. Verify Top Navigation Bar and the 3 clean initial Stratagems (2 shortcuts + 1 system)
	for _, want := range []string{
		"战略配备军械库", "战备配置", "面板外观", "通用设置",
		"轨道精准攻击：关闭窗口", "呼叫文件补给箱", "加入绝地潜兵", "系统", "实时预览",
	} {
		if !tt.HasText(want) {
			t.Fatalf("expected %q on initial Stratagems tab, texts=%v", want, tt.Texts())
		}
	}
	// Removed custom/default items and scattered settings must NOT appear
	for _, unexpected := range []string{
		"Google Chrome", "Zed Editor", "侦察浏览器", "战术代码终端", "命令行终端",
		"唤出快捷键", "指令按键音效", "高斯毛玻璃",
	} {
		if tt.HasText(unexpected) {
			t.Fatalf("unexpected text %q on Stratagems tab", unexpected)
		}
	}

	// Ensure raw ".svg" filenames never leak into Arsenal UI text
	for _, txt := range tt.Texts() {
		if strings.Contains(txt, ".svg") {
			t.Fatalf("found raw filename %q in Arsenal UI text", txt)
		}
	}

	// Open official Stratagem preset picker (all 111 canon items, zero non-canon icons)
	if err := tt.Click("轨道精准攻击 ▾"); err != nil {
		t.Fatalf("Click icon chip: %v", err)
	}
	if !tt.HasText("“飞鹰”500KG炸弹") || !tt.HasText("进攻") || !tt.HasText("支援") || !tt.HasText("防御") || !tt.HasText("任务") {
		t.Fatalf("expected official Helldivers 2 Stratagem presets & category filters, texts=%v", tt.Texts())
	}
	for _, txt := range tt.Texts() {
		if strings.Contains(txt, ".svg") {
			t.Fatalf("found raw filename %q inside icon picker", txt)
		}
	}
	// Switch category filter to 防御 (Defensive) -> shows sentry & emplacement presets
	if err := tt.Click("防御"); err != nil {
		t.Fatalf("Click 防御 category filter: %v", err)
	}
	if !tt.HasText("A/G-16 加特林哨戒炮") {
		t.Fatalf("expected defensive sentry in 防御 filter, texts=%v", tt.Texts())
	}
	if err := tt.Click("全部"); err != nil {
		t.Fatalf("Click 全部 category filter: %v", err)
	}
	if err := tt.Click("轨道精准攻击"); err != nil {
		t.Fatalf("Click 轨道精准攻击: %v", err)
	}

	// 2. Select System Stratagem "加入绝地潜兵": verify immutable Enlistment Terminal UX!
	if err := tt.Click("加入绝地潜兵"); err != nil {
		t.Fatalf("Click 加入绝地潜兵: %v", err)
	}
	if tt.HasText("删除") || tt.HasText("保存") || tt.HasText("录制按键") {
		t.Fatalf("system stratagem must not expose mutable editor controls, texts=%v", tt.Texts())
	}
	for _, want := range []string{"真理部钦定", "不可更改", "空投舱发射井坐标", "本机未探测到《绝地潜兵 2》空投发射井"} {
		if !tt.HasText(want) {
			t.Fatalf("expected %q in missing-game Enlistment Terminal, texts=%v", want, tt.Texts())
		}
	}

	// Click Steam Enlist button when game is not installed
	if err := tt.Click("✪ 前往超级地球征兵处 (Steam)"); err != nil {
		t.Fatalf("Click Steam store button: %v", err)
	}
	if enlistURLCalls != 1 {
		t.Fatalf("expected OpenEnlistURL called once, got %d", enlistURLCalls)
	}

	// Browse & pick custom helldivers2.exe -> transitions to active custom coordinates + Dive Now!
	if err := tt.Click("手动选择启动程序…"); err != nil {
		t.Fatalf("Click 手动选择启动程序…: %v", err)
	}
	if cat.Snapshot().Settings.HelldiversPath != fakeExe {
		t.Fatalf("expected Settings.HelldiversPath=%q, got %q", fakeExe, cat.Snapshot().Settings.HelldiversPath)
	}
	if !tt.HasText("● 已挂载自定义空投坐标") {
		t.Fatalf("expected custom coordinates active status, texts=%v", tt.Texts())
	}
	if err := tt.Click("立即潜入游戏"); err != nil {
		t.Fatalf("Click ⚡ 立即潜入游戏: %v", err)
	}
	if diveCalls != 1 {
		t.Fatalf("expected LaunchHelldivers called once, got %d", diveCalls)
	}

	// Restore Auto-Detect when Steam library now has the game -> transitions to Steam Auto-Detected!
	steamAutoExists = true
	if err := tt.Click("恢复自动探测"); err != nil {
		t.Fatalf("Click 恢复自动探测: %v", err)
	}
	if cat.Snapshot().Settings.HelldiversPath != "" {
		t.Fatalf("expected empty HelldiversPath after auto-detect, got %q", cat.Snapshot().Settings.HelldiversPath)
	}
	if !tt.HasText("Steam 自动探测") {
		t.Fatalf("expected Steam auto-detected status, texts=%v", tt.Texts())
	}

	// 3. Navigate to Tab 1: 面板外观 (Appearance)
	// Requirement: Classic theme MUST NOT show "3D 头盔弧面投射"; Tactical theme MUST show it!
	if err := tt.Click("面板外观"); err != nil {
		t.Fatalf("Click 面板外观: %v", err)
	}
	if !tt.HasText("面板主题") || !tt.HasText("高斯毛玻璃") || !tt.HasText("头盔战术目镜") {
		t.Fatalf("expected formal theme cards on Appearance tab, texts=%v", tt.Texts())
	}
	if tt.HasText("3D 头盔弧面投射") {
		t.Fatalf("Classic theme must NOT display 3D 头盔弧面投射 option, texts=%v", tt.Texts())
	}

	if err := tt.Click("战术"); err != nil {
		t.Fatalf("Click 战术 theme card: %v", err)
	}
	if cat.Snapshot().Settings.HUDTheme != catalog.ThemeTactical {
		t.Fatalf("expected ThemeTactical, got %q", cat.Snapshot().Settings.HUDTheme)
	}
	if !tt.HasText("3D 头盔弧面投射") {
		t.Fatalf("Tactical theme MUST display 3D 头盔弧面投射 option, texts=%v", tt.Texts())
	}
	if previewCalls == 0 {
		t.Fatal("expected PreviewHUD hook to fire when switching theme card")
	}

	if err := tt.Click("原版"); err != nil {
		t.Fatalf("Click 原版 theme card: %v", err)
	}
	if cat.Snapshot().Settings.HUDTheme != catalog.ThemeClassic {
		t.Fatalf("expected ThemeClassic, got %q", cat.Snapshot().Settings.HUDTheme)
	}
	if tt.HasText("3D 头盔弧面投射") {
		t.Fatalf("switching back to Classic theme must hide 3D 头盔弧面投射 again, texts=%v", tt.Texts())
	}

	// 4. Navigate to Tab 2: 通用设置 (Settings)
	if err := tt.Click("通用设置"); err != nil {
		t.Fatalf("Click 通用设置: %v", err)
	}
	for _, want := range []string{"操控与反馈", "唤出快捷键", "指令按键音效", "通用与系统", "界面语言", "运行日志", "默认战备数据"} {
		if !tt.HasText(want) {
			t.Fatalf("expected %q on Settings tab, texts=%v", want, tt.Texts())
		}
	}

	if err := tt.Click("左 Ctrl"); err != nil {
		t.Fatalf("Click 左 Ctrl: %v", err)
	}
	if cat.Snapshot().Settings.TriggerMode != catalog.ModeHoldLeftCtrl {
		t.Fatalf("expected ModeHoldLeftCtrl, got %q", cat.Snapshot().Settings.TriggerMode)
	}

	if err := tt.Click("指令按键音效"); err != nil {
		t.Fatalf("Click 指令按键音效 switch: %v", err)
	}
	if !cat.Snapshot().Settings.Muted {
		t.Fatal("expected Catalog Settings.Muted=true after toggling 指令按键音效")
	}

	if err := tt.Click("打开目录"); err != nil {
		t.Fatalf("Click 打开目录: %v", err)
	}
	if openLogsCalls != 1 {
		t.Fatalf("expected OpenLogs hook called once, got %d", openLogsCalls)
	}

	// Switch Language to English -> localizes default Stratagems too ("Join the Helldivers")
	if err := tt.Click("EN"); err != nil {
		t.Fatalf("Click EN: %v", err)
	}
	if cat.Snapshot().Settings.Language != catalog.LangEN {
		t.Fatalf("expected LangEN, got %q", cat.Snapshot().Settings.Language)
	}
	for _, want := range []string{"Stratagem Arsenal", "Stratagems", "Appearance", "Settings", "Controls & Audio"} {
		if !tt.HasText(want) {
			t.Fatalf("expected English text %q after switching to EN, texts=%v", want, tt.Texts())
		}
	}
	if err := tt.Click("中文"); err != nil {
		t.Fatalf("Click 中文: %v", err)
	}

	// 5. Return to Tab 0: 战备配置 (Stratagems) and create a new Stratagem using an official HD2 preset!
	if err := tt.Click("战备配置"); err != nil {
		t.Fatalf("Click 战备配置: %v", err)
	}

	if err := tt.Click("+ 新建"); err != nil {
		t.Fatalf("Click + 新建: %v", err)
	}
	if err := tt.Click("Ghostty"); err != nil {
		t.Fatalf("Click Ghostty app chip: %v", err)
	}
	if err := tt.Click("⊕ 鼠标落点"); err != nil {
		t.Fatalf("Click ⊕ 鼠标落点: %v", err)
	}

	// Record conflicting sequence ↓↓↑→ (same as 呼叫文件补给箱) via keyboard recorder
	if err := tt.Click("录制按键"); err != nil {
		t.Fatalf("Click 录制按键: %v", err)
	}
	tt.Key(0, ui.KeyS)
	tt.Key(0, ui.KeyS)
	tt.Key(0, ui.KeyW)
	tt.Key(0, ui.KeyD)
	tt.Key(0, ui.KeyEnter)

	if !tt.HasText("指令序列冲突") {
		t.Fatalf("expected live ConflictError feedback on chord overlap, texts=%v", tt.Texts())
	}

	// Clear sequence and click "套用原版指令" (Eagle 500kg Bomb canonical ↑→↓↓↓)
	if err := tt.Click("清空"); err != nil {
		t.Fatalf("Click 清空: %v", err)
	}
	if err := tt.Click("套用原版指令"); err != nil {
		t.Fatalf("Click 套用原版指令: %v", err)
	}

	if err := tt.Click("保存"); err != nil {
		t.Fatalf("Click 保存: %v", err)
	}
	if !tt.HasText("✓ 已保存") {
		t.Fatalf("expected save confirmation toast, texts=%v", tt.Texts())
	}

	saved := cat.Snapshot()
	if len(saved.Stratagems) != 4 {
		t.Fatalf("expected 4 stratagems after adding Ghostty, got %d", len(saved.Stratagems))
	}
	// Custom stratagem is inserted right above the pinned system stratagem at the bottom!
	if saved.Stratagems[len(saved.Stratagems)-1].ID != catalog.SystemJoinHelldiversID {
		t.Fatalf("expected system stratagem anchored at bottom, got %q", saved.Stratagems[len(saved.Stratagems)-1].ID)
	}
	added := saved.Stratagems[len(saved.Stratagems)-2]
	want500kg := []catalog.Dir{catalog.DirUp, catalog.DirRight, catalog.DirDown, catalog.DirDown, catalog.DirDown}
	if added.Name != "Ghostty" ||
		added.Delivery != catalog.DeliveryBeaconClick ||
		!slices.Equal(added.Code, want500kg) ||
		added.Steps[0].Target != "com.mitchellh.ghostty" {
		t.Fatalf("unexpected saved stratagem: %+v", added)
	}
}

func TestArsenalSystemTacticsMultiStepComboReorderAndSettingsExtras(t *testing.T) {
	cat, err := catalog.Open(t.TempDir())
	if err != nil {
		t.Fatalf("catalog.Open: %v", err)
	}

	var (
		openAtLoginVal  bool
		onboardingCalls int
		exportedData    []byte
	)

	mgr := arsenal.New(cat, func() []arsenal.AppOption {
		return []arsenal.AppOption{
			{ID: "code", Name: "VS Code", Path: "code.exe"},
		}
	})
	mgr.SetHooks(arsenal.Hooks{
		OpenOnboarding: func() { onboardingCalls++ },
		GetOpenAtLogin: func() bool { return openAtLoginVal },
		SetOpenAtLogin: func(open bool) error {
			openAtLoginVal = open
			return nil
		},
		ExportBackup: func(_ *mygo.Window, data []byte, onSaved func(string, error)) {
			exportedData = slices.Clone(data)
			onSaved("backup.json", nil)
		},
		ImportBackup: func(_ *mygo.Window, onPicked func([]byte, error)) {
			onPicked(exportedData, nil)
		},
	})
	defer mgr.Close()

	tt := ui.NewTester(mgr.View, 1090, 650)
	tt.Frame()

	// 1. Roster Reordering: initial order is [orbital-close-win, resupply-files, sys-join-helldivers]
	// Select orbital-close-win (already selected) and click ▼ to swap with resupply-files!
	if err := tt.Click("▼"); err != nil {
		t.Fatalf("Click ▼ reorder down: %v", err)
	}
	reordered := cat.Snapshot().Stratagems
	if reordered[0].ID != "resupply-files" || reordered[1].ID != "orbital-close-win" || reordered[2].ID != catalog.SystemJoinHelldiversID {
		t.Fatalf("unexpected order after ▼: [%s, %s, %s]", reordered[0].ID, reordered[1].ID, reordered[2].ID)
	}
	if err := tt.Click("▲"); err != nil {
		t.Fatalf("Click ▲ reorder up: %v", err)
	}
	if cat.Snapshot().Stratagems[0].ID != "orbital-close-win" {
		t.Fatalf("expected orbital-close-win back at index 0")
	}

	// 2. Create a Multi-Step Combo Stratagem combining Open+{clipboard} AND System Tactic (Tile Window Left)
	if err := tt.Click("+ 新建"); err != nil {
		t.Fatalf("Click + 新建: %v", err)
	}
	if err := tt.Click("打开"); err != nil {
		t.Fatalf("Click 打开 step kind: %v", err)
	}
	if err := tt.Click("+ {clipboard} 剪贴板"); err != nil {
		t.Fatalf("Click + {clipboard} 剪贴板: %v", err)
	}
	// Add Step 2 (Combo Step) -> defaults to System Tactic (win_left), pick Tile Window Right
	if err := tt.Click("+ 连携动作"); err != nil {
		t.Fatalf("Click + 连携动作: %v", err)
	}
	if !tt.HasText("步骤 1 · 打开") || !tt.HasText("步骤 2 · 系统") {
		t.Fatalf("expected multi-step combo pills, texts=%v", tt.Texts())
	}
	tt.SetSize(1090, 780)
	tt.Frame()
	trashAcc, okTrash := tt.AccessByRole(ui.RoleButton, "清空回收站")
	muteAcc, okMute := tt.AccessByRole(ui.RoleButton, "系统静音切换")
	if !okTrash || !okMute || trashAcc.Bounds.W <= 100 || muteAcc.Bounds.W <= 100 {
		t.Fatalf("expected visible system tactic tiles in 2-column grid, okTrash=%v (%+v) okMute=%v (%+v)", okTrash, trashAcc.Bounds, okMute, muteAcc.Bounds)
	}
	if wDiff := trashAcc.Bounds.W - muteAcc.Bounds.W; wDiff < -0.5 || wDiff > 0.5 {
		t.Fatalf("expected odd last tactic tile 清空回收站 to match col-1 width: trash=%.2f mute=%.2f", trashAcc.Bounds.W, muteAcc.Bounds.W)
	}
	tt.SetSize(1090, 650)
	tt.Frame()
	if err := tt.Click("窗口贴靠右半屏"); err != nil {
		t.Fatalf("Click 窗口贴靠右半屏 tactic tile: %v", err)
	}
	// Enter non-conflicting sequence ← ← ↑
	if err := tt.Click("←"); err != nil {
		t.Fatalf("Click ←: %v", err)
	}
	if err := tt.Click("←"); err != nil {
		t.Fatalf("Click ←: %v", err)
	}
	if err := tt.Click("↑"); err != nil {
		t.Fatalf("Click ↑: %v", err)
	}
	if err := tt.Click("保存"); err != nil {
		t.Fatalf("Click 保存 combo stratagem: %v", err)
	}

	snap := cat.Snapshot()
	if len(snap.Stratagems) != 4 {
		t.Fatalf("expected 4 stratagems after saving combo, got %d", len(snap.Stratagems))
	}
	combo := snap.Stratagems[2]
	if len(combo.Steps) != 2 ||
		combo.Steps[0].Kind != catalog.StepOpenTarget ||
		combo.Steps[0].Target != "{clipboard}" ||
		combo.Steps[1].Kind != catalog.StepSystemAction ||
		combo.Steps[1].Target != string(catalog.SysWinRight) {
		t.Fatalf("unexpected saved combo steps: %+v", combo.Steps)
	}

	// 3. Verify Settings extras: Game Bypass, Ship Name Randomizer, Launch at Login, Replay Onboarding, Export/Import JSON
	if err := tt.Click("通用设置"); err != nil {
		t.Fatalf("Click 通用设置: %v", err)
	}
	if err := tt.Click("全屏与游戏自动避让"); err != nil {
		t.Fatalf("Click 全屏与游戏自动避让: %v", err)
	}
	if !cat.Snapshot().Settings.DisableGameBypass {
		t.Fatal("expected DisableGameBypass=true after toggling switch off")
	}

	if err := tt.Click("随机授舰"); err != nil {
		t.Fatalf("Click 🎲 随机授舰: %v", err)
	}
	ship := cat.Snapshot().Settings.ShipName
	if !strings.HasPrefix(ship, "SES ") {
		t.Fatalf("expected randomized SES ship name, got %q", ship)
	}

	if err := tt.Click("开机自动待命"); err != nil {
		t.Fatalf("Click 开机自动待命: %v", err)
	}
	if !openAtLoginVal {
		t.Fatal("expected SetOpenAtLogin(true) to be called")
	}

	if err := tt.Click("重温入列引导…"); err != nil {
		t.Fatalf("Click 重温入列引导…: %v", err)
	}
	if onboardingCalls != 1 {
		t.Fatalf("expected OpenOnboarding called once, got %d", onboardingCalls)
	}

	if err := tt.Click("导出 JSON…"); err != nil {
		t.Fatalf("Click 导出 JSON…: %v", err)
	}
	if len(exportedData) == 0 || !tt.HasText("✓ 战备档案已导出") {
		t.Fatalf("expected JSON export payload & toast, texts=%v", tt.Texts())
	}

	// Reset defaults, then Import JSON backup and confirm our 2-step combo stratagem comes right back!
	if err := tt.Click("恢复默认战备"); err != nil {
		t.Fatalf("Click 恢复默认战备: %v", err)
	}
	if err := tt.Click("确认恢复默认"); err != nil {
		t.Fatalf("Click 确认恢复默认: %v", err)
	}
	if len(cat.Snapshot().Stratagems) != 3 {
		t.Fatalf("expected 3 stratagems after reset, got %d", len(cat.Snapshot().Stratagems))
	}

	if err := tt.Click("导入 JSON…"); err != nil {
		t.Fatalf("Click 导入 JSON…: %v", err)
	}
	if len(cat.Snapshot().Stratagems) != 4 || !tt.HasText("✓ 战备档案已导入") {
		t.Fatalf("expected 4 stratagems restored after JSON import, got %d, texts=%v", len(cat.Snapshot().Stratagems), tt.Texts())
	}

	// Also verify ShipName badge appears in Enlistment Terminal!
	if err := tt.Click("战备配置"); err != nil {
		t.Fatalf("Click 战备配置: %v", err)
	}
	if err := tt.Click("加入绝地潜兵"); err != nil {
		t.Fatalf("Click 加入绝地潜兵: %v", err)
	}
	if !tt.HasText(ship) {
		t.Fatalf("expected christened ship badge %q in Enlistment Terminal, texts=%v", ship, tt.Texts())
	}
}
