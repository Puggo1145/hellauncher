package main

import (
	"testing"
	"time"

	"helldriver/internal/actions"
	tacticalaudio "helldriver/internal/audio"
	"helldriver/internal/input"
)

func TestHudControllerSequenceMatchingAndNonBlockingDeploy(t *testing.T) {
	releaseApp := make(chan struct{})
	ctrl := &HudController{
		settings:   defaultSettings(),
		stratagems: actions.DefaultStratagems(),
		phase:      "IDLE",
		audio:      tacticalaudio.New(),
		executor: &actions.Executor{
			OverrideFn: func(s actions.Stratagem) error {
				<-releaseApp
				return nil
			},
		},
	}
	defer ctrl.audio.Close()
	ctrl.audio.SetMuted(true)

	// Open HUD
	ctrl.handleInputEvent(input.Event{Kind: input.EventOpen})

	// Type W, D, S, S (prefix shared by Zed WDSSW and Close Window WDSSS)
	for _, dir := range []string{"U", "R", "D", "D"} {
		ctrl.handleInputEvent(input.Event{Kind: input.EventDirection, Dir: dir})
	}
	snap := ctrl.snapshot()
	if snap.Phase != "OPEN" || len(snap.Typed) != 4 {
		t.Fatalf("expected OPEN with 4 typed steps, got %+v", snap)
	}

	// Wrong 5th key ("L" / A) -> resets typed to empty, increments ErrorSeq, stays OPEN!
	ctrl.handleInputEvent(input.Event{Kind: input.EventDirection, Dir: "L"})
	snap = ctrl.snapshot()
	if snap.Phase != "OPEN" || len(snap.Typed) != 0 || snap.ErrorSeq != 1 {
		t.Fatalf("expected sequence reset with ErrorSeq=1 while staying OPEN, got %+v", snap)
	}

	// Complete Chrome code: U, D, U -> MUST immediately enter DEPLOYING with all 3 arrows highlighted
	// WITHOUT blocking while releaseApp is still closed (simulating cold-start app launch)!
	ctrl.handleInputEvent(input.Event{Kind: input.EventDirection, Dir: "U"})
	ctrl.handleInputEvent(input.Event{Kind: input.EventDirection, Dir: "D"})
	ctrl.handleInputEvent(input.Event{Kind: input.EventDirection, Dir: "U"})

	snap = ctrl.snapshot()
	if snap.Phase != "DEPLOYING" {
		t.Fatalf("expected immediate DEPLOYING state on final keystroke, got %q", snap.Phase)
	}
	if snap.MatchedID != "open-chrome" || len(snap.Typed) != 3 || snap.Typed[2] != "U" {
		t.Fatalf("expected all 3 arrows typed & matchedId=open-chrome immediately, got %+v", snap)
	}

	// Simulate slow cold-start taking 380ms before window appears
	time.Sleep(380 * time.Millisecond)
	if s2 := ctrl.snapshot(); s2.Phase != "DEPLOYING" {
		t.Fatalf("expected HUD to remain smoothly in DEPLOYING until window appears, got %q", s2.Phase)
	}

	// Window appears!
	close(releaseApp)

	// Should transition through DEPLOYED -> IDLE cleanly
	deadline := time.Now().Add(900 * time.Millisecond)
	sawDeployed := false
	sawIdle := false
	for time.Now().Before(deadline) {
		p := ctrl.snapshot().Phase
		if p == "DEPLOYED" {
			sawDeployed = true
		}
		if p == "IDLE" {
			sawIdle = true
			break
		}
		time.Sleep(25 * time.Millisecond)
	}
	if !sawDeployed || !sawIdle {
		t.Fatalf("expected DEPLOYING -> DEPLOYED -> IDLE transition, sawDeployed=%v sawIdle=%v (cur=%s)",
			sawDeployed, sawIdle, ctrl.snapshot().Phase)
	}
}
