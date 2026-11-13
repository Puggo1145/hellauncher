package input

import (
	"sync"
	"time"
)

// TriggerMode defines how the global Stratagem HUD is summoned.
type TriggerMode string

const (
	// ModeDoubleHoldCtrl summons the HUD on double-tap-and-hold of Ctrl (default; avoids Ctrl+S/A/W conflicts).
	ModeDoubleHoldCtrl TriggerMode = "double_hold_ctrl"
	// ModeHoldLeftCtrl summons the HUD immediately when holding Left Ctrl.
	ModeHoldLeftCtrl TriggerMode = "hold_left_ctrl"
	// ModeRightCtrlOrAlt summons the HUD when holding Right Ctrl or Ctrl+Alt.
	ModeRightCtrlOrAlt TriggerMode = "right_ctrl_or_alt"
)

// KeyRole classifies a low-level keycode for the Stratagem input state machine.
type KeyRole int

const (
	KeyNone KeyRole = iota
	KeyDirUp
	KeyDirDown
	KeyDirLeft
	KeyDirRight
	KeyEscape
	KeyOther
)

// Mods represents currently held modifier keys.
type Mods struct {
	LeftCtrl  bool
	RightCtrl bool
	Alt       bool
	Shift     bool
	Super     bool // Command on macOS, Win on Windows
}

func (m Mods) AnyCtrl() bool {
	return m.LeftCtrl || m.RightCtrl
}

// EventKind represents an action emitted by Tracker.
type EventKind int

const (
	EventOpen EventKind = iota
	EventClose
	EventDirection
	EventInvalidKey
	EventCancel
)

// Event is dispatched asynchronously from the low-level OS hook to the app controller.
type Event struct {
	Kind EventKind
	Dir  string // "U", "D", "L", "R" when Kind == EventDirection
}

const (
	maxFirstTapDuration = 340 * time.Millisecond
	maxSecondTapGap     = 360 * time.Millisecond
)

// Tracker implements the pure, deterministic state machine shared across macOS and Windows hooks.
type Tracker struct {
	mu   sync.Mutex
	mode TriggerMode

	prevMods Mods
	active   bool
	hasTyped bool

	// Double-tap-and-hold state
	firstTapStart        time.Time
	hadOtherKeyDuringTap bool
	secondTapDeadline    time.Time

	// Set after a Stratagem finishes executing while Ctrl is still held,
	// so residual key bounces are swallowed cleanly until Ctrl is released.
	completedWaitRelease bool

	// Tracks physical keycodes swallowed on KeyDown so matching KeyUp is also swallowed.
	swallowedKeys map[uint16]bool

	emit func(Event)
}

func NewTracker(mode TriggerMode, emit func(Event)) *Tracker {
	if mode == "" {
		mode = ModeDoubleHoldCtrl
	}
	return &Tracker{
		mode:          mode,
		swallowedKeys: make(map[uint16]bool),
		emit:          emit,
	}
}

func (t *Tracker) SetMode(mode TriggerMode) {
	t.mu.Lock()
	wasActive := t.active
	t.mode = mode
	t.active = false
	t.hasTyped = false
	t.firstTapStart = time.Time{}
	t.secondTapDeadline = time.Time{}
	t.completedWaitRelease = false
	t.mu.Unlock()

	if wasActive && t.emit != nil {
		t.emit(Event{Kind: EventClose})
	}
}

func (t *Tracker) Mode() TriggerMode {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.mode
}

// CompleteCurrentSession marks the current Ctrl-hold session as satisfied after a Stratagem fires,
// preventing stray WASD leaks until the user physically releases the trigger modifier.
func (t *Tracker) CompleteCurrentSession() {
	t.mu.Lock()
	if t.active {
		t.active = false
		t.completedWaitRelease = true
	}
	t.mu.Unlock()
}

// Reset clears any active modifier state (e.g. if event tap re-enables).
func (t *Tracker) Reset() {
	t.mu.Lock()
	wasActive := t.active
	t.prevMods = Mods{}
	t.active = false
	t.hasTyped = false
	t.firstTapStart = time.Time{}
	t.secondTapDeadline = time.Time{}
	t.completedWaitRelease = false
	clear(t.swallowedKeys)
	t.mu.Unlock()

	if wasActive && t.emit != nil {
		t.emit(Event{Kind: EventClose})
	}
}

// OnModifierChange processes modifier transitions and emits Open/Close events.
func (t *Tracker) OnModifierChange(now time.Time, mods Mods) {
	var out []Event

	t.mu.Lock()
	prev := t.prevMods
	t.prevMods = mods

	if t.completedWaitRelease {
		if !t.isTriggerHeldLocked(mods) {
			t.completedWaitRelease = false
		}
		t.mu.Unlock()
		return
	}

	switch t.mode {
	case ModeDoubleHoldCtrl:
		bareCtrlNow := mods.AnyCtrl() && !mods.Super && !mods.Alt && !mods.Shift
		anyCtrlPrev := prev.AnyCtrl()
		anyCtrlNow := mods.AnyCtrl()

		if !anyCtrlPrev && bareCtrlNow {
			if !t.secondTapDeadline.IsZero() && !now.After(t.secondTapDeadline) {
				// Second tap & hold -> summon Stratagem HUD!
				t.secondTapDeadline = time.Time{}
				t.firstTapStart = time.Time{}
				t.active = true
				t.hasTyped = false
				out = append(out, Event{Kind: EventOpen})
			} else {
				// First tap started
				t.firstTapStart = now
				t.hadOtherKeyDuringTap = false
				t.secondTapDeadline = time.Time{}
			}
		} else if anyCtrlPrev && !anyCtrlNow {
			if t.active {
				t.active = false
				t.hasTyped = false
				out = append(out, Event{Kind: EventClose})
			} else if !t.firstTapStart.IsZero() {
				elapsed := now.Sub(t.firstTapStart)
				if !t.hadOtherKeyDuringTap && elapsed <= maxFirstTapDuration {
					t.secondTapDeadline = now.Add(maxSecondTapGap)
				}
				t.firstTapStart = time.Time{}
			}
		} else if !t.active && (mods.Super || mods.Alt || mods.Shift) {
			// Multi-modifier chord like Ctrl+Shift -> not a bare Ctrl tap
			t.hadOtherKeyDuringTap = true
			t.secondTapDeadline = time.Time{}
		}

	case ModeHoldLeftCtrl:
		want := mods.LeftCtrl && !mods.Super && !mods.Alt && !mods.Shift
		if !t.active && !prev.LeftCtrl && want {
			t.active = true
			t.hasTyped = false
			out = append(out, Event{Kind: EventOpen})
		} else if t.active {
			if !mods.LeftCtrl {
				t.active = false
				t.hasTyped = false
				out = append(out, Event{Kind: EventClose})
			} else if !t.hasTyped && (mods.Super || mods.Alt || mods.Shift) {
				// User pressed Ctrl+Shift+... before typing WASD -> yield silently
				t.active = false
				out = append(out, Event{Kind: EventClose})
			}
		}

	case ModeRightCtrlOrAlt:
		want := (mods.RightCtrl || (mods.AnyCtrl() && mods.Alt)) && !mods.Super
		if !t.active && want {
			t.active = true
			t.hasTyped = false
			out = append(out, Event{Kind: EventOpen})
		} else if t.active && !want {
			t.active = false
			t.hasTyped = false
			out = append(out, Event{Kind: EventClose})
		}
	}
	t.mu.Unlock()

	if t.emit != nil {
		for _, ev := range out {
			t.emit(ev)
		}
	}
}

func (t *Tracker) isTriggerHeldLocked(mods Mods) bool {
	switch t.mode {
	case ModeHoldLeftCtrl:
		return mods.LeftCtrl
	case ModeRightCtrlOrAlt:
		return mods.RightCtrl || (mods.AnyCtrl() && mods.Alt)
	default:
		return mods.AnyCtrl()
	}
}

// OnKeyDown processes a non-modifier key press and reports whether the OS hook should swallow it.
func (t *Tracker) OnKeyDown(now time.Time, keyCode uint16, role KeyRole, isRepeat bool) bool {
	var out *Event

	t.mu.Lock()
	if t.completedWaitRelease {
		// Swallow residual WASD/Arrow presses until Ctrl is released after completion.
		if role == KeyDirUp || role == KeyDirDown || role == KeyDirLeft || role == KeyDirRight || role == KeyEscape {
			t.swallowedKeys[keyCode] = true
			t.mu.Unlock()
			return true
		}
		t.mu.Unlock()
		return false
	}

	if !t.active {
		t.hadOtherKeyDuringTap = true
		t.secondTapDeadline = time.Time{}
		t.mu.Unlock()
		return false
	}

	swallow := false
	switch role {
	case KeyDirUp, KeyDirDown, KeyDirLeft, KeyDirRight:
		swallow = true
		t.swallowedKeys[keyCode] = true
		t.hasTyped = true
		if !isRepeat {
			dir := "U"
			switch role {
			case KeyDirDown:
				dir = "D"
			case KeyDirLeft:
				dir = "L"
			case KeyDirRight:
				dir = "R"
			}
			out = &Event{Kind: EventDirection, Dir: dir}
		}

	case KeyEscape:
		swallow = true
		t.swallowedKeys[keyCode] = true
		t.active = false
		t.hasTyped = false
		t.completedWaitRelease = true
		out = &Event{Kind: EventCancel}

	default:
		if t.mode == ModeHoldLeftCtrl && !t.hasTyped {
			// Let normal Ctrl+C / Ctrl+V / Ctrl+Z pass through when no WASD was typed yet!
			t.active = false
			swallow = false
			out = &Event{Kind: EventClose}
		} else {
			swallow = true
			t.swallowedKeys[keyCode] = true
			if !isRepeat {
				out = &Event{Kind: EventInvalidKey}
			}
		}
	}
	t.mu.Unlock()

	if out != nil && t.emit != nil {
		t.emit(*out)
	}
	return swallow
}

// OnKeyUp reports whether a key release should be swallowed (matching a swallowed KeyDown).
func (t *Tracker) OnKeyUp(keyCode uint16) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.swallowedKeys[keyCode] {
		delete(t.swallowedKeys, keyCode)
		return true
	}
	return false
}
