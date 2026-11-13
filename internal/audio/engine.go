package tacticalaudio

import (
	"embed"
	"fmt"
	"log"
	"sync"
	"sync/atomic"
	"time"

	"github.com/egoist/mygo/audio"
	_ "github.com/egoist/mygo/audio/vorbis"
)

//go:embed sounds/*.ogg
var soundsFS embed.FS

// keepWarmInterval stays safely below mygo/audio's 5-second idleClose window so
// CoreAudio (AudioQueue) / WASAPI stays open and ready for zero-lag first keystrokes.
const keepWarmInterval = 3200 * time.Millisecond

// Engine plays pre-decoded Helldivers 2 wrist-terminal keyboard sound effects
// directly via Go's native audio mixer with zero browser or cold-device latency.
type Engine struct {
	beeps      [8]*audio.Sound
	fails      [2]*audio.Sound
	ready      *audio.Sound
	keepWarm   *audio.Sound
	muted      atomic.Bool
	closed     atomic.Bool
	warmActive atomic.Bool
	failToggle atomic.Uint32

	warmOnce  sync.Once
	closeOnce sync.Once
	stopCh    chan struct{}
}

// New decodes all embedded keyboard SFX into memory once at startup.
func New() *Engine {
	audio.SetLatency(audio.LatencyInteractive)

	e := &Engine{
		keepWarm: audio.NewSound(48000, 1, make([]float32, 64)),
		stopCh:   make(chan struct{}),
	}
	for i := 0; i < 8; i++ {
		name := fmt.Sprintf("sounds/stratagem-input-beep-%d.ogg", i+1)
		e.beeps[i] = loadSound(name)
	}
	e.fails[0] = loadSound("sounds/stratagem-input-fail-1.ogg")
	e.fails[1] = loadSound("sounds/stratagem-input-fail-2.ogg")
	e.ready = loadSound("sounds/stratagem-input-ready.ogg")
	return e
}

func loadSound(path string) *audio.Sound {
	data, err := soundsFS.ReadFile(path)
	if err != nil {
		log.Printf("[audio] failed reading %s: %v", path, err)
		return nil
	}
	snd, err := audio.DecodeSound(data)
	if err != nil {
		log.Printf("[audio] failed decoding %s: %v", path, err)
		return nil
	}
	return snd
}

// WarmUp wakes the native OS audio output queue with a 1.3ms silent buffer
// so the next directional keystroke plays with zero cold-start lag.
func (e *Engine) WarmUp() {
	if e.muted.Load() || e.closed.Load() || e.keepWarm == nil {
		return
	}
	e.keepWarm.NewPlayer().SetVolume(0).Play()
}

// StartKeepWarm starts a background heartbeat that keeps the OS audio queue open
// across idle gaps (>5s) unless muted or closed.
func (e *Engine) StartKeepWarm() {
	e.warmOnce.Do(func() {
		e.warmActive.Store(true)
		e.WarmUp()
		go func() {
			ticker := time.NewTicker(keepWarmInterval)
			defer ticker.Stop()
			for {
				select {
				case <-e.stopCh:
					return
				case <-ticker.C:
					e.WarmUp()
				}
			}
		}()
	})
}

// Close stops the background audio keep-warm loop.
func (e *Engine) Close() {
	e.closed.Store(true)
	e.closeOnce.Do(func() {
		close(e.stopCh)
	})
}

func (e *Engine) SetMuted(m bool) {
	wasMuted := e.muted.Swap(m)
	if wasMuted && !m && e.warmActive.Load() {
		e.WarmUp()
	}
}

func (e *Engine) IsMuted() bool {
	return e.muted.Load()
}

// PlayBeep plays the directional WASD keystroke chime for step (1..8).
func (e *Engine) PlayBeep(step int) {
	if e.muted.Load() || e.closed.Load() {
		return
	}
	if step < 1 {
		step = 1
	} else if step > 8 {
		step = 8
	}
	if s := e.beeps[step-1]; s != nil {
		s.NewPlayer().SetVolume(0.78).Play()
	}
}

// PlayFail plays the negative terminal buzz when a wrong key resets the sequence.
func (e *Engine) PlayFail() {
	if e.muted.Load() || e.closed.Load() {
		return
	}
	idx := e.failToggle.Add(1) % 2
	if s := e.fails[idx]; s != nil {
		s.NewPlayer().SetVolume(0.85).Play()
	}
}

// PlayReady plays the short crisp code-completed confirmation chime (~0.2s).
func (e *Engine) PlayReady() {
	if e.muted.Load() || e.closed.Load() {
		return
	}
	if e.ready != nil {
		e.ready.NewPlayer().SetVolume(0.82).Play()
	}
}
