package sys_test

import (
	"context"
	"testing"
	"time"

	"helldriver/internal/catalog"
	"helldriver/internal/sys"
)

func TestFakeRigGatesMouseClicksUntilMouseCaptureEnabled(t *testing.T) {
	rig := sys.NewFakeRig()
	var received []sys.InputKind
	stop := rig.StartInput(func(ev sys.InputEvent) bool {
		received = append(received, ev.Kind)
		return true
	}, nil)
	defer stop()

	// By default mouse capture is false: mouse clicks are never intercepted.
	if rig.Emit(sys.InputEvent{At: time.Now(), Kind: sys.InputMouseLeftDown, Point: sys.Point{X: 300, Y: 200}}) {
		t.Fatal("expected mouse click to pass through when SetMouseCapture(false)")
	}
	if len(received) != 0 {
		t.Fatalf("unexpected mouse event delivered: %v", received)
	}

	// Enable mouse capture -> click intercepted.
	rig.SetMouseCapture(true)
	if !rig.Emit(sys.InputEvent{At: time.Now(), Kind: sys.InputMouseLeftDown, Point: sys.Point{X: 300, Y: 200}}) {
		t.Fatal("expected mouse click to be swallowed when SetMouseCapture(true)")
	}
	if len(received) != 1 || received[0] != sys.InputMouseLeftDown {
		t.Fatalf("expected [InputMouseLeftDown], got %v", received)
	}

	// Execute records beacon coordinates cleanly.
	pt := sys.Point{X: 640, Y: 480}
	err := rig.Execute(context.Background(), catalog.Step{Kind: catalog.StepLaunchApp, Target: "zed"}, &pt)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	calls := rig.Executed()
	if len(calls) != 1 || calls[0].Beacon == nil || *calls[0].Beacon != pt {
		t.Fatalf("unexpected executed calls: %+v", calls)
	}
}
