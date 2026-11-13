package hud

import (
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/shader"
)

func TestVisorLensShaderRendersWithoutArtifacts(t *testing.T) {
	ensureAssets()
	if visorShader == nil {
		t.Fatal("expected visorShader to be compiled")
	}

	img := ui.Render(func(c *ui.Context) {
		c.Root().Background(ui.Transparent)
		ui.Box(c).
			Size(36, 24).
			Background(ui.Hex("#ffe900")).
			Effect(visorShader.Effect(shader.Options{
				MaxSampleOffset: ui.Point{X: 8, Y: 10},
			}))
	}, 48, 36, 1)

	b := img.Bounds()
	if b.Dx() != 48 || b.Dy() != 36 {
		t.Fatalf("unexpected rendered image bounds: %v", b)
	}
}
