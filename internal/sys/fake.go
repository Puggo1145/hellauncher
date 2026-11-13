package sys

import (
	"context"
	"slices"
	"sync"
	"sync/atomic"

	"helldriver/internal/catalog"
)

// ExecutedCall records one Execute invocation observed by FakeRig.
type ExecutedCall struct {
	Step   catalog.Step
	Beacon *Point
}

// FakeRig is a deterministic in-memory Rig Adapter for unit tests.
type FakeRig struct {
	mu           sync.Mutex
	onEvent      func(InputEvent) bool
	onTrust      func(bool)
	stopped      bool
	trusted      bool
	mouseCapture atomic.Bool
	executed     []ExecutedCall
	ExecFn       func(ctx context.Context, step catalog.Step, beacon *Point) error
}

func NewFakeRig() *FakeRig {
	return &FakeRig{trusted: true}
}

func (f *FakeRig) StartInput(
	onEvent func(InputEvent) (swallow bool),
	onTrust func(trusted bool),
) (stop func()) {
	f.mu.Lock()
	f.onEvent = onEvent
	f.onTrust = onTrust
	f.stopped = false
	trusted := f.trusted
	f.mu.Unlock()

	if onTrust != nil {
		onTrust(trusted)
	}

	var once sync.Once
	return func() {
		once.Do(func() {
			f.mu.Lock()
			f.stopped = true
			f.mu.Unlock()
		})
	}
}

func (f *FakeRig) SetMouseCapture(enabled bool) {
	f.mouseCapture.Store(enabled)
}

func (f *FakeRig) MouseCaptured() bool {
	return f.mouseCapture.Load()
}

func (f *FakeRig) Execute(ctx context.Context, step catalog.Step, beacon *Point) error {
	var bp *Point
	if beacon != nil {
		cp := *beacon
		bp = &cp
	}
	f.mu.Lock()
	f.executed = append(f.executed, ExecutedCall{
		Step: catalog.Step{
			Kind:   step.Kind,
			Target: step.Target,
			Args:   slices.Clone(step.Args),
		},
		Beacon: bp,
	})
	fn := f.ExecFn
	f.mu.Unlock()

	if fn != nil {
		return fn(ctx, step, bp)
	}
	return nil
}

// Emit sends an InputEvent into the registered StartInput handler and returns swallow.
func (f *FakeRig) Emit(ev InputEvent) bool {
	if (ev.Kind == InputMouseLeftDown || ev.Kind == InputMouseRightDown) && !f.mouseCapture.Load() {
		return false
	}
	f.mu.Lock()
	fn := f.onEvent
	stopped := f.stopped
	f.mu.Unlock()
	if stopped || fn == nil {
		return false
	}
	return fn(ev)
}

func (f *FakeRig) SetTrusted(trusted bool) {
	f.mu.Lock()
	f.trusted = trusted
	cb := f.onTrust
	f.mu.Unlock()
	if cb != nil {
		cb(trusted)
	}
}

func (f *FakeRig) Executed() []ExecutedCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.executed)
}
