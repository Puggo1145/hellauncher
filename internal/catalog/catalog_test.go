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
	if len(snap.Stratagems) != 5 {
		t.Fatalf("expected 5 default stratagems, got %d", len(snap.Stratagems))
	}
	if snap.Settings.TriggerMode != catalog.ModeDoubleHoldCtrl {
		t.Fatalf("unexpected default TriggerMode: %s", snap.Settings.TriggerMode)
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
		s.Stratagems = append(s.Stratagems, catalog.Stratagem{
			ID:       "beacon-browser",
			Name:     "空投浏览器信标",
			Subtitle: "左键点击屏幕坐标打开",
			Category: catalog.CategoryYellow,
			Enabled:  true,
			Code:     []catalog.Dir{catalog.DirRight, catalog.DirRight, catalog.DirUp},
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
	if !reSnap.Settings.Muted || len(reSnap.Stratagems) != 6 {
		t.Fatalf("reopened snapshot mismatch: muted=%v count=%d", reSnap.Settings.Muted, len(reSnap.Stratagems))
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

	// Default "open-chrome" has code [U, D, U]. Adding [U, D, U, R] must fail with ConflictError!
	err = cat.Update(func(s *catalog.Snapshot) error {
		s.Stratagems = append(s.Stratagems, catalog.Stratagem{
			ID:       "longer-conflict",
			Name:     "冲突长指令",
			Category: catalog.CategoryRed,
			Enabled:  true,
			Code:     []catalog.Dir{catalog.DirUp, catalog.DirDown, catalog.DirUp, catalog.DirRight},
			Delivery: catalog.DeliveryImmediate,
			Steps:    []catalog.Step{{Kind: catalog.StepCloseActiveWindow}},
		})
		return nil
	})
	var conflictErr *catalog.ConflictError
	if !errors.As(err, &conflictErr) {
		t.Fatalf("expected *ConflictError, got: %v", err)
	}
	if !slices.Equal(conflictErr.Prefix, []catalog.Dir{catalog.DirUp, catalog.DirDown, catalog.DirUp}) {
		t.Fatalf("unexpected conflict prefix: %v", conflictErr.Prefix)
	}
	if subCalled {
		t.Fatal("failed Update must not trigger subscribers")
	}
	if len(cat.Snapshot().Stratagems) != 5 {
		t.Fatal("failed Update mutated catalog length")
	}

	// Disabled Stratagems are allowed to share prefixes without deadlocking active input.
	err = cat.Update(func(s *catalog.Snapshot) error {
		s.Stratagems = append(s.Stratagems, catalog.Stratagem{
			ID:       "disabled-draft",
			Name:     "停用草稿",
			Category: catalog.CategoryRed,
			Enabled:  false,
			Code:     []catalog.Dir{catalog.DirUp, catalog.DirDown},
			Delivery: catalog.DeliveryImmediate,
			Steps:    []catalog.Step{{Kind: catalog.StepCloseActiveWindow}},
		})
		return nil
	})
	if err != nil {
		t.Fatalf("disabled stratagem should not trigger prefix conflict: %v", err)
	}
}

func TestLegacySettingsMigration(t *testing.T) {
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
}
