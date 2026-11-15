package hud

import (
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/shader"

	"hellauncher/internal/catalog"
	"hellauncher/internal/engine"
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

func TestDeployingSweepBarStaysClippedWithinStripAcrossCycle(t *testing.T) {
	loadout := catalog.DefaultSnapshot()
	loadout.Settings.HUDTheme = catalog.ThemeTactical
	loadout.Settings.VisorCurved = false
	snap := engine.Snapshot{
		Phase:     engine.PhaseDeploying,
		MatchedID: "open-chrome",
		Typed:     []catalog.Dir{catalog.DirUp, catalog.DirDown, catalog.DirUp},
		Trusted:   true,
		Loadout:   loadout,
	}

	tt := ui.NewTester(func(c *ui.Context) {
		View(c, snap)
	}, tacticalWidth, tacticalHeight)
	tt.Frame()
	// Let entrance/shift animations settle so strip sits at its steady X bounds.
	tt.Advance(220 * time.Millisecond)

	// Outer padding X=20 + visor-stack padding left=10 + targetShift=6 -> strip spans [36 .. 418].
	// Even including outer 18px yellow glow shadow around the strip, pixels outside the halo
	// in the bottom sweep bar's Y band must stay transparent across the entire 620ms sweep loop.
	for step := 0; step < 8; step++ {
		img := tt.Image()
		for y := 136; y < 149; y++ {
			for _, x := range []int{2, 8, 448, 462, 474} {
				_, _, _, a := img.At(x, y).RGBA()
				if a > 512 {
					t.Fatalf("sweep bar bled outside strip bounds at step=%d (%dms), pixel (%d,%d) alpha=%d",
						step, step*120, x, y, a)
				}
			}
		}
		tt.Advance(120 * time.Millisecond)
	}
}
