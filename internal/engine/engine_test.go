package engine_test

import (
	"context"
	"slices"
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

	// Open Chrome code in DefaultSnapshot is U, D, U (keycodes 13, 1, 13).
	steps := []struct {
		kc   uint16
		role sys.KeyRole
	}{
		{13, sys.KeyDirUp},
		{1, sys.KeyDirDown},
		{13, sys.KeyDirUp},
	}
	for i, s := range steps {
		now = now.Add(25 * time.Millisecond)
		if !rig.Emit(sys.InputEvent{At: now, Kind: sys.InputKeyDown, KeyCode: s.kc, Role: s.role}) {
			t.Fatalf("step %d KeyDown should be swallowed", i)
		}
		// Release first two keys immediately; hold the final key until AFTER PhaseDeploying starts!
		if i < len(steps)-1 {
			now = now.Add(10 * time.Millisecond)
			if !rig.Emit(sys.InputEvent{At: now, Kind: sys.InputKeyUp, KeyCode: s.kc}) {
				t.Fatalf("step %d KeyUp should be swallowed", i)
			}
		}
	}

	snap := eng.Snapshot()
	if snap.Phase != engine.PhaseDeploying || snap.MatchedID != "open-chrome" {
		t.Fatalf("expected PhaseDeploying for open-chrome, got phase=%q matched=%q", snap.Phase, snap.MatchedID)
	}

	// Paired KeyUp invariant: final 'W' KeyUp arrives after leaving PhaseOpen -> MUST still be swallowed!
	if !rig.Emit(sys.InputEvent{At: now.Add(20 * time.Millisecond), Kind: sys.InputKeyUp, KeyCode: 13}) {
		t.Fatal("final KeyUp after transition to PhaseDeploying must still be swallowed")
	}
	// A second unswallowed KeyUp is NOT swallowed.
	if rig.Emit(sys.InputEvent{At: now.Add(30 * time.Millisecond), Kind: sys.InputKeyUp, KeyCode: 13}) {
		t.Fatal("unpaired KeyUp should not be swallowed")
	}

	waitForCondition(t, "FakeRig.Execute(chrome)", func() bool {
		return len(rig.Executed()) == 1
	})
	exec := rig.Executed()[0]
	if exec.Step.Target != "chrome" || exec.Beacon != nil {
		t.Fatalf("unexpected execute call: %+v", exec)
	}

	wantCues := []sfx.Cue{sfx.CueOpen, sfx.CueStep1, sfx.CueStep2, sfx.CueReady}
	if !slices.Equal(spk.Cues(), wantCues) {
		t.Fatalf("unexpected audio cues: got %v, want %v", spk.Cues(), wantCues)
	}

	waitForCondition(t, "PhaseIdle after deployment", func() bool {
		return eng.Snapshot().Phase == engine.PhaseIdle
	})
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
		Code:     []catalog.Dir{catalog.DirRight, catalog.DirLeft},
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
	rig.Emit(sys.InputEvent{At: now.Add(20 * time.Millisecond), Kind: sys.InputKeyDown, KeyCode: 2, Role: sys.KeyDirRight})
	rig.Emit(sys.InputEvent{At: now.Add(30 * time.Millisecond), Kind: sys.InputKeyUp, KeyCode: 2})
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

	// Also verify right-click cancels PhaseArmed cleanly:
	now2 := summonDoubleHoldCtrl(rig, now.Add(2*time.Second))
	rig.Emit(sys.InputEvent{At: now2.Add(20 * time.Millisecond), Kind: sys.InputKeyDown, KeyCode: 2, Role: sys.KeyDirRight})
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

	// Summon again, type U -> L (not a valid prefix in DefaultSnapshot) -> ErrorSeq increments, HUD stays Open!
	t1 := t0.Add(150 * time.Millisecond)
	rig.Emit(sys.InputEvent{At: t1, Kind: sys.InputModifiers, Mods: sys.Mods{LeftCtrl: true}})
	rig.Emit(sys.InputEvent{At: t1.Add(20 * time.Millisecond), Kind: sys.InputKeyDown, KeyCode: 13, Role: sys.KeyDirUp})
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
	rig.Emit(sys.InputEvent{At: t1.Add(60 * time.Millisecond), Kind: sys.InputKeyDown, KeyCode: 13, Role: sys.KeyDirUp})
	rig.Emit(sys.InputEvent{At: t1.Add(80 * time.Millisecond), Kind: sys.InputKeyDown, KeyCode: 1, Role: sys.KeyDirDown})
	rig.Emit(sys.InputEvent{At: t1.Add(100 * time.Millisecond), Kind: sys.InputKeyDown, KeyCode: 13, Role: sys.KeyDirUp})
	if elapsed := time.Since(start); elapsed > 50*time.Millisecond {
		t.Fatalf("HandleInput blocked on slow Execute (%v)", elapsed)
	}
	close(blocked)
}
