package input

import "sync/atomic"

// Hook manages a cross-platform low-level global keyboard interceptor.
type Hook struct {
	tracker   *Tracker
	events    chan Event
	trusted   atomic.Bool
	onTrustCh func(bool)
	stopFn    func()
}

// Start creates and starts the OS-level keyboard hook on a dedicated background thread.
func Start(initialMode TriggerMode, onEvent func(Event), onPermissionChange func(trusted bool)) *Hook {
	evCh := make(chan Event, 128)
	h := &Hook{
		events:    evCh,
		onTrustCh: onPermissionChange,
	}
	h.tracker = NewTracker(initialMode, func(ev Event) {
		select {
		case evCh <- ev:
		default:
		}
	})

	go func() {
		for ev := range evCh {
			if onEvent != nil {
				onEvent(ev)
			}
		}
	}()

	h.startPlatform()
	return h
}

func (h *Hook) SetMode(m TriggerMode) {
	h.tracker.SetMode(m)
}

func (h *Hook) Mode() TriggerMode {
	return h.tracker.Mode()
}

func (h *Hook) CompleteCurrentSession() {
	h.tracker.CompleteCurrentSession()
}

func (h *Hook) IsTrusted() bool {
	return h.trusted.Load()
}

func (h *Hook) setTrusted(v bool) {
	prev := h.trusted.Swap(v)
	if prev != v && h.onTrustCh != nil {
		go h.onTrustCh(v)
	}
}

func (h *Hook) Stop() {
	if h.stopFn != nil {
		h.stopFn()
	}
}
