package engine_test

import (
	"context"
	"slices"
	"sync/atomic"
	"testing"
	"time"

	"hellauncher/internal/catalog"
	"hellauncher/internal/engine"
	"hellauncher/internal/sfx"
	"hellauncher/internal/sys"
)

func waitForCondition(t *testing.T, desc string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", desc)
}

func summonDoubleHoldCtrl(rig *sys.FakeRig, base time.Time) time.Time {
	t0 := base
	rig.Emit(sys.InputEvent{At: t0, Kind: sys.InputModifiers, Mods: sys.Mods{LeftCtrl: true}})
	t1 := t0.Add(80 * time.Millisecond)
	rig.Emit(sys.InputEvent{At: t1, Kind: sys.InputModifiers, Mods: sys.Mods{}})
	t2 := t1.Add(90 * time.Millisecond)
	rig.Emit(sys.InputEvent{At: t2, Kind: sys.InputModifiers, Mods: sys.Mods{LeftCtrl: true}})
	return t2
}

func TestEngineImmediateDeployAndPairedKeySwallowing(t *testing.T) {
	rig := sys.NewFakeRig()
	spk := sfx.NewRecordSpeaker()
	eng := engine.New(rig, spk, catalog.DefaultSnapshot())
	stop := rig.StartInput(eng.HandleInput, eng.SetTrusted)
	defer stop()

	now := summonDoubleHoldCtrl(rig, time.Now())
	if got := eng.Snapshot().Phase; got != engine.PhaseOpen {
		t.Fatalf("expected PhaseOpen after double-hold Ctrl, got %q", got)
	}

	// Resupply Files in DefaultSnapshot uses Helldivers 2 Resupply code: D, D, U, R.
	steps := []struct {
		kc   uint16
		role sys.KeyRole
	}{
		{1, sys.KeyDirDown},
		{1, sys.KeyDirDown},
		{13, sys.KeyDirUp},
		{2, sys.KeyDirRight},
	}
	for i, s := range steps {
		now = now.Add(25 * time.Millisecond)
		if !rig.Emit(sys.InputEvent{At: now, Kind: sys.InputKeyDown, KeyCode: s.kc, Role: s.role}) {
			t.Fatalf("step %d KeyDown should be swallowed", i)
		}
		// Release first three keys immediately; hold the final key until AFTER PhaseDeploying starts!
		if i < len(steps)-1 {
			now = now.Add(10 * time.Millisecond)
			if !rig.Emit(sys.InputEvent{At: now, Kind: sys.InputKeyUp, KeyCode: s.kc}) {
				t.Fatalf("step %d KeyUp should be swallowed", i)
			}
		}
	}

	snap := eng.Snapshot()
	if snap.Phase != engine.PhaseDeploying || snap.MatchedID != "resupply-files" {
		t.Fatalf("expected PhaseDeploying for resupply-files, got phase=%q matched=%q", snap.Phase, snap.MatchedID)
	}

	// Paired KeyUp invariant: final 'D' KeyUp arrives after leaving PhaseOpen -> MUST still be swallowed!
	if !rig.Emit(sys.InputEvent{At: now.Add(20 * time.Millisecond), Kind: sys.InputKeyUp, KeyCode: 2}) {
		t.Fatal("final KeyUp after transition to PhaseDeploying must still be swallowed")
	}
	// A second unswallowed KeyUp is NOT swallowed.
	if rig.Emit(sys.InputEvent{At: now.Add(30 * time.Millisecond), Kind: sys.InputKeyUp, KeyCode: 2}) {
		t.Fatal("unpaired KeyUp should not be swallowed")
	}

	waitForCondition(t, "FakeRig.Execute(files)", func() bool {
		return len(rig.Executed()) == 1
	})
	exec := rig.Executed()[0]
	if exec.Step.Target != catalog.DefaultFileManagerTarget() || exec.Beacon != nil {
		t.Fatalf("unexpected execute call: %+v", exec)
	}

	wantCues := []sfx.Cue{sfx.CueOpen, sfx.CueStep1, sfx.CueStep2, sfx.CueStep3, sfx.CueStep4}
	if !slices.Equal(spk.Cues(), wantCues) {
		t.Fatalf("unexpected audio cues: got %v, want %v", spk.Cues(), wantCues)
	}

	waitForCondition(t, "PhaseIdle after deployment", func() bool {
		return eng.Snapshot().Phase == engine.PhaseIdle
	})
}

func TestEngineSystemJoinHelldiversLaunchAndMissingCallback(t *testing.T) {
	rig := sys.NewFakeRig()
	spk := sfx.NewRecordSpeaker()
	eng := engine.New(rig, spk, catalog.DefaultSnapshot())
	eng.SetHelldiversReady(true)

	var missingCount atomic.Int32
	eng.SetOnMissingHelldivers(func() {
		missingCount.Add(1)
	})

	stop := rig.StartInput(eng.HandleInput, eng.SetTrusted)
	defer stop()

	// Case 1: Game missing -> returns ErrHelldiversMissing, transitions directly to PhaseIdle, fires OnMissingHelldivers.
	rig.ExecFn = func(_ context.Context, step catalog.Step, _ *sys.Point) error {
		if step.Kind == catalog.StepLaunchHelldivers {
			return sys.ErrHelldiversMissing
		}
		return nil
	}

	reinforceRoles := []struct {
		kc   uint16
		role sys.KeyRole
	}{
		{13, sys.KeyDirUp},
		{1, sys.KeyDirDown},
		{2, sys.KeyDirRight},
		{0, sys.KeyDirLeft},
		{13, sys.KeyDirUp},
	}

	now := summonDoubleHoldCtrl(rig, time.Now())
	for _, k := range reinforceRoles {
		now = now.Add(20 * time.Millisecond)
		rig.Emit(sys.InputEvent{At: now, Kind: sys.InputKeyDown, KeyCode: k.kc, Role: k.role})
		now = now.Add(5 * time.Millisecond)
		rig.Emit(sys.InputEvent{At: now, Kind: sys.InputKeyUp, KeyCode: k.kc})
	}
	rig.Emit(sys.InputEvent{At: now.Add(10 * time.Millisecond), Kind: sys.InputModifiers, Mods: sys.Mods{}})

	waitForCondition(t, "OnMissingHelldivers callback", func() bool {
		return missingCount.Load() == 1
	})
	if eng.Snapshot().HelldiversReady {
		t.Fatal("expected HelldiversReady=false after ErrHelldiversMissing")
	}
	if eng.Snapshot().Phase != engine.PhaseIdle {
		t.Fatalf("expected immediate PhaseIdle on missing Helldivers, got %q", eng.Snapshot().Phase)
	}
	if slices.Contains(spk.Cues(), sfx.CueHellpodLaunch) {
		t.Fatalf("CueHellpodLaunch must NOT play when Helldivers 2 is missing: %v", spk.Cues())
	}

	// Case 2: Game present -> launches StepLaunchHelldivers cleanly AND plays CueHellpodLaunch!
	spk.Reset()
	rig.ExecFn = nil
	eng.SetHelldiversReady(true)
	now2 := summonDoubleHoldCtrl(rig, now.Add(2*time.Second))
	for _, k := range reinforceRoles {
		now2 = now2.Add(20 * time.Millisecond)
		rig.Emit(sys.InputEvent{At: now2, Kind: sys.InputKeyDown, KeyCode: k.kc, Role: k.role})
		now2 = now2.Add(5 * time.Millisecond)
		rig.Emit(sys.InputEvent{At: now2, Kind: sys.InputKeyUp, KeyCode: k.kc})
	}
	waitForCondition(t, "StepLaunchHelldivers executed + CueHellpodLaunch", func() bool {
		return len(rig.Executed()) == 2 && slices.Contains(spk.Cues(), sfx.CueHellpodLaunch)
	})
	if rig.Executed()[1].Step.Kind != catalog.StepLaunchHelldivers {
		t.Fatalf("expected StepLaunchHelldivers, got %+v", rig.Executed()[1].Step)
	}
}

func TestEnginePauseAndFullscreenAutoBypass(t *testing.T) {
	rig := sys.NewFakeRig()
	spk := sfx.NewRecordSpeaker()
	eng := engine.New(rig, spk, catalog.DefaultSnapshot())
	stop := rig.StartInput(eng.HandleInput, eng.SetTrusted)
	defer stop()

	// 1. Paused engine ignores summon triggers.
	eng.SetPaused(true)
	t0 := summonDoubleHoldCtrl(rig, time.Now())
	if eng.Snapshot().Phase != engine.PhaseIdle {
		t.Fatalf("expected PhaseIdle while paused, got %q", eng.Snapshot().Phase)
	}
	rig.Emit(sys.InputEvent{At: t0.Add(20 * time.Millisecond), Kind: sys.InputModifiers, Mods: sys.Mods{}})

	// 2. Unpaused engine with active game bypass ignores summon until DisableGameBypass=true.
	eng.SetPaused(false)
	inFullscreenGame := true
	eng.SetShouldBypass(func() bool { return inFullscreenGame })

	t1 := summonDoubleHoldCtrl(rig, t0.Add(1*time.Second))
	if eng.Snapshot().Phase != engine.PhaseIdle {
		t.Fatalf("expected PhaseIdle during fullscreen game auto-bypass, got %q", eng.Snapshot().Phase)
	}
	rig.Emit(sys.InputEvent{At: t1.Add(20 * time.Millisecond), Kind: sys.InputModifiers, Mods: sys.Mods{}})

	loadout := catalog.DefaultSnapshot()
	loadout.Settings.DisableGameBypass = true
	eng.SetLoadout(loadout)

	t2 := summonDoubleHoldCtrl(rig, t1.Add(1*time.Second))
	if eng.Snapshot().Phase != engine.PhaseOpen {
		t.Fatalf("expected PhaseOpen when DisableGameBypass=true, got %q", eng.Snapshot().Phase)
	}
	_ = t2
}

func TestEngineBeaconClickMouseCaptureLifecycle(t *testing.T) {
	rig := sys.NewFakeRig()
	spk := sfx.NewRecordSpeaker()

	loadout := catalog.DefaultSnapshot()
	loadout.Stratagems = append(loadout.Stratagems, catalog.Stratagem{
		ID:       "drop-term-here",
		Name:     "信标空投终端",
		Category: catalog.CategoryGreen,
		Enabled:  true,
		Code:     []catalog.Dir{catalog.DirUp, catalog.DirLeft},
		Delivery: catalog.DeliveryBeaconClick,
		Steps:    []catalog.Step{{Kind: catalog.StepLaunchApp, Target: "terminal"}},
	})

	eng := engine.New(rig, spk, loadout)
	stop := rig.StartInput(eng.HandleInput, eng.SetTrusted)
	defer stop()

	if rig.MouseCaptured() {
		t.Fatal("mouse capture must be disabled while idle")
	}

	now := summonDoubleHoldCtrl(rig, time.Now())
	rig.Emit(sys.InputEvent{At: now.Add(20 * time.Millisecond), Kind: sys.InputKeyDown, KeyCode: 13, Role: sys.KeyDirUp})
	rig.Emit(sys.InputEvent{At: now.Add(30 * time.Millisecond), Kind: sys.InputKeyUp, KeyCode: 13})
	rig.Emit(sys.InputEvent{At: now.Add(45 * time.Millisecond), Kind: sys.InputKeyDown, KeyCode: 0, Role: sys.KeyDirLeft})
	rig.Emit(sys.InputEvent{At: now.Add(55 * time.Millisecond), Kind: sys.InputKeyUp, KeyCode: 0})

	// Release Ctrl while holding armed beacon in hand -> stays PhaseArmed!
	rig.Emit(sys.InputEvent{At: now.Add(70 * time.Millisecond), Kind: sys.InputModifiers, Mods: sys.Mods{}})

	if got := eng.Snapshot().Phase; got != engine.PhaseArmed {
		t.Fatalf("expected PhaseArmed after releasing Ctrl with beacon armed, got %q", got)
	}
	if !rig.MouseCaptured() {
		t.Fatal("mouse capture must be enabled during PhaseArmed")
	}

	// Left-click at (640, 480) deploys the beacon and immediately releases mouse capture!
	targetPt := sys.Point{X: 640, Y: 480}
	if !rig.Emit(sys.InputEvent{At: now.Add(120 * time.Millisecond), Kind: sys.InputMouseLeftDown, Point: targetPt}) {
		t.Fatal("left click during PhaseArmed must be swallowed")
	}
	if rig.MouseCaptured() {
		t.Fatal("mouse capture must be disabled immediately after beacon click")
	}

	waitForCondition(t, "beacon Execute", func() bool {
		return len(rig.Executed()) == 1
	})
	call := rig.Executed()[0]
	if call.Beacon == nil || *call.Beacon != targetPt {
		t.Fatalf("expected beacon=%+v, got %+v", targetPt, call.Beacon)
	}
	if wantBeaconCues := []sfx.Cue{sfx.CueOpen, sfx.CueStep1, sfx.CueStep2}; !slices.Equal(spk.Cues(), wantBeaconCues) {
		t.Fatalf("expected beacon deploy cues %v without post-command sound, got %v", wantBeaconCues, spk.Cues())
	}

	// Also verify right-click cancels PhaseArmed cleanly:
	now2 := summonDoubleHoldCtrl(rig, now.Add(2*time.Second))
	rig.Emit(sys.InputEvent{At: now2.Add(20 * time.Millisecond), Kind: sys.InputKeyDown, KeyCode: 13, Role: sys.KeyDirUp})
	rig.Emit(sys.InputEvent{At: now2.Add(40 * time.Millisecond), Kind: sys.InputKeyDown, KeyCode: 0, Role: sys.KeyDirLeft})
	rig.Emit(sys.InputEvent{At: now2.Add(55 * time.Millisecond), Kind: sys.InputModifiers, Mods: sys.Mods{}})
	if !rig.MouseCaptured() {
		t.Fatal("expected mouse capture on second arming")
	}
	if !rig.Emit(sys.InputEvent{At: now2.Add(80 * time.Millisecond), Kind: sys.InputMouseRightDown, Point: targetPt}) {
		t.Fatal("right click cancel should be swallowed")
	}
	if rig.MouseCaptured() || eng.Snapshot().Phase != engine.PhaseIdle {
		t.Fatalf("expected PhaseIdle and mouseCapture=false after right-click cancel, got phase=%q mouse=%v",
			eng.Snapshot().Phase, rig.MouseCaptured())
	}
	cues := spk.Cues()
	if len(cues) == 0 || cues[len(cues)-1] != sfx.CueClose {
		t.Fatalf("expected CueClose after right-click beacon cancel, got cues=%v", cues)
	}

	// Also verify opening and releasing Ctrl without completing plays CueOpen -> CueClose:
	spk.Reset()
	now3 := summonDoubleHoldCtrl(rig, now2.Add(2*time.Second))
	rig.Emit(sys.InputEvent{At: now3.Add(90 * time.Millisecond), Kind: sys.InputModifiers, Mods: sys.Mods{}})
	if want := []sfx.Cue{sfx.CueOpen, sfx.CueClose}; !slices.Equal(spk.Cues(), want) {
		t.Fatalf("expected open+close cues %v on Ctrl summon+release, got %v", want, spk.Cues())
	}
}

func TestEngineHoldLeftCtrlPassesThroughCtrlCAndWrongArrowResetsSequence(t *testing.T) {
	rig := sys.NewFakeRig()
	spk := sfx.NewRecordSpeaker()
	loadout := catalog.DefaultSnapshot()
	loadout.Settings.TriggerMode = catalog.ModeHoldLeftCtrl

	eng := engine.New(rig, spk, loadout)
	stop := rig.StartInput(eng.HandleInput, eng.SetTrusted)
	defer stop()

	t0 := time.Now()
	rig.Emit(sys.InputEvent{At: t0, Kind: sys.InputModifiers, Mods: sys.Mods{LeftCtrl: true}})
	if eng.Snapshot().Phase != engine.PhaseOpen {
		t.Fatal("expected PhaseOpen on HoldLeftCtrl")
	}

	// Pressing 'C' (KeyOther) before any WASD yields silently without swallowing!
	if rig.Emit(sys.InputEvent{At: t0.Add(30 * time.Millisecond), Kind: sys.InputKeyDown, KeyCode: 8, Role: sys.KeyOther}) {
		t.Fatal("Ctrl+C before directional input must NOT be swallowed")
	}
	if eng.Snapshot().Phase != engine.PhaseIdle {
		t.Fatalf("expected PhaseIdle after Ctrl+C pass-through, got %q", eng.Snapshot().Phase)
	}
	rig.Emit(sys.InputEvent{At: t0.Add(50 * time.Millisecond), Kind: sys.InputModifiers, Mods: sys.Mods{}})

	// Summon again, type R -> L (not a valid prefix in DefaultSnapshot) -> ErrorSeq increments, HUD stays Open!
	t1 := t0.Add(150 * time.Millisecond)
	rig.Emit(sys.InputEvent{At: t1, Kind: sys.InputModifiers, Mods: sys.Mods{LeftCtrl: true}})
	rig.Emit(sys.InputEvent{At: t1.Add(20 * time.Millisecond), Kind: sys.InputKeyDown, KeyCode: 2, Role: sys.KeyDirRight})
	rig.Emit(sys.InputEvent{At: t1.Add(40 * time.Millisecond), Kind: sys.InputKeyDown, KeyCode: 0, Role: sys.KeyDirLeft})

	snap := eng.Snapshot()
	if snap.Phase != engine.PhaseOpen || len(snap.Typed) != 0 || snap.ErrorSeq != 1 {
		t.Fatalf("expected PhaseOpen with empty Typed and ErrorSeq=1, got %+v", snap)
	}

	// SetLoadout syncs Speaker.SetMuted(true)
	loadout.Settings.Muted = true
	eng.SetLoadout(loadout)
	if !spk.Muted() {
		t.Fatal("expected Speaker to be muted after SetLoadout(Muted=true)")
	}

	// HandleInput remains non-blocking even when Rig.Execute sleeps!
	blocked := make(chan struct{})
	rig.ExecFn = func(ctx context.Context, _ catalog.Step, _ *sys.Point) error {
		<-blocked
		return nil
	}
	start := time.Now()
	rig.Emit(sys.InputEvent{At: t1.Add(60 * time.Millisecond), Kind: sys.InputKeyDown, KeyCode: 2, Role: sys.KeyDirRight})
	rig.Emit(sys.InputEvent{At: t1.Add(80 * time.Millisecond), Kind: sys.InputKeyDown, KeyCode: 2, Role: sys.KeyDirRight})
	rig.Emit(sys.InputEvent{At: t1.Add(100 * time.Millisecond), Kind: sys.InputKeyDown, KeyCode: 13, Role: sys.KeyDirUp})
	if elapsed := time.Since(start); elapsed > 50*time.Millisecond {
		t.Fatalf("HandleInput blocked on slow Execute (%v)", elapsed)
	}
	close(blocked)
}

func TestEnginePinnedPreviewAndDrillIntercept(t *testing.T) {
	rig := sys.NewFakeRig()
	spk := sfx.NewRecordSpeaker()
	eng := engine.New(rig, spk, catalog.DefaultSnapshot())
	stop := rig.StartInput(eng.HandleInput, eng.SetTrusted)
	defer stop()

	// 1. SetPinnedPreview projects PhasePreview on the real desktop HUD while idle.
	eng.SetPinnedPreview(true)
	if got := eng.Snapshot().Phase; got != engine.PhasePreview {
		t.Fatalf("expected PhasePreview when pinned idle, got %q", got)
	}

	// 2. Intercept live-fire drill so OS steps are skipped while HUD deploying animation still fires.
	interceptCh := make(chan string, 2)
	eng.SetDrillIntercept(func(st catalog.Stratagem) bool {
		interceptCh <- st.ID
		return true
	})

	now := summonDoubleHoldCtrl(rig, time.Now())
	if got := eng.Snapshot().Phase; got != engine.PhaseOpen {
		t.Fatalf("expected real summon to transition from PhasePreview to PhaseOpen, got %q", got)
	}

	// Type Resupply: Down, Down, Up, Right
	for _, k := range []struct {
		kc   uint16
		role sys.KeyRole
	}{
		{1, sys.KeyDirDown},
		{1, sys.KeyDirDown},
		{13, sys.KeyDirUp},
		{2, sys.KeyDirRight},
	} {
		now = now.Add(20 * time.Millisecond)
		rig.Emit(sys.InputEvent{At: now, Kind: sys.InputKeyDown, KeyCode: k.kc, Role: k.role})
		now = now.Add(5 * time.Millisecond)
		rig.Emit(sys.InputEvent{At: now, Kind: sys.InputKeyUp, KeyCode: k.kc})
	}

	if got := eng.Snapshot().MatchedID; got != "resupply-files" {
		t.Fatalf("expected MatchedID='resupply-files', got %q", got)
	}
	select {
	case id := <-interceptCh:
		if id != "resupply-files" {
			t.Fatalf("expected drill intercept to capture resupply-files, got %q", id)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("timed out waiting for drill intercept callback")
	}
	if len(rig.Executed()) != 0 {
		t.Fatalf("expected zero OS steps executed when drill intercept returns true, got %+v", rig.Executed())
	}

	// Releasing pinned preview returns to PhaseIdle once deploy finishes.
	eng.SetPinnedPreview(false)
	eng.SetDrillIntercept(nil)
	waitForCondition(t, "return to PhaseIdle after intercepted deploy", func() bool {
		return eng.Snapshot().Phase == engine.PhaseIdle
	})
}
