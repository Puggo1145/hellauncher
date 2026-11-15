package hud

import (
	"image"
	"math"
)

// backdropCapture holds a pre-blurred desktop region captured right before the
// HUD overlay appears, along with its DIP geometry relative to the HUD window's
// client origin (0, 0).
type backdropCapture struct {
	img     *image.RGBA
	offsetX float32
	offsetY float32
	width   float32
	height  float32
}

// downsampleAndBlurBGRA converts a raw top-down 32-bit BGRA capture buffer
// (capW × capH) into a 2× downsampled, 3-pass Gaussian-approximated *image.RGBA
// suitable for silky GPU bilinear upsampling behind the frosted Classic HUD.
func downsampleAndBlurBGRA(bgra []byte, capW, capH int, dpiScale float32) *image.RGBA {
	if capW <= 0 || capH <= 0 || len(bgra) < capW*capH*4 {
		return nil
	}
	if dpiScale <= 0 {
		dpiScale = 1
	}

	dstW := max(capW/2, 1)
	dstH := max(capH/2, 1)
	img := image.NewRGBA(image.Rect(0, 0, dstW, dstH))
	pix := img.Pix

	// 2×2 box-filtered downsample + BGRA -> opaque RGBA conversion.
	if capW >= 2 && capH >= 2 {
		srcStride := capW * 4
		for y := range dstH {
			sy0 := y * 2
			sy1 := min(sy0+1, capH-1)
			row0 := sy0 * srcStride
			row1 := sy1 * srcStride
			dstRow := y * dstW * 4
			for x := range dstW {
				sx0 := x * 2 * 4
				sx1 := min(x*2+1, capW-1) * 4

				b := (uint32(bgra[row0+sx0+0]) + uint32(bgra[row0+sx1+0]) + uint32(bgra[row1+sx0+0]) + uint32(bgra[row1+sx1+0]) + 2) >> 2
				g := (uint32(bgra[row0+sx0+1]) + uint32(bgra[row0+sx1+1]) + uint32(bgra[row1+sx0+1]) + uint32(bgra[row1+sx1+1]) + 2) >> 2
				r := (uint32(bgra[row0+sx0+2]) + uint32(bgra[row0+sx1+2]) + uint32(bgra[row1+sx0+2]) + uint32(bgra[row1+sx1+2]) + 2) >> 2

				di := dstRow + x*4
				pix[di+0] = uint8(r)
				pix[di+1] = uint8(g)
				pix[di+2] = uint8(b)
				pix[di+3] = 255
			}
		}
	} else {
		for i := 0; i < dstW*dstH; i++ {
			pix[i*4+0] = bgra[i*4+2]
			pix[i*4+1] = bgra[i*4+1]
			pix[i*4+2] = bgra[i*4+0]
			pix[i*4+3] = 255
		}
	}

	// 3 passes of separable horizontal + vertical box blur (~12 DIPs Gaussian sigma).
	radius := max(int(math.Round(float64(5.5*dpiScale))), 2)
	blurRGBAInPlace(pix, dstW, dstH, radius, 3)
	return img
}

func blurRGBAInPlace(pix []byte, w, h, radius, passes int) {
	if w <= 1 && h <= 1 || radius <= 0 || passes <= 0 {
		return
	}
	tmp := make([]byte, len(pix))
	for range passes {
		boxBlurH(pix, tmp, w, h, radius)
		boxBlurV(tmp, pix, w, h, radius)
	}
}

func boxBlurH(src, dst []byte, w, h, r int) {
	win := uint32(2*r + 1)
	half := win >> 1
	for y := range h {
		row := y * w * 4
		var sumR, sumG, sumB uint32
		for k := -r; k <= r; k++ {
			cx := min(max(k, 0), w-1)
			si := row + cx*4
			sumR += uint32(src[si+0])
			sumG += uint32(src[si+1])
			sumB += uint32(src[si+2])
		}
		for x := range w {
			di := row + x*4
			dst[di+0] = uint8((sumR + half) / win)
			dst[di+1] = uint8((sumG + half) / win)
			dst[di+2] = uint8((sumB + half) / win)
			dst[di+3] = 255

			leftX := max(x-r, 0)
			rightX := min(x+r+1, w-1)
			li := row + leftX*4
			ri := row + rightX*4
			sumR = sumR + uint32(src[ri+0]) - uint32(src[li+0])
			sumG = sumG + uint32(src[ri+1]) - uint32(src[li+1])
			sumB = sumB + uint32(src[ri+2]) - uint32(src[li+2])
		}
	}
}

func boxBlurV(src, dst []byte, w, h, r int) {
	win := uint32(2*r + 1)
	half := win >> 1
	stride := w * 4
	for x := range w {
		col := x * 4
		var sumR, sumG, sumB uint32
		for k := -r; k <= r; k++ {
			cy := min(max(k, 0), h-1)
			si := cy*stride + col
			sumR += uint32(src[si+0])
			sumG += uint32(src[si+1])
			sumB += uint32(src[si+2])
		}
		for y := range h {
			di := y*stride + col
			dst[di+0] = uint8((sumR + half) / win)
			dst[di+1] = uint8((sumG + half) / win)
			dst[di+2] = uint8((sumB + half) / win)
			dst[di+3] = 255

			topY := max(y-r, 0)
			botY := min(y+r+1, h-1)
			ti := topY*stride + col
			bi := botY*stride + col
			sumR = sumR + uint32(src[bi+0]) - uint32(src[ti+0])
			sumG = sumG + uint32(src[bi+1]) - uint32(src[ti+1])
			sumB = sumB + uint32(src[bi+2]) - uint32(src[ti+2])
		}
	}
}
