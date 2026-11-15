package onboarding

import (
	"bytes"
	_ "embed"
	"image"
	"image/jpeg"
	"image/png"
	"log/slog"
	"sync"

	"github.com/egoist/mygo/ui/shader"
)

//go:embed assets/montage.mp4
var rawMontageMP4 []byte

//go:embed assets/montage_poster.jpg
var rawMontagePosterJPG []byte

//go:embed assets/hero_logo.png
var rawHeroLogoPNG []byte

const montageDefaultDurationSec = 14.80

// imprintShaderSrc runs natively as a GPU LayerEffect over the 2560x1200 H.264
// broadcast video layer. It applies physical-pixel micro-contrast sharpening
// (zero zoom oscillation so native resolution stays crisp), outer-edge-only
// feathered vignette so the central 75% stays completely clear, and subtle dark
// space/shadow translucency into the desktop.
const imprintShaderSrc = `package main

var Time float
var Reveal float

func LayerEffect(pos vec2) vec4 {
	sz := rectSize()
	if sz.x <= 1.0 || sz.y <= 1.0 || Reveal <= 0.001 {
		return vec4(0)
	}

	step := 1.0 / max(pixelScale(), 1.0)
	c := layerSample(pos).rgb
	n := layerSample(pos + vec2(0.0, -step)).rgb
	s := layerSample(pos + vec2(0.0, step)).rgb
	e := layerSample(pos + vec2(step, 0.0)).rgb
	w := layerSample(pos + vec2(-step, 0.0)).rgb
	sharp := clamp(c+(c-0.25*(n+s+e+w))*0.34, vec3(0.0), vec3(1.0))
	graded := clamp((sharp-vec3(0.022))*1.08, vec3(0.0), vec3(1.0))

	nx := pos.x / sz.x
	ny := pos.y / sz.y
	dx := (nx - 0.5) * 1.54
	dy := (ny - 0.5) * 1.66
	dist := sqrt(dx*dx + dy*dy)
	vignette := 1.0 - smoothstep(0.74, 1.18, dist)

	luma := dot(graded, vec3(0.299, 0.587, 0.114))
	imprint := 0.88 + 0.12*smoothstep(0.02, 0.35, luma)

	alpha := clamp(Reveal*vignette*imprint, 0.0, 0.98)
	return vec4(graded*alpha, alpha)
}
`

var (
	montageOnce      sync.Once
	imprintShader    *shader.Shader
	montagePosterImg image.Image
	heroLogoImg      image.Image
)

func ensureMontageAssets() {
	montageOnce.Do(func() {
		sh, err := shader.Compile(imprintShaderSrc)
		if err != nil {
			slog.Error("compile onboarding imprint shader failed", "err", err)
		} else {
			imprintShader = sh
		}

		if len(rawMontagePosterJPG) > 0 {
			if poster, pErr := jpeg.Decode(bytes.NewReader(rawMontagePosterJPG)); pErr != nil {
				slog.Error("decode onboarding montage_poster.jpg failed", "err", pErr)
			} else {
				montagePosterImg = poster
			}
		}

		if len(rawHeroLogoPNG) > 0 {
			if logo, lErr := png.Decode(bytes.NewReader(rawHeroLogoPNG)); lErr != nil {
				slog.Error("decode onboarding hero_logo.png failed", "err", lErr)
			} else {
				heroLogoImg = logo
			}
		}
	})
}

func heroLogoImage() image.Image {
	ensureMontageAssets()
	return heroLogoImg
}

func montagePosterImage() image.Image {
	ensureMontageAssets()
	return montagePosterImg
}

func montageDurationSec() float64 {
	return montageDefaultDurationSec
}

// CompileImprintShaderForTest exposes the Kage LayerEffect shader for unit tests.
func CompileImprintShaderForTest() *shader.Shader {
	ensureMontageAssets()
	return imprintShader
}

// MontageVideoBytesLenForTest returns the byte size of the embedded HD montage MP4.
func MontageVideoBytesLenForTest() int {
	return len(rawMontageMP4)
}

// MontagePosterResolutionForTest returns the pixel bounds of the embedded poster frame.
func MontagePosterResolutionForTest() image.Rectangle {
	img := montagePosterImage()
	if img == nil {
		return image.Rectangle{}
	}
	return img.Bounds()
}

// HeroLogoImageForTest exposes the decoded authentic Helldivers 2 Hero Logo image.
func HeroLogoImageForTest() image.Image {
	return heroLogoImage()
}
