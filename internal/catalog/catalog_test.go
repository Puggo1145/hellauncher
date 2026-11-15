package catalog_test

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"hellauncher/internal/catalog"
)

func TestOpenSeedsDefaultsAndPersistsAtomically(t *testing.T) {
	dir := t.TempDir()
	cat, err := catalog.Open(dir)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}

	snap := cat.Snapshot()
	if len(snap.Stratagems) != 3 {
		t.Fatalf("expected 3 default stratagems (2 shortcuts + 1 system), got %d", len(snap.Stratagems))
	}
	if snap.Settings.TriggerMode != catalog.ModeDoubleHoldCtrl {
		t.Fatalf("unexpected default TriggerMode: %s", snap.Settings.TriggerMode)
	}

	// Verify authentic Helldivers 2 default codes:
	// 0: Orbital Precision Strike -> Close Window (R, R, U)
	// 1: Resupply -> File Manager (D, D, U, R)
	// 2: System Join the Helldivers -> Reinforce (U, D, R, L, U)
	if !slices.Equal(snap.Stratagems[0].Code, []catalog.Dir{catalog.DirRight, catalog.DirRight, catalog.DirUp}) {
		t.Fatalf("unexpected orbital close window code: %v", snap.Stratagems[0].Code)
	}
	if !slices.Equal(snap.Stratagems[1].Code, []catalog.Dir{catalog.DirDown, catalog.DirDown, catalog.DirUp, catalog.DirRight}) {
		t.Fatalf("unexpected file resupply code (must match HD2 Resupply ↓↓↑→): %v", snap.Stratagems[1].Code)
	}
	wantReinforce := []catalog.Dir{catalog.DirUp, catalog.DirDown, catalog.DirRight, catalog.DirLeft, catalog.DirUp}
	if snap.Stratagems[2].ID != catalog.SystemJoinHelldiversID ||
		snap.Stratagems[2].Name != "加入绝地潜兵" ||
		snap.Stratagems[2].Icon != "reinforce.svg" ||
		!slices.Equal(snap.Stratagems[2].Code, wantReinforce) {
		t.Fatalf("unexpected system stratagem: %+v", snap.Stratagems[2])
	}

	// Mutating returned Snapshot slice must never affect Catalog's internal state.
	snap.Stratagems[0].Name = "TAMPERED"
	snap.Stratagems[0].Code[0] = catalog.DirLeft
	if cat.Snapshot().Stratagems[0].Name == "TAMPERED" {
		t.Fatal("Snapshot leaked mutable internal memory")
	}

	// Update and reopen from disk to verify persistence.
	var notified int
	cancel := cat.Subscribe(func(updated catalog.Snapshot) {
		notified++
		if !updated.Settings.Muted {
			t.Fatal("expected Muted=true in subscriber snapshot")
		}
	})
	defer cancel()

	err = cat.Update(func(s *catalog.Snapshot) error {
		s.Settings.Muted = true
		s.Settings.HelldiversPath = `D:\SteamLibrary\steamapps\common\Helldivers 2\bin\helldivers2.exe`
		s.Stratagems = append(s.Stratagems, catalog.Stratagem{
			ID:       "beacon-browser",
			Name:     "空投浏览器信标",
			Subtitle: "左键点击屏幕坐标打开",
			Category: catalog.CategoryBlue,
			Enabled:  true,
			Code:     []catalog.Dir{catalog.DirUp, catalog.DirRight, catalog.DirDown},
			Delivery: catalog.DeliveryBeaconClick,
			Steps:    []catalog.Step{{Kind: catalog.StepOpenTarget, Target: "https://helldivers.com"}},
		})
		return nil
	})
	if err != nil {
		t.Fatalf("Update failed: %v", err)
	}
	if notified != 1 {
		t.Fatalf("expected 1 subscriber call, got %d", notified)
	}

	reopened, err := catalog.Open(dir)
	if err != nil {
		t.Fatalf("Reopen failed: %v", err)
	}
	reSnap := reopened.Snapshot()
	if !reSnap.Settings.Muted || len(reSnap.Stratagems) != 4 {
		t.Fatalf("reopened snapshot mismatch: muted=%v count=%d", reSnap.Settings.Muted, len(reSnap.Stratagems))
	}
	if reSnap.Stratagems[2].Steps[0].Target != `D:\SteamLibrary\steamapps\common\Helldivers 2\bin\helldivers2.exe` {
		t.Fatalf("expected custom HelldiversPath synced to system stratagem step, got %q", reSnap.Stratagems[2].Steps[0].Target)
	}
}

func TestSystemStratagemCannotBeRemovedOrTampered(t *testing.T) {
	cat, err := catalog.Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}

	// 1. Attempting to remove sys-join-helldivers must fail with ValSystemProtected.
	err = cat.Update(func(s *catalog.Snapshot) error {
		filtered := make([]catalog.Stratagem, 0, 2)
		for _, st := range s.Stratagems {
			if st.ID != catalog.SystemJoinHelldiversID {
				filtered = append(filtered, st)
			}
		}
		s.Stratagems = filtered
		return nil
	})
	var valErr *catalog.ValidationError
	if !errors.As(err, &valErr) || valErr.Code != catalog.ValSystemProtected {
		t.Fatalf("expected ValSystemProtected when deleting system stratagem, got: %v", err)
	}

	// 2. Attempting to disable or change sequence of sys-join-helldivers must fail.
	err = cat.Update(func(s *catalog.Snapshot) error {
		for i := range s.Stratagems {
			if s.Stratagems[i].ID == catalog.SystemJoinHelldiversID {
				s.Stratagems[i].Enabled = false
			}
		}
		return nil
	})
	if !errors.As(err, &valErr) || valErr.Code != catalog.ValSystemProtected {
		t.Fatalf("expected ValSystemProtected when disabling system stratagem, got: %v", err)
	}

	err = cat.Update(func(s *catalog.Snapshot) error {
		for i := range s.Stratagems {
			if s.Stratagems[i].ID == catalog.SystemJoinHelldiversID {
				s.Stratagems[i].Code = []catalog.Dir{catalog.DirUp, catalog.DirUp}
			}
		}
		return nil
	})
	if !errors.As(err, &valErr) || valErr.Code != catalog.ValSystemProtected {
		t.Fatalf("expected ValSystemProtected when mutating system stratagem code, got: %v", err)
	}
}

func TestPrefixCollisionRejectedWithoutMutatingState(t *testing.T) {
	dir := t.TempDir()
	cat, err := catalog.Open(dir)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}

	subCalled := false
	cancel := cat.Subscribe(func(catalog.Snapshot) {
		subCalled = true
	})
	defer cancel()

	// Default "orbital-close-win" has code [R, R, U]. Adding [R, R, U, R] must fail with ConflictError!
	err = cat.Update(func(s *catalog.Snapshot) error {
		s.Stratagems = append(s.Stratagems, catalog.Stratagem{
			ID:       "longer-conflict",
			Name:     "冲突长指令",
			Category: catalog.CategoryRed,
			Enabled:  true,
			Code:     []catalog.Dir{catalog.DirRight, catalog.DirRight, catalog.DirUp, catalog.DirRight},
			Delivery: catalog.DeliveryImmediate,
			Steps:    []catalog.Step{{Kind: catalog.StepCloseActiveWindow}},
		})
		return nil
	})
	var conflictErr *catalog.ConflictError
	if !errors.As(err, &conflictErr) {
		t.Fatalf("expected *ConflictError, got: %v", err)
	}
	if !slices.Equal(conflictErr.Prefix, []catalog.Dir{catalog.DirRight, catalog.DirRight, catalog.DirUp}) {
		t.Fatalf("unexpected conflict prefix: %v", conflictErr.Prefix)
	}
	if subCalled {
		t.Fatal("failed Update must not trigger subscribers")
	}
	if len(cat.Snapshot().Stratagems) != 3 {
		t.Fatal("failed Update mutated catalog length")
	}

	// Disabled Stratagems are allowed to share prefixes without deadlocking active input.
	err = cat.Update(func(s *catalog.Snapshot) error {
		s.Stratagems = append(s.Stratagems, catalog.Stratagem{
			ID:       "disabled-draft",
			Name:     "停用草稿",
			Category: catalog.CategoryRed,
			Enabled:  false,
			Code:     []catalog.Dir{catalog.DirRight, catalog.DirRight},
			Delivery: catalog.DeliveryImmediate,
			Steps:    []catalog.Step{{Kind: catalog.StepCloseActiveWindow}},
		})
		return nil
	})
	if err != nil {
		t.Fatalf("disabled stratagem should not trigger prefix conflict: %v", err)
	}
}

func TestLegacySettingsAndOldCatalogMigration(t *testing.T) {
	dir := t.TempDir()
	legacyJSON := `{"triggerMode":"hold_left_ctrl","muted":true,"visorCurved":false}`
	if err := os.WriteFile(filepath.Join(dir, "settings.json"), []byte(legacyJSON), 0o644); err != nil {
		t.Fatal(err)
	}

	cat, err := catalog.Open(dir)
	if err != nil {
		t.Fatalf("Open with legacy settings failed: %v", err)
	}
	got := cat.Snapshot().Settings
	if got.TriggerMode != catalog.ModeHoldLeftCtrl || !got.Muted || got.VisorCurved {
		t.Fatalf("legacy settings not migrated cleanly: %+v", got)
	}

	// Also verify opening an older catalog.json missing sys-join-helldivers & using removed tactical-* icon keys
	dir2 := t.TempDir()
	oldCatalog := `{
  "version": 1,
  "settings": {"triggerMode": "double_hold_ctrl"},
  "stratagems": [
    {
      "id": "open-chrome",
      "name": "Chrome",
      "category": "blue",
      "icon": "tactical-chrome",
      "enabled": true,
      "code": ["U", "D", "U"],
      "delivery": "immediate",
      "steps": [{"kind": "launch_app", "target": "chrome"}]
    }
  ]
}`
	if err := os.WriteFile(filepath.Join(dir2, "catalog.json"), []byte(oldCatalog), 0o644); err != nil {
		t.Fatal(err)
	}
	cat2, err := catalog.Open(dir2)
	if err != nil {
		t.Fatalf("Open old catalog failed: %v", err)
	}
	s2 := cat2.Snapshot()
	if len(s2.Stratagems) != 2 || s2.Stratagems[1].ID != catalog.SystemJoinHelldiversID {
		t.Fatalf("expected sys-join-helldivers injected on open, got %+v", s2.Stratagems)
	}
	if s2.Stratagems[0].Icon == "tactical-chrome" {
		t.Fatalf("expected legacy tactical-chrome icon migrated to official svg, got %q", s2.Stratagems[0].Icon)
	}

	// Verify defensive migration when upgrading from legacy ↓↓↓↓↓ Join-the-Helldivers
	// with a user stratagem that overlaps the new Reinforce code (↑↓→←↑).
	dir3 := t.TempDir()
	conflictCatalog := `{
  "version": 1,
  "settings": {"triggerMode": "double_hold_ctrl"},
  "stratagems": [
    {
      "id": "user-udr",
      "name": "Custom UDR",
      "category": "blue",
      "icon": "jump-pack.svg",
      "enabled": true,
      "code": ["U", "D", "R"],
      "delivery": "immediate",
      "steps": [{"kind": "system_action", "target": "win_left"}]
    },
    {
      "id": "sys-join-helldivers",
      "name": "加入绝地潜兵",
      "category": "yellow",
      "icon": "super-earth-flag.svg",
      "enabled": true,
      "code": ["D", "D", "D", "D", "D"],
      "delivery": "immediate",
      "steps": [{"kind": "launch_helldivers"}]
    }
  ]
}`
	if err := os.WriteFile(filepath.Join(dir3, "catalog.json"), []byte(conflictCatalog), 0o644); err != nil {
		t.Fatal(err)
	}
	cat3, err := catalog.Open(dir3)
	if err != nil {
		t.Fatalf("Open conflicting legacy catalog failed: %v", err)
	}
	s3 := cat3.Snapshot()
	if len(s3.Stratagems) != 2 {
		t.Fatalf("expected user stratagem preserved during migration, got %d items", len(s3.Stratagems))
	}
	if s3.Stratagems[0].Enabled {
		t.Fatal("expected conflicting user stratagem to be safely disabled on Reinforce code migration")
	}
	if s3.Stratagems[1].Icon != "reinforce.svg" || !slices.Equal(s3.Stratagems[1].Code, catalog.SystemJoinHelldiversCode) {
		t.Fatalf("expected sys-join-helldivers migrated to reinforce.svg + ↑↓→←↑, got %+v", s3.Stratagems[1])
	}
}

func TestSystemActionsExportImportAndShipNames(t *testing.T) {
	cat, err := catalog.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	// Valid system_action + multi-step combo succeeds.
	err = cat.Update(func(s *catalog.Snapshot) error {
		s.Settings.ShipName = catalog.RandomShipName(catalog.LangZH)
		s.Settings.Onboarded = true
		s.Stratagems = append(s.Stratagems, catalog.Stratagem{
			ID:       "split-combo",
			Name:     "左半屏分屏战术",
			Category: catalog.CategoryGreen,
			Icon:     "shield-generator-relay.svg",
			Enabled:  true,
			Code:     []catalog.Dir{catalog.DirUp, catalog.DirRight, catalog.DirRight},
			Delivery: catalog.DeliveryImmediate,
			Steps: []catalog.Step{
				{Kind: catalog.StepSystemAction, Target: string(catalog.SysWinLeft)},
				{Kind: catalog.StepOpenTarget, Target: "https://google.com/search?q={clipboard}"},
			},
		})
		return nil
	})
	if err != nil {
		t.Fatalf("expected valid system_action combo to pass, got %v", err)
	}

	// Invalid system_action target must be rejected.
	err = cat.Update(func(s *catalog.Snapshot) error {
		s.Stratagems = append(s.Stratagems, catalog.Stratagem{
			ID:       "bad-sys",
			Name:     "Bad Sys",
			Category: catalog.CategoryGreen,
			Icon:     "sos.svg",
			Enabled:  false,
			Code:     []catalog.Dir{catalog.DirLeft, catalog.DirLeft},
			Delivery: catalog.DeliveryImmediate,
			Steps:    []catalog.Step{{Kind: catalog.StepSystemAction, Target: "not_a_real_action"}},
		})
		return nil
	})
	if err == nil {
		t.Fatal("expected invalid system_action target to fail validation")
	}

	raw, err := cat.ExportJSON()
	if err != nil {
		t.Fatalf("ExportJSON failed: %v", err)
	}
	catDest, err := catalog.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := catDest.ImportJSON(raw); err != nil {
		t.Fatalf("ImportJSON failed: %v", err)
	}
	if len(catDest.Snapshot().Stratagems) != 4 || catDest.Snapshot().Settings.ShipName == "" {
		t.Fatalf("unexpected imported snapshot: %+v", catDest.Snapshot())
	}
}
