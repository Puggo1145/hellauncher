package sfx_test

import (
	"slices"
	"testing"

	"helldriver/internal/sfx"
)

func TestAudioSpeakerLifecycleAndCues(t *testing.T) {
	spk := sfx.NewAudioSpeaker()
	defer spk.Close()

	spk.SetMuted(true)
	spk.Play(sfx.CueOpen)
	spk.Play(sfx.StepCue(1))
	spk.Play(sfx.CueFail)
	spk.Play(sfx.CueArmed)
	spk.Play(sfx.CueReady)

	spk.Close()
	// Calls after Close must be safe no-ops.
	spk.SetMuted(false)
	spk.Play(sfx.StepCue(3))
}

func TestRecordSpeakerRespectsMuteAndClose(t *testing.T) {
	rec := sfx.NewRecordSpeaker()
	rec.Play(sfx.CueOpen)
	rec.Play(sfx.StepCue(1))
	rec.Play(sfx.StepCue(2))

	rec.SetMuted(true)
	rec.Play(sfx.CueFail)

	rec.SetMuted(false)
	rec.Play(sfx.CueReady)
	rec.Close()
	rec.Play(sfx.StepCue(8))

	want := []sfx.Cue{sfx.CueOpen, sfx.CueStep1, sfx.CueStep2, sfx.CueReady}
	if !slices.Equal(rec.Cues(), want) {
		t.Fatalf("got cues %v, want %v", rec.Cues(), want)
	}
}
