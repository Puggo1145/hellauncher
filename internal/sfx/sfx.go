package sfx

import (
	"embed"
	"fmt"
	"log/slog"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"github.com/egoist/mygo/audio"
	_ "github.com/egoist/mygo/audio/vorbis"
)

//go:embed sounds/*.ogg
var soundsFS embed.FS

// Cue identifies a semantic wrist-terminal sound effect.
type Cue uint8

const (
	CueOpen Cue = iota
	CueStep1
	CueStep2
	CueStep3
	CueStep4
	CueStep5
	CueStep6
	CueStep7
	CueStep8
	CueFail
	CueArmed
	CueReady
)

// StepCue maps a 1-based sequence step index to CueStep1..CueStep8.
func StepCue(step int) Cue {
	if step <= 1 {
		return CueStep1
	}
	if step >= 8 {
		return CueStep8
	}
	return CueStep1 + Cue(step-1)
}

// Speaker is the narrow tactical audio output Seam.
type Speaker interface {
	Play(cue Cue)
	SetMuted(muted bool)
	Close()
}

// keepWarmInterval stays safely below mygo/audio's 5s idleClose window so
// CoreAudio / WASAPI stays warm for <5ms first-keystroke playback.
const keepWarmInterval = 3200 * time.Millisecond

// AudioSpeaker is the production Speaker Adapter backed by mygo/audio.
type AudioSpeaker struct {
	beeps      [8]*audio.Sound
	fails      [2]*audio.Sound
	ready      *audio.Sound
	keepWarm   *audio.Sound
	muted      atomic.Bool
	closed     atomic.Bool
	failToggle atomic.Uint32

	closeOnce sync.Once
	stopCh    chan struct{}
}

// NewAudioSpeaker decodes all embedded OGG cues into PCM memory and starts
// autonomous zero-volume hardware keep-warm management.
func NewAudioSpeaker() *AudioSpeaker {
	audio.SetLatency(audio.LatencyInteractive)

	s := &AudioSpeaker{
		keepWarm: audio.NewSound(48000, 1, make([]float32, 64)),
		stopCh:   make(chan struct{}),
	}
	for i := 0; i < 8; i++ {
		name := fmt.Sprintf("sounds/stratagem-input-beep-%d.ogg", i+1)
		s.beeps[i] = decodeEmbeddedSound(name)
	}
	s.fails[0] = decodeEmbeddedSound("sounds/stratagem-input-fail-1.ogg")
	s.fails[1] = decodeEmbeddedSound("sounds/stratagem-input-fail-2.ogg")
	s.ready = decodeEmbeddedSound("sounds/stratagem-input-ready.ogg")

	s.pulseWarm()
	go s.keepWarmLoop()
	return s
}

func decodeEmbeddedSound(path string) *audio.Sound {
	data, err := soundsFS.ReadFile(path)
	if err != nil {
		slog.Warn("sfx read embedded sound failed", "path", path, "err", err)
		return nil
	}
	snd, err := audio.DecodeSound(data)
	if err != nil {
		slog.Warn("sfx decode sound failed", "path", path, "err", err)
		return nil
	}
	return snd
}

func (s *AudioSpeaker) pulseWarm() {
	if s.muted.Load() || s.closed.Load() || s.keepWarm == nil {
		return
	}
	s.keepWarm.NewPlayer().SetVolume(0).Play()
}

func (s *AudioSpeaker) keepWarmLoop() {
	ticker := time.NewTicker(keepWarmInterval)
	defer ticker.Stop()
	for {
		select {
		case <-s.stopCh:
			return
		case <-ticker.C:
			s.pulseWarm()
		}
	}
}

func (s *AudioSpeaker) Play(cue Cue) {
	if s.muted.Load() || s.closed.Load() {
		return
	}
	switch {
	case cue == CueOpen:
		s.pulseWarm()
	case cue >= CueStep1 && cue <= CueStep8:
		idx := int(cue - CueStep1)
		if snd := s.beeps[idx]; snd != nil {
			snd.NewPlayer().SetVolume(0.78).Play()
		}
	case cue == CueFail:
		idx := s.failToggle.Add(1) % 2
		if snd := s.fails[idx]; snd != nil {
			snd.NewPlayer().SetVolume(0.85).Play()
		}
	case cue == CueArmed:
		if s.ready != nil {
			s.ready.NewPlayer().SetVolume(0.72).Play()
		}
	case cue == CueReady:
		if s.ready != nil {
			s.ready.NewPlayer().SetVolume(0.84).Play()
		}
	}
}

func (s *AudioSpeaker) SetMuted(muted bool) {
	wasMuted := s.muted.Swap(muted)
	if wasMuted && !muted {
		s.pulseWarm()
	}
}

func (s *AudioSpeaker) Close() {
	s.closed.Store(true)
	s.closeOnce.Do(func() {
		close(s.stopCh)
	})
}

// RecordSpeaker is an in-memory Speaker Adapter for deterministic unit tests.
type RecordSpeaker struct {
	mu     sync.Mutex
	muted  bool
	closed bool
	cues   []Cue
}

func NewRecordSpeaker() *RecordSpeaker {
	return &RecordSpeaker{}
}

func (r *RecordSpeaker) Play(cue Cue) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.muted || r.closed {
		return
	}
	r.cues = append(r.cues, cue)
}

func (r *RecordSpeaker) SetMuted(muted bool) {
	r.mu.Lock()
	r.muted = muted
	r.mu.Unlock()
}

func (r *RecordSpeaker) Close() {
	r.mu.Lock()
	r.closed = true
	r.mu.Unlock()
}

func (r *RecordSpeaker) Cues() []Cue {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.cues)
}

func (r *RecordSpeaker) Muted() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.muted
}

func (r *RecordSpeaker) Reset() {
	r.mu.Lock()
	r.cues = nil
	r.mu.Unlock()
}
