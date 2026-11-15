package sfx

import (
	"fmt"
	"testing"
	"time"
)

func TestEmbeddedGameAssetsDecodeAndTrimCleanly(t *testing.T) {
	spk := NewAudioSpeaker()
	defer spk.Close()

	if spk.openSnd == nil {
		t.Fatal("openSnd failed to decode embedded stratagem-menu-open.ogg")
	}
	if d := spk.openSnd.Duration(); d < 250*time.Millisecond || d > 500*time.Millisecond {
		t.Fatalf("unexpected openSnd duration %v", d)
	}
	rawOpen := decodeEmbeddedSound("sounds/stratagem-menu-open.ogg")
	if rawOpen == nil {
		t.Fatal("raw stratagem-menu-open.ogg failed to decode")
	}
	if trimmed := rawOpen.Duration() - spk.openSnd.Duration(); trimmed <= 0 || trimmed > 50*time.Millisecond {
		t.Fatalf("expected leading dead-air trim within (0, 50ms] on stratagem-menu-open.ogg, got %v", trimmed)
	}

	if spk.closeSnd == nil {
		t.Fatal("closeSnd failed to decode embedded stratagem-menu-close.ogg")
	}
	if d := spk.closeSnd.Duration(); d < 200*time.Millisecond || d > 450*time.Millisecond {
		t.Fatalf("unexpected closeSnd duration %v", d)
	}

	for i, snd := range spk.beeps {
		if snd == nil {
			t.Fatalf("beeps[%d] failed to decode", i)
		}
		if d := snd.Duration(); d < 150*time.Millisecond {
			t.Fatalf("%s duration too short: %v", fmt.Sprintf("beep-%d", i+1), d)
		}
	}

	for i, snd := range spk.fails {
		if snd == nil {
			t.Fatalf("fails[%d] failed to decode", i)
		}
		if d := snd.Duration(); d < 150*time.Millisecond {
			t.Fatalf("fail-%d duration too short: %v", i+1, d)
		}
	}

	if spk.hellpodLockSnd == nil {
		t.Fatal("hellpodLockSnd failed to decode embedded hellpod-lock-in.mp3")
	}
	if d := spk.hellpodLockSnd.Duration(); d < 1500*time.Millisecond || d > 3500*time.Millisecond {
		t.Fatalf("unexpected hellpodLockSnd duration %v", d)
	}

	if spk.hellpodDropSnd == nil {
		t.Fatal("hellpodDropSnd failed to decode embedded hellpod-drop.mp3")
	}
	if d := spk.hellpodDropSnd.Duration(); d < 18*time.Second || d > 19*time.Second {
		t.Fatalf("expected hellpodDropSnd windowed decode ~18.5s, got %v", d)
	}

	if spk.onboardingIntroSnd == nil {
		t.Fatal("onboardingIntroSnd failed to decode embedded onboarding-intro.mp3")
	}
	if d := spk.onboardingIntroSnd.Duration(); d < 16*time.Second || d > 18*time.Second {
		t.Fatalf("expected onboardingIntroSnd windowed decode ~17.2s, got %v", d)
	}

	if spk.joinHelldiversSnd == nil {
		t.Fatal("joinHelldiversSnd failed to decode embedded join-helldivers.mp3")
	}
	if d := spk.joinHelldiversSnd.Duration(); d < 2*time.Second || d > 4*time.Second {
		t.Fatalf("unexpected joinHelldiversSnd duration %v", d)
	}

	if spk.missionCompleteSnd == nil {
		t.Fatal("missionCompleteSnd failed to decode embedded mission-complete.mp3")
	}
	if d := spk.missionCompleteSnd.Duration(); d < 3500*time.Millisecond || d > 5*time.Second {
		t.Fatalf("unexpected missionCompleteSnd duration %v", d)
	}
}
