package sfx_test

import (
	"slices"
	"testing"
	"time"

	"hellauncher/internal/sfx"
)

func TestAudioSpeakerLifecycleAndCues(t *testing.T) {
	spk := sfx.NewAudioSpeaker()
	defer spk.Close()

	spk.SetMuted(true)
	spk.Play(sfx.CueOpen)
	spk.Play(sfx.CueClose)
	spk.Play(sfx.StepCue(1))
	spk.Play(sfx.CueFail)
	spk.Play(sfx.CueArmed)
	spk.Play(sfx.CueReady)
	spk.Play(sfx.CueHellpodLaunch)
	spk.Play(sfx.CueOnboardingIntro)
	spk.Play(sfx.CueJoinHelldiversVO)
	spk.Play(sfx.CueMissionComplete)
	spk.StopMusic(120 * time.Millisecond)

	spk.Close()
	// Calls after Close must be safe no-ops.
	spk.SetMuted(false)
	spk.Play(sfx.CueOpen)
	spk.Play(sfx.CueClose)
	spk.Play(sfx.StepCue(3))
	spk.Play(sfx.CueHellpodLaunch)
	spk.StopMusic(80 * time.Millisecond)
}

func TestRecordSpeakerRespectsMuteAndClose(t *testing.T) {
	rec := sfx.NewRecordSpeaker()
	rec.Play(sfx.CueOpen)
	rec.Play(sfx.StepCue(1))
	rec.Play(sfx.StepCue(2))
	rec.Play(sfx.CueClose)
	rec.Play(sfx.CueHellpodLaunch)
	rec.StopMusic(450 * time.Millisecond)

	rec.SetMuted(true)
	rec.Play(sfx.CueFail)

	rec.SetMuted(false)
	rec.Play(sfx.CueReady)
	rec.Close()
	rec.Play(sfx.StepCue(8))
	rec.StopMusic(200 * time.Millisecond)

	want := []sfx.Cue{sfx.CueOpen, sfx.CueStep1, sfx.CueStep2, sfx.CueClose, sfx.CueHellpodLaunch, sfx.CueReady}
	if !slices.Equal(rec.Cues(), want) {
		t.Fatalf("got cues %v, want %v", rec.Cues(), want)
	}
	wantFades := []time.Duration{450 * time.Millisecond}
	if !slices.Equal(rec.StopFades(), wantFades) {
		t.Fatalf("got stop fades %v, want %v", rec.StopFades(), wantFades)
	}
}
