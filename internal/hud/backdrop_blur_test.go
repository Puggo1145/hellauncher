package hud

import "testing"

func TestDownsampleAndBlurBGRA(t *testing.T) {
	const capW, capH = 80, 60
	bgra := make([]byte, capW*capH*4)

	// Fill left half with dark blue-tinted BGRA (B=40, G=20, R=10)
	// and right half with bright warm white BGRA (B=210, G=240, R=250).
	for y := range capH {
		for x := range capW {
			idx := (y*capW + x) * 4
			if x < capW/2 {
				bgra[idx+0] = 40
				bgra[idx+1] = 20
				bgra[idx+2] = 10
				bgra[idx+3] = 0 // GDI BitBlt often leaves alpha=0; blur must force opaque 255
			} else {
				bgra[idx+0] = 210
				bgra[idx+1] = 240
				bgra[idx+2] = 250
				bgra[idx+3] = 0
			}
		}
	}

	out := downsampleAndBlurBGRA(bgra, capW, capH, 1.0)
	if out == nil {
		t.Fatal("expected non-nil blurred RGBA image")
	}
	if gotW, gotH := out.Bounds().Dx(), out.Bounds().Dy(); gotW != capW/2 || gotH != capH/2 {
		t.Fatalf("expected 2x downsampled size %dx%d, got %dx%d", capW/2, capH/2, gotW, gotH)
	}

	farLeft := out.RGBAAt(1, capH/4)
	seam := out.RGBAAt(capW/4, capH/4)
	farRight := out.RGBAAt(capW/2-2, capH/4)

	// Verify BGRA -> RGBA channel swap and forced opaque alpha=255
	if farLeft.A != 255 || seam.A != 255 || farRight.A != 255 {
		t.Fatalf("expected opaque alpha=255 everywhere, got %d/%d/%d", farLeft.A, seam.A, farRight.A)
	}
	if farLeft.B <= farLeft.R {
		t.Fatalf("expected BGRA->RGBA swap on left (B=40 > R=10), got RGBA=%+v", farLeft)
	}
	if farRight.R <= farRight.B {
		t.Fatalf("expected BGRA->RGBA swap on right (R=250 > B=210), got RGBA=%+v", farRight)
	}

	// Verify Gaussian-approximated box blur smoothed the hard vertical step into a gradient
	if !(farLeft.R < seam.R && seam.R < farRight.R && seam.R > 60 && seam.R < 200) {
		t.Fatalf("expected smooth blurred gradient across seam, got left.R=%d seam.R=%d right.R=%d", farLeft.R, seam.R, farRight.R)
	}
}
