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
	_ "github.com/egoist/mygo/audio/mp3"
	_ "github.com/egoist/mygo/audio/vorbis"
)

//go:embed sounds/*.ogg sounds/*.mp3
var soundsFS embed.FS

// Cue identifies a semantic wrist-terminal sound effect or musical sequence.
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
	CueHellpodLaunch
	CueOnboardingIntro
	CueJoinHelldiversVO
	CueMissionComplete
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
	StopMusic(fadeDur time.Duration)
	SetMuted(muted bool)
	Close()
}

// keepWarmInterval stays safely below mygo/audio's 5s idleClose window so
// CoreAudio / WASAPI stays warm for <5ms first-keystroke playback.
const keepWarmInterval = 3200 * time.Millisecond

type playerVol struct {
	player *audio.Player
	vol    float64
}

type musicTrack struct {
	mu        sync.Mutex
	cancelled bool
	cancelCh  chan struct{}
	players   map[*audio.Player]float64
}

func newMusicTrack() *musicTrack {
	return &musicTrack{
		cancelCh: make(chan struct{}),
		players:  make(map[*audio.Player]float64, 2),
	}
}

func (t *musicTrack) attachAndPlay(snd *audio.Sound, vol float64) *audio.Player {
	if snd == nil {
		return nil
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.cancelled {
		return nil
	}
	p := snd.NewPlayer().SetVolume(vol)
	t.players[p] = vol
	p.Play()
	return p
}

func (t *musicTrack) setVolume(p *audio.Player, vol float64) bool {
	if p == nil {
		return false
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.cancelled {
		return false
	}
	t.players[p] = vol
	p.SetVolume(vol)
	return true
}

func (t *musicTrack) detachAndStop(p *audio.Player) {
	if p == nil {
		return
	}
	t.mu.Lock()
	cancelled := t.cancelled
	if !cancelled {
		delete(t.players, p)
	}
	t.mu.Unlock()
	if !cancelled {
		p.SetVolume(0)
		p.Stop()
	}
}

func (t *musicTrack) cancel() []playerVol {
	t.mu.Lock()
	if t.cancelled {
		t.mu.Unlock()
		return nil
	}
	t.cancelled = true
	close(t.cancelCh)
	snap := make([]playerVol, 0, len(t.players))
	for p, v := range t.players {
		snap = append(snap, playerVol{player: p, vol: v})
	}
	t.players = nil
	t.mu.Unlock()
	return snap
}

// AudioSpeaker is the production Speaker Adapter backed by mygo/audio.
type AudioSpeaker struct {
	openSnd            *audio.Sound
	closeSnd           *audio.Sound
	beeps              [8]*audio.Sound
	fails              [2]*audio.Sound
	hellpodLockSnd     *audio.Sound
	hellpodDropSnd     *audio.Sound
	onboardingIntroSnd *audio.Sound
	joinHelldiversSnd  *audio.Sound
	missionCompleteSnd *audio.Sound
	keepWarm           *audio.Sound

	muted      atomic.Bool
	closed     atomic.Bool
	failToggle atomic.Uint32

	musicMu     sync.Mutex
	activeTrack *musicTrack
	fadingMu    sync.Mutex
	fadingSet   map[*audio.Player]struct{}

	closeOnce sync.Once
	stopCh    chan struct{}
}

// NewAudioSpeaker decodes embedded Helldivers 2 OGG/MP3 cues into PCM memory
// and starts zero-volume hardware keep-warm.
func NewAudioSpeaker() *AudioSpeaker {
	audio.SetLatency(audio.LatencyInteractive)

	s := &AudioSpeaker{
		openSnd:            decodeTrimmedSound("sounds/stratagem-menu-open.ogg"),
		closeSnd:           decodeTrimmedSound("sounds/stratagem-menu-close.ogg"),
		hellpodLockSnd:     decodeTrimmedSound("sounds/hellpod-lock-in.mp3"),
		hellpodDropSnd:     decodeSoundWindow("sounds/hellpod-drop.mp3", 18500*time.Millisecond),
		onboardingIntroSnd: decodeSoundWindow("sounds/onboarding-intro.mp3", 17200*time.Millisecond),
		joinHelldiversSnd:  decodeTrimmedSound("sounds/join-helldivers.mp3"),
		missionCompleteSnd: decodeTrimmedSound("sounds/mission-complete.mp3"),
		keepWarm:           audio.NewSound(48000, 1, make([]float32, 64)),
		fadingSet:          make(map[*audio.Player]struct{}),
		stopCh:             make(chan struct{}),
	}
	for i := 0; i < 8; i++ {
		name := fmt.Sprintf("sounds/stratagem-input-beep-%d.ogg", i+1)
		s.beeps[i] = decodeEmbeddedSound(name)
	}
	s.fails[0] = decodeEmbeddedSound("sounds/stratagem-input-fail-1.ogg")
	s.fails[1] = decodeEmbeddedSound("sounds/stratagem-input-fail-2.ogg")

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

// decodeSoundWindow decodes at most maxDur of an embedded audio asset into PCM
// memory so long orchestral themes keep startup decode fast and RAM bounded.
func decodeSoundWindow(path string, maxDur time.Duration) *audio.Sound {
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
	maxFrames := int(float64(sr) * maxDur.Seconds())
	if maxFrames <= 0 {
		return nil
	}
	samples := make([]float32, 0, maxFrames*keep)
	buf := make([]float32, 4096/ch*ch)
	framesRead := 0
	for framesRead < maxFrames {
		n, rErr := dec.Read(buf)
		n -= n % ch
		for i := 0; i < n && framesRead < maxFrames; i += ch {
			samples = append(samples, buf[i:i+keep]...)
			framesRead++
		}
		if rErr == io.EOF {
			break
		}
		if rErr != nil || n == 0 {
			slog.Warn("sfx read decoded pcm failed", "path", path, "err", rErr)
			break
		}
	}
	if len(samples) == 0 {
		return nil
	}
	return audio.NewSound(sr, keep, samples)
}

// decodeTrimmedSound decodes an embedded OGG/MP3 asset and strips leading digital
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

func (s *AudioSpeaker) beginMusicTrack() *musicTrack {
	s.StopMusic(0)
	track := newMusicTrack()
	s.musicMu.Lock()
	s.activeTrack = track
	s.musicMu.Unlock()
	return track
}

func (s *AudioSpeaker) ensureMusicTrack() *musicTrack {
	s.musicMu.Lock()
	defer s.musicMu.Unlock()
	if s.activeTrack == nil {
		s.activeTrack = newMusicTrack()
	}
	return s.activeTrack
}

func (s *AudioSpeaker) runScheduledMusic(
	track *musicTrack,
	delay time.Duration,
	snd *audio.Sound,
	baseVol float64,
	holdDur time.Duration,
	fadeDur time.Duration,
) {
	if snd == nil {
		return
	}
	if delay > 0 {
		timer := time.NewTimer(delay)
		select {
		case <-s.stopCh:
			timer.Stop()
			return
		case <-track.cancelCh:
			timer.Stop()
			return
		case <-timer.C:
		}
	}
	if s.muted.Load() || s.closed.Load() {
		return
	}
	p := track.attachAndPlay(snd, baseVol)
	if p == nil {
		return
	}
	if holdDur > 0 {
		timer := time.NewTimer(holdDur)
		select {
		case <-s.stopCh:
			timer.Stop()
			p.Stop()
			return
		case <-track.cancelCh:
			timer.Stop()
			return
		case <-timer.C:
		}
	}
	if fadeDur <= 0 {
		track.detachAndStop(p)
		return
	}

	const tickStep = 40 * time.Millisecond
	ticker := time.NewTicker(tickStep)
	defer ticker.Stop()
	started := time.Now()
	for {
		select {
		case <-s.stopCh:
			p.Stop()
			return
		case <-track.cancelCh:
			return
		case <-ticker.C:
			if s.muted.Load() || s.closed.Load() {
				track.detachAndStop(p)
				return
			}
			prog := float64(time.Since(started)) / float64(fadeDur)
			if prog >= 1 {
				track.detachAndStop(p)
				s.musicMu.Lock()
				if s.activeTrack == track {
					s.activeTrack = nil
				}
				s.musicMu.Unlock()
				return
			}
			rem := 1 - prog
			if !track.setVolume(p, baseVol*rem*rem) {
				return
			}
		}
	}
}

// StopMusic halts or smoothly fades out any active Hellpod / Onboarding music cues.
func (s *AudioSpeaker) StopMusic(fadeDur time.Duration) {
	s.musicMu.Lock()
	track := s.activeTrack
	s.activeTrack = nil
	s.musicMu.Unlock()

	var snap []playerVol
	if track != nil {
		snap = track.cancel()
	}

	if fadeDur <= 0 || s.muted.Load() || s.closed.Load() {
		for _, pv := range snap {
			pv.player.SetVolume(0)
			pv.player.Stop()
		}
		s.fadingMu.Lock()
		for p := range s.fadingSet {
			p.SetVolume(0)
			p.Stop()
			delete(s.fadingSet, p)
		}
		s.fadingMu.Unlock()
		return
	}

	if len(snap) == 0 {
		return
	}

	s.fadingMu.Lock()
	for _, pv := range snap {
		s.fadingSet[pv.player] = struct{}{}
	}
	s.fadingMu.Unlock()

	go func(items []playerVol) {
		const step = 30 * time.Millisecond
		ticker := time.NewTicker(step)
		defer ticker.Stop()
		started := time.Now()
		cleanup := func() {
			s.fadingMu.Lock()
			for _, pv := range items {
				pv.player.SetVolume(0)
				pv.player.Stop()
				delete(s.fadingSet, pv.player)
			}
			s.fadingMu.Unlock()
		}
		for {
			select {
			case <-s.stopCh:
				cleanup()
				return
			case <-ticker.C:
				if s.muted.Load() || s.closed.Load() {
					cleanup()
					return
				}
				prog := float64(time.Since(started)) / float64(fadeDur)
				if prog >= 1 {
					cleanup()
					return
				}
				rem := 1 - prog
				gain := rem * rem
				for _, pv := range items {
					pv.player.SetVolume(pv.vol * gain)
				}
			}
		}
	}(snap)
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
	case cue == CueHellpodLaunch:
		// All-players-ready Hellpod hydraulic lock-in followed by tube launch +
		// main brass fanfare (~11.15s bounded with smooth 2.8s fade-out).
		track := s.beginMusicTrack()
		track.attachAndPlay(s.hellpodLockSnd, 0.72)
		go s.runScheduledMusic(track, 550*time.Millisecond, s.hellpodDropSnd, 0.85, 7800*time.Millisecond, 2800*time.Millisecond)
	case cue == CueOnboardingIntro:
		// Ministry of Truth opening broadcast score (16.6s bounded with smooth 2.8s fade-out).
		intro := s.onboardingIntroSnd
		if intro == nil {
			intro = s.hellpodDropSnd
		}
		track := s.beginMusicTrack()
		go s.runScheduledMusic(track, 0, intro, 0.72, 13800*time.Millisecond, 2800*time.Millisecond)
	case cue == CueJoinHelldiversVO:
		// Announcer climax ("JOIN... THE HELLDIVERS!") layered onto active broadcast track so Skip fades both.
		track := s.ensureMusicTrack()
		track.attachAndPlay(s.joinHelldiversSnd, 0.84)
	case cue == CueMissionComplete:
		// Brasch Tactics live-fire drill completion fanfare.
		track := s.beginMusicTrack()
		go s.runScheduledMusic(track, 0, s.missionCompleteSnd, 0.85, 3600*time.Millisecond, 700*time.Millisecond)
	}
}

func (s *AudioSpeaker) SetMuted(muted bool) {
	wasMuted := s.muted.Swap(muted)
	if muted {
		s.StopMusic(0)
	} else if wasMuted {
		s.pulseWarm()
	}
}

func (s *AudioSpeaker) Close() {
	s.closed.Store(true)
	s.StopMusic(0)
	s.closeOnce.Do(func() {
		close(s.stopCh)
	})
}

// RecordSpeaker is an in-memory Speaker Adapter for deterministic unit tests.
type RecordSpeaker struct {
	mu        sync.Mutex
	muted     bool
	closed    bool
	cues      []Cue
	stopFades []time.Duration
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

func (r *RecordSpeaker) StopMusic(fadeDur time.Duration) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return
	}
	r.stopFades = append(r.stopFades, fadeDur)
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

func (r *RecordSpeaker) StopFades() []time.Duration {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.stopFades)
}

func (r *RecordSpeaker) Muted() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.muted
}

func (r *RecordSpeaker) Reset() {
	r.mu.Lock()
	r.cues = nil
	r.stopFades = nil
	r.mu.Unlock()
}
