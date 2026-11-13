package input

import (
	"testing"
	"time"
)

func TestDoubleHoldCtrlFlowAndConflictSafety(t *testing.T) {
	var events []Event
	tr := NewTracker(ModeDoubleHoldCtrl, func(ev Event) {
		events = append(events, ev)
	})

	t0 := time.Now()

	// 1. Rapid Ctrl+C followed by Ctrl+V should NEVER open the HUD!
	tr.OnModifierChange(t0, Mods{LeftCtrl: true})
	if tr.OnKeyDown(t0.Add(30*time.Millisecond), 8, KeyOther, false) {
		t.Fatal("Ctrl+C 'C' key must not be swallowed")
	}
	tr.OnModifierChange(t0.Add(80*time.Millisecond), Mods{})
	tr.OnModifierChange(t0.Add(160*time.Millisecond), Mods{LeftCtrl: true})
	if len(events) != 0 {
		t.Fatalf("expected 0 HUD events on Ctrl+C -> Ctrl+V, got %+v", events)
	}
	tr.OnModifierChange(t0.Add(220*time.Millisecond), Mods{})

	// 2. Clean double-tap & hold Ctrl -> opens HUD and swallows WASD!
	t1 := t0.Add(1 * time.Second)
	tr.OnModifierChange(t1, Mods{LeftCtrl: true})
	tr.OnModifierChange(t1.Add(110*time.Millisecond), Mods{})
	tr.OnModifierChange(t1.Add(210*time.Millisecond), Mods{LeftCtrl: true})

	if len(events) != 1 || events[0].Kind != EventOpen {
		t.Fatalf("expected EventOpen on double-hold Ctrl, got %+v", events)
	}

	// Press W (with auto-repeat) -> swallowed, only 1 EventDirection("U")
	if !tr.OnKeyDown(t1.Add(260*time.Millisecond), 13, KeyDirUp, false) {
		t.Fatal("expected W keydown to be swallowed")
	}
	if !tr.OnKeyDown(t1.Add(300*time.Millisecond), 13, KeyDirUp, true) {
		t.Fatal("expected W autorepeat to be swallowed")
	}
	if len(events) != 2 || events[1].Kind != EventDirection || events[1].Dir != "U" {
		t.Fatalf("expected single U direction event, got %+v", events)
	}

	// Complete session while Ctrl is still held -> residual W keyup is still swallowed
	tr.CompleteCurrentSession()
	if !tr.OnKeyUp(13) {
		t.Fatal("expected matching W keyup to be swallowed")
	}
	tr.OnModifierChange(t1.Add(400*time.Millisecond), Mods{})
}

func TestHoldLeftCtrlLetsCtrlCPassThrough(t *testing.T) {
	var events []Event
	tr := NewTracker(ModeHoldLeftCtrl, func(ev Event) {
		events = append(events, ev)
	})

	now := time.Now()
	tr.OnModifierChange(now, Mods{LeftCtrl: true})
	if len(events) != 1 || events[0].Kind != EventOpen {
		t.Fatalf("expected EventOpen, got %+v", events)
	}

	// Pressing 'C' before any WASD passes through and closes HUD cleanly
	if tr.OnKeyDown(now.Add(40*time.Millisecond), 8, KeyOther, false) {
		t.Fatal("Ctrl+C should pass through when no WASD has been typed")
	}
	if len(events) != 2 || events[1].Kind != EventClose {
		t.Fatalf("expected EventClose after passthrough, got %+v", events)
	}
}
