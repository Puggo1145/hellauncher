package tacticalaudio

import "testing"

func TestAllEmbeddedOggSoundsDecode(t *testing.T) {
	eng := New()
	defer eng.Close()

	for i, s := range eng.beeps {
		if s == nil {
			t.Fatalf("expected beep-%d.ogg to decode cleanly, got nil", i+1)
		}
	}
	for i, s := range eng.fails {
		if s == nil {
			t.Fatalf("expected fail-%d.ogg to decode cleanly, got nil", i+1)
		}
	}
	if eng.ready == nil {
		t.Fatal("expected ready.ogg to decode cleanly, got nil")
	}
	if eng.keepWarm == nil {
		t.Fatal("expected keepWarm buffer to be initialized")
	}
}

func TestKeepWarmLifecycleWhenMuted(t *testing.T) {
	eng := New()
	eng.SetMuted(true)
	eng.StartKeepWarm()
	eng.WarmUp()
	eng.Close()
}
