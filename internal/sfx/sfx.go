package sfx

import (
	"embed"
	"fmt"
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

// NewAudioSpeaker decodes all embedded OGG cues plus wrist-terminal open/close
// telemetry chimes into PCM memory and starts zero-volume hardware keep-warm.
func NewAudioSpeaker() *AudioSpeaker {
	audio.SetLatency(audio.LatencyInteractive)

	s := &AudioSpeaker{
		openSnd:  synthWristOpenSound(),
		closeSnd: synthWristCloseSound(),
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
		if s.openSnd != nil {
			s.openSnd.NewPlayer().SetVolume(0.76).Play()
		}
	case cue == CueClose:
		if s.closeSnd != nil {
			s.closeSnd.NewPlayer().SetVolume(0.72).Play()
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

// synthWristOpenSound generates a 48kHz stereo tactical wrist-pad activation
// chime (tactile micro-switch detent + ascending two-note crystal telemetry
// lock matched to the 2.6kHz harmonic profile of stratagem-input-beep-*.ogg).
func synthWristOpenSound() *audio.Sound {
	const (
		sr     = 48000
		durSec = 0.098
		frames = int(sr * durSec)
	)
	pcm := make([]float32, frames*2)
	var p1, p2 float64
	for i := 0; i < frames; i++ {
		t := float64(i) / sr
		var sample float64

		// Crisp tactile wrist-latch click transient (0..7ms)
		if t < 0.007 {
			env := math.Exp(-t / 0.0016)
			sample += 0.14 * env * (math.Sin(2*math.Pi*2150*t) + 0.5*math.Sin(2*math.Pi*3400*t))
		}

		// Note 1 (3..44ms): 880 Hz telemetry wake tone
		if t >= 0.003 && t < 0.048 {
			lt := t - 0.003
			att := smoothAttack(lt, 0.0025)
			dec := math.Exp(-lt / 0.020)
			p1 += 2 * math.Pi * 880.0 / sr
			tone := 0.66*math.Sin(p1) + 0.24*math.Sin(2*p1) + 0.10*math.Sin(3*p1)
			sample += 0.22 * att * dec * tone
		}

		// Note 2 (36..98ms): ascending 1320 Hz ready-for-code harmonic lock
		if t >= 0.036 {
			lt := t - 0.036
			freq := 1320.0 - 40.0*math.Exp(-lt/0.006)
			att := smoothAttack(lt, 0.0025)
			dec := math.Exp(-lt / 0.023)
			fadeOut := smoothTail(t, durSec, 0.008)
			p2 += 2 * math.Pi * freq / sr
			tone := 0.62*math.Sin(p2) + 0.27*math.Sin(2*p2) + 0.11*math.Sin(3*p2)
			sample += 0.26 * att * dec * fadeOut * tone
		}

		writeStereo(pcm, i, sample)
	}
	return audio.NewSound(sr, 2, pcm)
}

// synthWristCloseSound generates the inverse 48kHz stereo tactical wrist-pad
// power-down / stow chime (1175 Hz -> 740->600 Hz descending lock-off).
func synthWristCloseSound() *audio.Sound {
	const (
		sr     = 48000
		durSec = 0.088
		frames = int(sr * durSec)
	)
	pcm := make([]float32, frames*2)
	var p1, p2 float64
	for i := 0; i < frames; i++ {
		t := float64(i) / sr
		var sample float64

		// Note 1 (0..40ms): high-mid release chime
		if t < 0.042 {
			att := smoothAttack(t, 0.0022)
			dec := math.Exp(-t / 0.017)
			p1 += 2 * math.Pi * 1175.0 / sr
			tone := 0.68*math.Sin(p1) + 0.24*math.Sin(2*p1) + 0.08*math.Sin(3*p1)
			sample += 0.21 * att * dec * tone
		}

		// Subtle latch release detent + descending power-down (30..88ms)
		if t >= 0.030 {
			lt := t - 0.030
			if lt < 0.006 {
				sample += 0.08 * math.Exp(-lt/0.0015) * math.Sin(2*math.Pi*1650*lt)
			}
			freq := 610.0 + 170.0*math.Exp(-lt/0.024)
			att := smoothAttack(lt, 0.0025)
			dec := math.Exp(-lt / 0.021)
			fadeOut := smoothTail(t, durSec, 0.008)
			p2 += 2 * math.Pi * freq / sr
			tone := 0.74*math.Sin(p2) + 0.20*math.Sin(2*p2) + 0.06*math.Sin(3*p2)
			sample += 0.23 * att * dec * fadeOut * tone
		}

		writeStereo(pcm, i, sample)
	}
	return audio.NewSound(sr, 2, pcm)
}

func smoothAttack(t, rise float64) float64 {
	if t <= 0 {
		return 0
	}
	if t >= rise {
		return 1
	}
	return 0.5 - 0.5*math.Cos(math.Pi*(t/rise))
}

func smoothTail(t, total, tail float64) float64 {
	rem := total - t
	if rem <= 0 {
		return 0
	}
	if rem >= tail {
		return 1
	}
	return 0.5 - 0.5*math.Cos(math.Pi*(rem/tail))
}

func writeStereo(pcm []float32, frame int, v float64) {
	if v > 0.95 {
		v = 0.95
	} else if v < -0.95 {
		v = -0.95
	}
	// Slight 8-sample Haas stereo offset adds subtle helmet-visor spatial width
	pcm[frame*2] = float32(v)
	rIdx := frame + 8
	if rIdx*2+1 < len(pcm) {
		pcm[rIdx*2+1] = float32(v * 0.96)
	} else {
		pcm[frame*2+1] = float32(v * 0.96)
	}
}
