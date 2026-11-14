package arsenal_test

import (
	"slices"
	"testing"

	"github.com/egoist/mygo/ui"

	"hellauncher/internal/arsenal"
	"hellauncher/internal/catalog"
)

func TestArsenalCreateRecordConflictAndSaveFlow(t *testing.T) {
	cat, err := catalog.Open(t.TempDir())
	if err != nil {
		t.Fatalf("catalog.Open: %v", err)
	}

	mgr := arsenal.New(cat, func() []arsenal.AppOption {
		return []arsenal.AppOption{
			{ID: "com.mitchellh.ghostty", Name: "Ghostty", Path: "/Applications/Ghostty.app"},
		}
	})
	defer mgr.Close()

	tt := ui.NewTester(mgr.View, 960, 700)
	tt.Frame()

	if !tt.HasText("Google Chrome") || !tt.HasText("Zed Editor") {
		t.Fatalf("expected default roster in Arsenal, texts=%v", tt.Texts())
	}

	// Toggle global sound checkbox -> updates Catalog immediately!
	if err := tt.Click("按键音效"); err != nil {
		t.Fatalf("Click 按键音效: %v", err)
	}
	if !cat.Snapshot().Settings.Muted {
		t.Fatal("expected Catalog Settings.Muted=true after unchecking 按键音效")
	}

	// Create a new Stratagem
	if err := tt.Click("+ 新建战备"); err != nil {
		t.Fatalf("Click + 新建战备: %v", err)
	}

	// Pick installed app "Ghostty" from quick picker
	if err := tt.Click("Ghostty"); err != nil {
		t.Fatalf("Click Ghostty app chip: %v", err)
	}

	// Select BeaconClick delivery mode
	if err := tt.Click("⊕ 握持信标 · 左键点击目标位置空投"); err != nil {
		t.Fatalf("Click BeaconClick radio: %v", err)
	}

	// Record conflicting sequence ↑↓↑ (same as Google Chrome) via keyboard recorder!
	if err := tt.Click("录制按键序列"); err != nil {
		t.Fatalf("Click 录制按键序列: %v", err)
	}
	tt.Key(0, ui.KeyW)
	tt.Key(0, ui.KeyS)
	tt.Key(0, ui.KeyW)
	tt.Key(0, ui.KeyEnter)

	// Attempt to save -> Catalog rejects prefix conflict and Arsenal surfaces inline error banner!
	if err := tt.Click("保存战备配置"); err != nil {
		t.Fatalf("Click 保存战备配置: %v", err)
	}
	if !tt.HasText("指令序列冲突") {
		t.Fatalf("expected inline ConflictError banner, texts=%v", tt.Texts())
	}

	// Clear sequence and enter non-conflicting sequence →← (Right, Left)
	if err := tt.Click("清空序列"); err != nil {
		t.Fatalf("Click 清空序列: %v", err)
	}
	if err := tt.Click("+ → (D)"); err != nil {
		t.Fatalf("Click + → (D): %v", err)
	}
	if err := tt.Click("+ ← (A)"); err != nil {
		t.Fatalf("Click + ← (A): %v", err)
	}

	if err := tt.Click("保存战备配置"); err != nil {
		t.Fatalf("Click 保存战备配置 after fix: %v", err)
	}
	if !tt.HasText("✓ 战备已保存并同步至战术终端") {
		t.Fatalf("expected save confirmation banner, texts=%v", tt.Texts())
	}

	// Verify persisted Catalog state
	saved := cat.Snapshot()
	if len(saved.Stratagems) != 6 {
		t.Fatalf("expected 6 stratagems after adding Ghostty, got %d", len(saved.Stratagems))
	}
	added := saved.Stratagems[len(saved.Stratagems)-1]
	if added.Name != "Ghostty" ||
		added.Delivery != catalog.DeliveryBeaconClick ||
		!slices.Equal(added.Code, []catalog.Dir{catalog.DirRight, catalog.DirLeft}) ||
		added.Steps[0].Target != "com.mitchellh.ghostty" {
		t.Fatalf("unexpected saved stratagem: %+v", added)
	}
}
