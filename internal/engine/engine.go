package engine

import (
	"context"
	"errors"
	"log/slog"
	"slices"
	"sync"
	"time"

	"hellauncher/internal/catalog"
	"hellauncher/internal/sfx"
	"hellauncher/internal/sys"
)

// Phase represents the lifecycle state of the tactical Stratagem engine.
type Phase string

const (
	PhaseIdle      Phase = "idle"
	PhasePreview   Phase = "preview"
	PhaseOpen      Phase = "open"
	PhaseArmed     Phase = "armed"
	PhaseDeploying Phase = "deploying"
	PhaseDeployed  Phase = "deployed"
)

const (
	maxFirstTapDuration = 340 * time.Millisecond
	maxSecondTapGap     = 360 * time.Millisecond
	minDeployingFloor   = 280 * time.Millisecond
	deployedHoldDur     = 420 * time.Millisecond
	executeStepTimeout  = 12 * time.Second
)

// Snapshot is an immutable projection of Engine state consumed by UI views.
type Snapshot struct {
	Phase           Phase
	Typed           []catalog.Dir
	MatchedID       string
	ErrorSeq        uint32
	StepSeq         uint32
	Trusted         bool
	Paused          bool
	HelldiversReady bool
	Loadout         catalog.Snapshot
}

// Engine is the core Stratagem runtime state machine Module.
//
// It owns modifier/sequence tracking, paired KeyDown/KeyUp swallowing, scoped
// PhaseArmed mouse capture, Speaker mute synchronization, and off-thread Step
// execution behind a narrow reactive Interface.
type Engine struct {
	rig sys.Rig
	spk sfx.Speaker

	mu                   sync.Mutex
	phase                Phase
	typed                []catalog.Dir
	matchedID            string
	errorSeq             uint32
	stepSeq              uint32
	trusted              bool
	paused               bool
	pinnedPreview        bool
	shouldBypass         func() bool
	drillIntercept       func(catalog.Stratagem) bool
	helldiversReady      bool
	onMissingHelldivers  func()
	loadout              catalog.Snapshot
	timerToken           uint64
	prevMods             sys.Mods
	hasTyped             bool
	firstTapStart        time.Time
	hadOtherKeyDuringTap bool
	secondTapDeadline    time.Time
	completedWaitRelease bool
	swallowedKeys        map[uint16]bool

	pubMu  sync.Mutex
	subMu  sync.Mutex
	nextID uint64
	subs   map[uint64]func(Snapshot)
}

// New constructs an Engine wired to rig and spk with initial catalog state init.
func New(rig sys.Rig, spk sfx.Speaker, init catalog.Snapshot) *Engine {
	if rig == nil {
		rig = sys.NewFakeRig()
	}
	if spk == nil {
		spk = sfx.NewRecordSpeaker()
	}
	rig.SetMouseCapture(false)
	spk.SetMuted(init.Settings.Muted)

	return &Engine{
		rig:           rig,
		spk:           spk,
		phase:         PhaseIdle,
		trusted:       true,
		loadout:       cloneLoadout(init),
		swallowedKeys: make(map[uint16]bool),
		subs:          make(map[uint64]func(Snapshot)),
	}
}

// Snapshot returns an immutable copy of the Engine's current state.
func (e *Engine) Snapshot() Snapshot {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.snapshotLocked()
}

// Subscribe registers fn to receive every committed Engine Snapshot transition.
func (e *Engine) Subscribe(fn func(Snapshot)) (cancel func()) {
	if fn == nil {
		return func() {}
	}
	e.subMu.Lock()
	e.nextID++
	id := e.nextID
	e.subs[id] = fn
	e.subMu.Unlock()

	var once sync.Once
	return func() {
		once.Do(func() {
			e.subMu.Lock()
			delete(e.subs, id)
			e.subMu.Unlock()
		})
	}
}

// SetTrusted updates OS Accessibility trust status and notifies subscribers on change.
func (e *Engine) SetTrusted(trusted bool) {
	e.mu.Lock()
	if e.trusted == trusted {
		e.mu.Unlock()
		return
	}
	e.trusted = trusted
	snap := e.snapshotLocked()
	e.mu.Unlock()

	e.publish(snap)
}

// SetHelldiversReady updates whether HELLDIVERS 2 launch coordinates are detected
// and notifies HUD subscribers on change.
func (e *Engine) SetHelldiversReady(ready bool) {
	e.mu.Lock()
	if e.helldiversReady == ready {
		e.mu.Unlock()
		return
	}
	e.helldiversReady = ready
	snap := e.snapshotLocked()
	e.mu.Unlock()

	e.publish(snap)
}

// SetOnMissingHelldivers registers a callback invoked when the user inputs the
// system "Join the Helldivers" sequence (↑↓→←↑) while no game installation is configured.
func (e *Engine) SetOnMissingHelldivers(fn func()) {
	e.mu.Lock()
	e.onMissingHelldivers = fn
	e.mu.Unlock()
}

// SetShouldBypass registers an optional foreground fullscreen/game detector checked
// when attempting to summon a new Stratagem session.
func (e *Engine) SetShouldBypass(fn func() bool) {
	e.mu.Lock()
	e.shouldBypass = fn
	e.mu.Unlock()
}

// SetPinnedPreview keeps the HUD passively visible on the desktop while idle
// (used during live desktop onboarding calibration & drill) without blocking
// real hotkey summoning or chord input.
func (e *Engine) SetPinnedPreview(pinned bool) {
	e.mu.Lock()
	if e.pinnedPreview == pinned {
		e.mu.Unlock()
		return
	}
	e.pinnedPreview = pinned
	snap := e.snapshotLocked()
	e.mu.Unlock()

	e.publish(snap)
}

// SetDrillIntercept registers an optional interceptor called before executing
// deployed Stratagem OS steps. Returning true skips OS step execution while
// preserving full HUD deployment animations and sound effects.
func (e *Engine) SetDrillIntercept(fn func(catalog.Stratagem) bool) {
	e.mu.Lock()
	e.drillIntercept = fn
	e.mu.Unlock()
}

// SetPaused pauses or resumes global Stratagem hotkey summoning and notifies subscribers.
func (e *Engine) SetPaused(paused bool) {
	var disableMouse bool
	e.mu.Lock()
	if e.paused == paused {
		e.mu.Unlock()
		return
	}
	e.paused = paused
	if paused && (e.phase == PhaseOpen || e.phase == PhaseArmed) {
		disableMouse = e.phase == PhaseArmed
		e.timerToken++
		e.phase = PhaseIdle
		e.typed = nil
		e.matchedID = ""
		e.hasTyped = false
		e.firstTapStart = time.Time{}
		e.secondTapDeadline = time.Time{}
	}
	snap := e.snapshotLocked()
	e.mu.Unlock()

	if disableMouse {
		e.rig.SetMouseCapture(false)
	}
	e.publish(snap)
}

// Paused reports whether global Stratagem hotkey summoning is currently paused.
func (e *Engine) Paused() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.paused
}

// SetLoadout atomically applies a new validated Catalog Snapshot, syncs Speaker
// mute state, and safely cancels active sessions if their trigger or armed target changed.
func (e *Engine) SetLoadout(loadout catalog.Snapshot) {
	e.spk.SetMuted(loadout.Settings.Muted)

	var disableMouse bool
	e.mu.Lock()
	prevMode := e.loadout.Settings.TriggerMode
	e.loadout = cloneLoadout(loadout)

	if prevMode != loadout.Settings.TriggerMode {
		e.firstTapStart = time.Time{}
		e.secondTapDeadline = time.Time{}
		e.completedWaitRelease = false
		if e.phase == PhaseOpen {
			e.timerToken++
			e.phase = PhaseIdle
			e.typed = nil
			e.matchedID = ""
			e.hasTyped = false
		}
	}

	if e.phase == PhaseArmed {
		if _, ok := e.findEnabledStratagemLocked(e.matchedID); !ok {
			e.timerToken++
			e.phase = PhaseIdle
			e.typed = nil
			e.matchedID = ""
			disableMouse = true
		}
	}
	snap := e.snapshotLocked()
	e.mu.Unlock()

	if disableMouse {
		e.rig.SetMouseCapture(false)
	}
	e.publish(snap)
}

// Preview opens the HUD passively for duration d without intercepting input.
func (e *Engine) Preview(d time.Duration) {
	if d <= 0 {
		d = 3 * time.Second
	}
	e.mu.Lock()
	if e.phase == PhaseOpen || e.phase == PhaseArmed || e.phase == PhaseDeploying {
		e.mu.Unlock()
		return
	}
	e.timerToken++
	tok := e.timerToken
	e.phase = PhasePreview
	e.typed = nil
	e.matchedID = ""
	snap := e.snapshotLocked()
	e.mu.Unlock()

	e.publish(snap)

	time.AfterFunc(d, func() {
		e.mu.Lock()
		if e.timerToken != tok || e.phase != PhasePreview {
			e.mu.Unlock()
			return
		}
		e.phase = PhaseIdle
		next := e.snapshotLocked()
		e.mu.Unlock()
		e.publish(next)
	})
}

// HandleInput processes one normalized OS input event inline on the hook thread
// and reports whether the event must be swallowed.
func (e *Engine) HandleInput(ev sys.InputEvent) (swallow bool) {
	now := ev.At
	if now.IsZero() {
		now = time.Now()
	}

	switch ev.Kind {
	case sys.InputKeyUp:
		e.mu.Lock()
		if e.swallowedKeys[ev.KeyCode] {
			delete(e.swallowedKeys, ev.KeyCode)
			e.mu.Unlock()
			return true
		}
		e.mu.Unlock()
		return false

	case sys.InputReset:
		return e.handleReset()

	case sys.InputModifiers:
		e.handleModifiers(now, ev.Mods)
		return false

	case sys.InputKeyDown:
		return e.handleKeyDown(ev)

	case sys.InputMouseLeftDown:
		return e.handleMouseLeftDown(ev.Point)

	case sys.InputMouseRightDown:
		return e.handleMouseRightDown()

	default:
		return false
	}
}

func (e *Engine) handleReset() bool {
	var (
		changed      bool
		disableMouse bool
		snap         Snapshot
	)
	e.mu.Lock()
	e.prevMods = sys.Mods{}
	e.hasTyped = false
	e.firstTapStart = time.Time{}
	e.hadOtherKeyDuringTap = false
	e.secondTapDeadline = time.Time{}
	e.completedWaitRelease = false
	clear(e.swallowedKeys)

	if e.phase == PhaseOpen || e.phase == PhaseArmed {
		disableMouse = e.phase == PhaseArmed
		e.timerToken++
		e.phase = PhaseIdle
		e.typed = nil
		e.matchedID = ""
		changed = true
		snap = e.snapshotLocked()
	}
	e.mu.Unlock()

	if disableMouse {
		e.rig.SetMouseCapture(false)
	}
	if changed {
		e.publish(snap)
	}
	return false
}

func (e *Engine) handleModifiers(now time.Time, mods sys.Mods) {
	var (
		opened bool
		closed bool
		snap   Snapshot
	)

	e.mu.Lock()
	prev := e.prevMods
	e.prevMods = mods
	mode := e.loadout.Settings.TriggerMode

	if e.completedWaitRelease {
		if !isTriggerHeld(mode, mods) {
			e.completedWaitRelease = false
		}
		e.mu.Unlock()
		return
	}

	// While PhaseArmed, modifier releases do not cancel the armed beacon in hand.
	if e.phase == PhaseArmed {
		e.mu.Unlock()
		return
	}

	anyCtrlPrev := prev.LeftCtrl || prev.RightCtrl
	anyCtrlNow := mods.LeftCtrl || mods.RightCtrl

	switch mode {
	case catalog.ModeHoldLeftCtrl:
		want := mods.LeftCtrl && !mods.Super && !mods.Alt && !mods.Shift
		if e.phase != PhaseOpen && !prev.LeftCtrl && want && e.canOpenSessionLocked() {
			e.openSessionLocked()
			opened = true
			snap = e.snapshotLocked()
		} else if e.phase == PhaseOpen {
			if !mods.LeftCtrl || (!e.hasTyped && (mods.Super || mods.Alt || mods.Shift)) {
				e.closeOpenSessionLocked()
				closed = true
				snap = e.snapshotLocked()
			}
		}

	case catalog.ModeRightCtrlOrAlt:
		want := (mods.RightCtrl || (anyCtrlNow && mods.Alt)) && !mods.Super
		if e.phase != PhaseOpen && want && e.canOpenSessionLocked() {
			e.openSessionLocked()
			opened = true
			snap = e.snapshotLocked()
		} else if e.phase == PhaseOpen && !want {
			e.closeOpenSessionLocked()
			closed = true
			snap = e.snapshotLocked()
		}

	default: // ModeDoubleHoldCtrl
		bareCtrlNow := anyCtrlNow && !mods.Super && !mods.Alt && !mods.Shift
		if !anyCtrlPrev && bareCtrlNow {
			if !e.secondTapDeadline.IsZero() && !now.After(e.secondTapDeadline) {
				e.secondTapDeadline = time.Time{}
				e.firstTapStart = time.Time{}
				if e.canOpenSessionLocked() {
					e.openSessionLocked()
					opened = true
					snap = e.snapshotLocked()
				}
			} else {
				e.firstTapStart = now
				e.hadOtherKeyDuringTap = false
				e.secondTapDeadline = time.Time{}
			}
		} else if anyCtrlPrev && !anyCtrlNow {
			if e.phase == PhaseOpen {
				e.closeOpenSessionLocked()
				closed = true
				snap = e.snapshotLocked()
			} else if !e.firstTapStart.IsZero() {
				if !e.hadOtherKeyDuringTap && now.Sub(e.firstTapStart) <= maxFirstTapDuration {
					e.secondTapDeadline = now.Add(maxSecondTapGap)
				}
				e.firstTapStart = time.Time{}
			}
		} else if e.phase != PhaseOpen && (mods.Super || mods.Alt || mods.Shift) {
			e.hadOtherKeyDuringTap = true
			e.secondTapDeadline = time.Time{}
		}
	}
	e.mu.Unlock()

	if opened {
		e.spk.Play(sfx.CueOpen)
		e.publish(snap)
	} else if closed {
		e.spk.Play(sfx.CueClose)
		e.publish(snap)
	}
}

func (e *Engine) handleKeyDown(ev sys.InputEvent) bool {
	var (
		changed     bool
		enableMouse bool
		disableMs   bool
		playCue     *sfx.Cue
		deployTok   uint64
		deployStrat *catalog.Stratagem
		snap        Snapshot
	)

	e.mu.Lock()
	mode := e.loadout.Settings.TriggerMode

	// 1. Armed beacon state: Escape cancels; residual WASD while Ctrl is still held is swallowed.
	if e.phase == PhaseArmed {
		if ev.Role == sys.KeyEscape {
			e.swallowedKeys[ev.KeyCode] = true
			e.timerToken++
			e.phase = PhaseIdle
			e.typed = nil
			e.matchedID = ""
			e.hasTyped = false
			e.completedWaitRelease = isTriggerHeld(mode, e.prevMods)
			disableMs = true
			changed = true
			snap = e.snapshotLocked()
			e.mu.Unlock()

			e.rig.SetMouseCapture(false)
			e.spk.Play(sfx.CueClose)
			e.publish(snap)
			return true
		}
		if _, isDir := ev.Role.Dir(); isDir && e.completedWaitRelease {
			e.swallowedKeys[ev.KeyCode] = true
			e.mu.Unlock()
			return true
		}
		e.mu.Unlock()
		return false
	}

	// 2. Post-completion guard while trigger modifier is still physically held.
	if e.completedWaitRelease {
		if _, isDir := ev.Role.Dir(); isDir || ev.Role == sys.KeyEscape {
			e.swallowedKeys[ev.KeyCode] = true
			e.mu.Unlock()
			return true
		}
		e.mu.Unlock()
		return false
	}

	// 3. Inactive engine: record non-modifier activity so Ctrl+S doesn't count as a bare tap.
	if e.phase != PhaseOpen {
		e.hadOtherKeyDuringTap = true
		e.secondTapDeadline = time.Time{}
		e.mu.Unlock()
		return false
	}

	// 4. Active PhaseOpen sequence input:
	if dir, ok := ev.Role.Dir(); ok {
		e.swallowedKeys[ev.KeyCode] = true
		e.hasTyped = true
		if ev.Repeat {
			e.mu.Unlock()
			return true
		}

		candidate := append(slices.Clone(e.typed), dir)
		var exact *catalog.Stratagem
		anyPrefix := false

		for i := range e.loadout.Stratagems {
			st := &e.loadout.Stratagems[i]
			if !st.Enabled {
				continue
			}
			if hasDirPrefix(st.Code, candidate) {
				anyPrefix = true
				if len(st.Code) == len(candidate) {
					cp := cloneStratagem(*st)
					exact = &cp
					break
				}
			}
		}

		switch {
		case !anyPrefix:
			e.typed = nil
			e.errorSeq++
			cue := sfx.CueFail
			playCue = &cue
			changed = true
			snap = e.snapshotLocked()

		case exact != nil && exact.Delivery == catalog.DeliveryBeaconClick:
			e.timerToken++
			e.typed = candidate
			e.stepSeq++
			e.phase = PhaseArmed
			e.matchedID = exact.ID
			e.completedWaitRelease = isTriggerHeld(mode, e.prevMods)
			enableMouse = true
			cue := sfx.StepCue(len(candidate))
			playCue = &cue
			changed = true
			snap = e.snapshotLocked()

		case exact != nil:
			e.timerToken++
			deployTok = e.timerToken
			deployStrat = exact
			e.typed = candidate
			e.stepSeq++
			e.phase = PhaseDeploying
			e.matchedID = exact.ID
			e.completedWaitRelease = isTriggerHeld(mode, e.prevMods)
			cue := sfx.StepCue(len(candidate))
			playCue = &cue
			changed = true
			snap = e.snapshotLocked()

		default:
			e.typed = candidate
			e.stepSeq++
			cue := sfx.StepCue(len(candidate))
			playCue = &cue
			changed = true
			snap = e.snapshotLocked()
		}
		e.mu.Unlock()

		if enableMouse {
			e.rig.SetMouseCapture(true)
		}
		if playCue != nil {
			e.spk.Play(*playCue)
		}
		if changed {
			e.publish(snap)
		}
		if deployStrat != nil {
			go e.runDeploy(deployTok, *deployStrat, nil)
		}
		return true
	}

	if ev.Role == sys.KeyEscape {
		e.swallowedKeys[ev.KeyCode] = true
		e.timerToken++
		e.phase = PhaseIdle
		e.typed = nil
		e.matchedID = ""
		e.hasTyped = false
		e.completedWaitRelease = isTriggerHeld(mode, e.prevMods)
		snap = e.snapshotLocked()
		e.mu.Unlock()

		e.spk.Play(sfx.CueClose)
		e.publish(snap)
		return true
	}

	// Non-directional key during PhaseOpen:
	if mode == catalog.ModeHoldLeftCtrl && !e.hasTyped {
		// Pass-through standard shortcuts like Ctrl+C / Ctrl+V before any WASD input!
		e.closeOpenSessionLocked()
		snap = e.snapshotLocked()
		e.mu.Unlock()
		e.spk.Play(sfx.CueClose)
		e.publish(snap)
		return false
	}

	e.swallowedKeys[ev.KeyCode] = true
	if !ev.Repeat {
		e.typed = nil
		e.errorSeq++
		cue := sfx.CueFail
		playCue = &cue
		changed = true
		snap = e.snapshotLocked()
	}
	e.mu.Unlock()

	if disableMs {
		e.rig.SetMouseCapture(false)
	}
	if playCue != nil {
		e.spk.Play(*playCue)
	}
	if changed {
		e.publish(snap)
	}
	return true
}

func (e *Engine) handleMouseLeftDown(pt sys.Point) bool {
	e.mu.Lock()
	if e.phase != PhaseArmed {
		e.mu.Unlock()
		return false
	}
	matched, ok := e.findEnabledStratagemLocked(e.matchedID)
	e.timerToken++
	tok := e.timerToken
	if !ok {
		e.phase = PhaseIdle
		e.typed = nil
		e.matchedID = ""
		snap := e.snapshotLocked()
		e.mu.Unlock()
		e.rig.SetMouseCapture(false)
		e.publish(snap)
		return true
	}

	e.phase = PhaseDeploying
	snap := e.snapshotLocked()
	e.mu.Unlock()

	e.rig.SetMouseCapture(false)
	e.publish(snap)

	beacon := pt
	go e.runDeploy(tok, matched, &beacon)
	return true
}

func (e *Engine) handleMouseRightDown() bool {
	e.mu.Lock()
	if e.phase != PhaseArmed {
		e.mu.Unlock()
		return false
	}
	e.timerToken++
	e.phase = PhaseIdle
	e.typed = nil
	e.matchedID = ""
	e.hasTyped = false
	snap := e.snapshotLocked()
	e.mu.Unlock()

	e.rig.SetMouseCapture(false)
	e.spk.Play(sfx.CueClose)
	e.publish(snap)
	return true
}

func (e *Engine) runDeploy(tok uint64, st catalog.Stratagem, beacon *sys.Point) {
	start := time.Now()
	missingHelldivers := false
	launchedHelldivers := false

	e.mu.Lock()
	interceptFn := e.drillIntercept
	e.mu.Unlock()

	if interceptFn == nil || !interceptFn(cloneStratagem(st)) {
		for idx, step := range st.Steps {
			ctx, cancel := context.WithTimeout(context.Background(), executeStepTimeout)
			err := e.rig.Execute(ctx, step, beacon)
			cancel()
			if err != nil {
				if errors.Is(err, sys.ErrHelldiversMissing) {
					missingHelldivers = true
				}
				slog.Warn("stratagem step execution failed", "id", st.ID, "step", idx, "kind", step.Kind, "err", err)
			} else if step.Kind == catalog.StepLaunchHelldivers {
				launchedHelldivers = true
			}
		}
	}

	if missingHelldivers {
		e.mu.Lock()
		e.helldiversReady = false
		onMissing := e.onMissingHelldivers
		if e.timerToken == tok && e.phase == PhaseDeploying {
			e.phase = PhaseIdle
			e.typed = nil
			e.matchedID = ""
		}
		idleSnap := e.snapshotLocked()
		e.mu.Unlock()

		e.spk.Play(sfx.CueFail)
		e.publish(idleSnap)
		if onMissing != nil {
			onMissing()
		}
		return
	}

	if launchedHelldivers {
		e.spk.Play(sfx.CueHellpodLaunch)
	}

	if elapsed := time.Since(start); elapsed < minDeployingFloor {
		time.Sleep(minDeployingFloor - elapsed)
	}

	e.mu.Lock()
	if e.timerToken != tok || e.phase != PhaseDeploying {
		e.mu.Unlock()
		return
	}
	e.phase = PhaseDeployed
	deployedSnap := e.snapshotLocked()
	e.mu.Unlock()

	e.publish(deployedSnap)

	time.AfterFunc(deployedHoldDur, func() {
		e.mu.Lock()
		if e.timerToken != tok || e.phase != PhaseDeployed {
			e.mu.Unlock()
			return
		}
		e.phase = PhaseIdle
		e.typed = nil
		e.matchedID = ""
		idleSnap := e.snapshotLocked()
		e.mu.Unlock()

		e.publish(idleSnap)
	})
}

func (e *Engine) canOpenSessionLocked() bool {
	if e.paused {
		return false
	}
	if !e.loadout.Settings.DisableGameBypass && e.shouldBypass != nil && e.shouldBypass() {
		return false
	}
	return true
}

func (e *Engine) openSessionLocked() {
	e.timerToken++
	e.phase = PhaseOpen
	e.typed = nil
	e.matchedID = ""
	e.hasTyped = false
}

func (e *Engine) closeOpenSessionLocked() {
	e.timerToken++
	e.phase = PhaseIdle
	e.typed = nil
	e.matchedID = ""
	e.hasTyped = false
}

func (e *Engine) findEnabledStratagemLocked(id string) (catalog.Stratagem, bool) {
	for _, st := range e.loadout.Stratagems {
		if st.Enabled && st.ID == id {
			return cloneStratagem(st), true
		}
	}
	return catalog.Stratagem{}, false
}

func (e *Engine) snapshotLocked() Snapshot {
	phase := e.phase
	if phase == PhaseIdle && e.pinnedPreview && !e.paused {
		phase = PhasePreview
	}
	return Snapshot{
		Phase:           phase,
		Typed:           slices.Clone(e.typed),
		MatchedID:       e.matchedID,
		ErrorSeq:        e.errorSeq,
		StepSeq:         e.stepSeq,
		Trusted:         e.trusted,
		Paused:          e.paused,
		HelldiversReady: e.helldiversReady,
		Loadout:         cloneLoadout(e.loadout),
	}
}

func (e *Engine) publish(snap Snapshot) {
	e.pubMu.Lock()
	defer e.pubMu.Unlock()

	e.subMu.Lock()
	listeners := make([]func(Snapshot), 0, len(e.subs))
	for _, fn := range e.subs {
		listeners = append(listeners, fn)
	}
	e.subMu.Unlock()

	for _, fn := range listeners {
		fn(Snapshot{
			Phase:           snap.Phase,
			Typed:           slices.Clone(snap.Typed),
			MatchedID:       snap.MatchedID,
			ErrorSeq:        snap.ErrorSeq,
			StepSeq:         snap.StepSeq,
			Trusted:         snap.Trusted,
			Paused:          snap.Paused,
			HelldiversReady: snap.HelldiversReady,
			Loadout:         cloneLoadout(snap.Loadout),
		})
	}
}

func isTriggerHeld(mode catalog.TriggerMode, mods sys.Mods) bool {
	switch mode {
	case catalog.ModeHoldLeftCtrl:
		return mods.LeftCtrl
	case catalog.ModeRightCtrlOrAlt:
		return mods.RightCtrl || ((mods.LeftCtrl || mods.RightCtrl) && mods.Alt)
	default:
		return mods.LeftCtrl || mods.RightCtrl
	}
}

func hasDirPrefix(code, prefix []catalog.Dir) bool {
	if len(prefix) > len(code) {
		return false
	}
	for i := range prefix {
		if code[i] != prefix[i] {
			return false
		}
	}
	return true
}

func cloneStratagem(st catalog.Stratagem) catalog.Stratagem {
	steps := make([]catalog.Step, len(st.Steps))
	for i, step := range st.Steps {
		steps[i] = catalog.Step{
			Kind:   step.Kind,
			Target: step.Target,
			Args:   slices.Clone(step.Args),
		}
	}
	return catalog.Stratagem{
		ID:       st.ID,
		Name:     st.Name,
		Subtitle: st.Subtitle,
		Category: st.Category,
		Icon:     st.Icon,
		Enabled:  st.Enabled,
		Code:     slices.Clone(st.Code),
		Delivery: st.Delivery,
		Steps:    steps,
	}
}

func cloneLoadout(in catalog.Snapshot) catalog.Snapshot {
	out := catalog.Snapshot{
		Settings:   in.Settings,
		Stratagems: make([]catalog.Stratagem, len(in.Stratagems)),
	}
	for i, st := range in.Stratagems {
		out.Stratagems[i] = cloneStratagem(st)
	}
	return out
}
