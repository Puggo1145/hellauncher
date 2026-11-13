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

	if spk.ready == nil || spk.ready.Duration() < 500*time.Millisecond {
		t.Fatalf("ready sound failed to decode or too short: %v", spk.ready)
	}
}
