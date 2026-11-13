package sfx

import (
	"bytes"
	"embed"
	"fmt"
	"io"
	"log/slog"
	"math"
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
	CueClose
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
	openSnd    *audio.Sound
	closeSnd   *audio.Sound
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

// NewAudioSpeaker decodes all embedded Helldivers 2 OGG cues into PCM memory
// and starts zero-volume hardware keep-warm.
func NewAudioSpeaker() *AudioSpeaker {
	audio.SetLatency(audio.LatencyInteractive)

	s := &AudioSpeaker{
		openSnd:  decodeTrimmedSound("sounds/stratagem-menu-open.ogg"),
		closeSnd: decodeTrimmedSound("sounds/stratagem-menu-close.ogg"),
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

// decodeTrimmedSound decodes an embedded OGG asset and strips leading digital
// silence (<0.002 amplitude, preserving a 1ms pre-transient guard) so wrist-pad
// open/close game assets fire with immediate sub-millisecond attack latency.
func decodeTrimmedSound(path string) *audio.Sound {
	data, err := soundsFS.ReadFile(path)
	if err != nil {
		slog.Warn("sfx read embedded sound failed", "path", path, "err", err)
		return nil
	}
	dec, _, err := audio.NewDecoder(bytes.NewReader(data))
	if err != nil {
		slog.Warn("sfx decode sound failed", "path", path, "err", err)
		return nil
	}
	sr := dec.SampleRate()
	ch := dec.Channels()
	if ch < 1 || sr <= 0 {
		return nil
	}
	keep := min(ch, 2)
	var samples []float32
	buf := make([]float32, 4096/ch*ch)
	for {
		n, rErr := dec.Read(buf)
		n -= n % ch
		for i := 0; i < n; i += ch {
			samples = append(samples, buf[i:i+keep]...)
		}
		if rErr == io.EOF {
			break
		}
		if rErr != nil || n == 0 {
			slog.Warn("sfx read decoded pcm failed", "path", path, "err", rErr)
			return nil
		}
	}
	frames := len(samples) / keep
	if frames == 0 {
		return nil
	}
	guardFrames := max(sr/1000, 1)
	startFrame := 0
	for i := 0; i < frames; i++ {
		var peak float64
		for c := 0; c < keep; c++ {
			v := math.Abs(float64(samples[i*keep+c]))
			if v > peak {
				peak = v
			}
		}
		if peak >= 0.002 {
			if i > guardFrames {
				startFrame = i - guardFrames
			}
			break
		}
	}
	return audio.NewSound(sr, keep, samples[startFrame*keep:])
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
		if s.openSnd != nil {
			// Normalize raw Wwise -20 dBFS export (peak 0.099) to match input beep peak (~0.24).
			s.openSnd.NewPlayer().SetVolume(2.45).Play()
		}
	case cue == CueClose:
		if s.closeSnd != nil {
			// Normalize raw Wwise -25 dBFS export (peak 0.057) to balanced stow peak (~0.20).
			s.closeSnd.NewPlayer().SetVolume(3.50).Play()
		}
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
