//go:build windows

package hud

import (
	"testing"
	"unsafe"
)

func TestCaptureFromMemoryDCWindows(t *testing.T) {
	hdcScreen, _, err := procGetDC.Call(0)
	if hdcScreen == 0 {
		t.Fatalf("GetDC(0) failed: %v", err)
	}
	defer procReleaseDC.Call(0, hdcScreen)

	hdcSrc, _, err := procCreateCompatibleDC.Call(hdcScreen)
	if hdcSrc == 0 {
		t.Fatalf("CreateCompatibleDC failed: %v", err)
	}
	defer procDeleteDC.Call(hdcSrc)

	const srcW, srcH = 200, 160
	hbmpSrc, _, err := procCreateCompatibleBitmap.Call(hdcScreen, srcW, srcH)
	if hbmpSrc == 0 {
		t.Fatalf("CreateCompatibleBitmap failed: %v", err)
	}
	defer procDeleteObject.Call(hbmpSrc)

	// Populate hbmpSrc via SetDIBits with a left dark-blue half and right bright-white half.
	srcBGRA := make([]byte, srcW*srcH*4)
	for y := range srcH {
		for x := range srcW {
			i := (y*srcW + x) * 4
			if x < srcW/2 {
				srcBGRA[i+0] = 60 // B
				srcBGRA[i+1] = 30 // G
				srcBGRA[i+2] = 20 // R
			} else {
				srcBGRA[i+0] = 240 // B
				srcBGRA[i+1] = 246 // G
				srcBGRA[i+2] = 250 // R
			}
		}
	}
	bmi := bitmapInfo{
		Header: bitmapInfoHeader{
			Size:     uint32(unsafe.Sizeof(bitmapInfoHeader{})),
			Width:    srcW,
			Height:   -srcH,
			Planes:   1,
			BitCount: 32,
		},
	}
	procSetDIBits := hudGdi32.NewProc("SetDIBits")
	setLines, _, err := procSetDIBits.Call(
		hdcSrc,
		hbmpSrc,
		0,
		srcH,
		uintptr(unsafe.Pointer(&srcBGRA[0])),
		uintptr(unsafe.Pointer(&bmi)),
		0,
	)
	if setLines == 0 {
		t.Fatalf("SetDIBits failed: %v", err)
	}

	oldSrc, _, _ := procSelectObject.Call(hdcSrc, hbmpSrc)
	defer procSelectObject.Call(hdcSrc, oldSrc)

	blurred := captureFromDC(hdcSrc, 0, 0, srcW, srcH, 1.25)
	if blurred == nil {
		t.Fatal("captureFromDC returned nil")
	}
	if blurred.Bounds().Dx() != srcW/2 || blurred.Bounds().Dy() != srcH/2 {
		t.Fatalf("expected %dx%d, got %v", srcW/2, srcH/2, blurred.Bounds())
	}

	left := blurred.RGBAAt(2, srcH/4)
	mid := blurred.RGBAAt(srcW/4, srcH/4)
	right := blurred.RGBAAt(srcW/2-3, srcH/4)
	if !(left.R < mid.R && mid.R < right.R && left.A == 255 && right.A == 255) {
		t.Fatalf("unexpected blurred GDI output: left=%+v mid=%+v right=%+v", left, mid, right)
	}
}
