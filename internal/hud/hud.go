package hud

import (
	"embed"
	"fmt"
	"io/fs"
	"log/slog"
	"math"
	"path"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/shader"

	"helldriver/internal/catalog"
	"helldriver/internal/engine"
)

//go:embed assets/fonts/*.otf assets/icons/*
var assetsFS embed.FS

const (
	overlayWidth  = 476
	overlayHeight = 590
)

var (
	assetsOnce  sync.Once
	arrowSVG    *ui.SVG
	emblemSVG   *ui.SVG
	spinnerSVG  *ui.SVG
	svgIcons    map[string]*ui.SVG
	visorShader *shader.Shader
)

const arrowPolygonSVG = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 20 20" fill="currentColor"><polygon points="13 20 13 15 20 15 20 5 13 5 13 0 0 10 13 20"/></svg>`

const superEarthEmblemSVG = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 32 32">
  <rect x="2" y="2" width="28" height="28" fill="none" stroke="#ffe900" stroke-width="2.4"/>
  <polygon fill="#ffe900" points="16,5 19,12 26,12 20.5,16.5 22.5,24 16,19.5 9.5,24 11.5,16.5 6,12 13,12"/>
</svg>`

const tacticalSpinnerSVG = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 20 20" fill="none">
  <circle cx="10" cy="10" r="7.5" stroke="#ffe900" stroke-opacity="0.28" stroke-width="2.6"/>
  <path d="M10 2.5 A7.5 7.5 0 0 1 17.5 10" stroke="#ffffff" stroke-width="2.6" stroke-linecap="square"/>
</svg>`

// visorLensShaderSrc runs natively on Metal / Direct3D 11 via mygo/ui/shader:
// it projects the upper-left HUD onto a spherical helmet visor dome (optical
// center to the right-middle of the panel), adding genuine curved visor barrel
// geometry, subtle peripheral lens dispersion, and fine holographic scanlines.
const visorLensShaderSrc = `package main

func LayerEffect(pos vec2) vec4 {
	sz := rectSize()
	if sz.x <= 1.0 || sz.y <= 1.0 {
		return layerSample(pos)
	}
	nx := pos.x / sz.x
	ny := pos.y / sz.y

	// Optical center of the helmet visor sits to the right of the upper-left HUD
	vx := 1.08 - nx
	vy := 0.52 - ny
	r2 := vx*vx + vy*vy

	offX := (r2 - 0.85) * vx * 11.5
	offY := (r2 - 0.45) * vy * 14.5
	src := vec2(pos.x-offX, pos.y-offY)

	// Subtle peripheral polycarbonate visor chromatic dispersion near outer rim
	disp := vx * vx * 0.36
	cR := layerSample(src + vec2(disp, 0))
	cG := layerSample(src)
	cB := layerSample(src - vec2(disp, 0))
	a := max(cG.a, max(cR.a, cB.a))
	col := vec4(cR.r, cG.g, cB.b, a)

	// Fine holographic visor scanline modulation
	scan := 0.968 + 0.032*sin(pos.y*2.35)
	return vec4(col.rgb*scan, col.a)
}
`

var inlineTacticalSVGs = map[string]string{
	"tactical-chrome": `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 126 126" fill="#fff">
		<path fill="#4FB3D0" d="M8 28V8h20L8 28Zm110 0V8H98l20 20ZM8 98v20h20L8 98Zm110 0v20H98l20-20Z"/>
		<path fill="#4FB3D0" d="M63 16a47 47 0 0 1 40.7 23.5H63a23.5 23.5 0 0 0-20.3 11.8L22.3 27.8A46.8 46.8 0 0 1 63 16Z"/>
		<path d="M20 44.3 40.4 79.6A23.5 23.5 0 0 1 40 63c0-4.2 1.1-8.1 3-11.5L23.5 28A47 47 0 0 0 16 63c0 18.6 10.8 34.7 26.5 42.3L63 70a7 7 0 0 1-6-3.5"/>
		<path d="M63 45.5h43.5A47 47 0 0 1 110 63c0 25.9-21 47-47 47l20.3-35.2A23.5 23.5 0 0 0 63 45.5Z"/>
		<circle cx="63" cy="63" r="15" fill="#fff"/>
		<circle cx="63" cy="63" r="8" fill="#4FB3D0"/>
	</svg>`,
	"tactical-zed": `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 126 126" fill="#fff">
		<path fill="#4FB3D0" d="M18 14h90l10 10v78l-10 10H18L8 102V24l10-10Zm8 14v70h74V28H26Z"/>
		<path d="M36 38h54v13L54 88h36v12H36V87l36-37H36V38Z"/>
		<path fill="#4FB3D0" d="M59 10h8v10h-8zm0 96h8v10h-8zM10 59h10v8H10zm96 0h10v8h-10z"/>
	</svg>`,
	"tactical-terminal": `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 126 126" fill="#fff">
		<path fill="#679452" d="M12 20h102v86H12V20Zm10 18v58h82V38H22Z"/>
		<path d="m32 50 18 15-18 15-6-7 11-8-11-8 6-7Zm24 24h32v9H56v-9Z"/>
	</svg>`,
}

func ensureAssets() {
	assetsOnce.Do(func() {
		fontFiles := []string{
			"assets/fonts/FSSinclair-Light.otf",
			"assets/fonts/FSSinclair-Regular.otf",
			"assets/fonts/FSSinclair-Medium.otf",
		}
		for _, f := range fontFiles {
			if data, err := assetsFS.ReadFile(f); err == nil {
				if rErr := ui.RegisterFont(data, "FS Sinclair"); rErr != nil {
					slog.Warn("hud register font failed", "file", f, "err", rErr)
				}
			}
		}

		arrowSVG = ui.MustParseSVG([]byte(arrowPolygonSVG))
		emblemSVG = ui.MustParseSVG([]byte(superEarthEmblemSVG))
		spinnerSVG = ui.MustParseSVG([]byte(tacticalSpinnerSVG))
		visorShader = shader.MustCompile(visorLensShaderSrc)
		svgIcons = make(map[string]*ui.SVG, 24)

		for key, raw := range inlineTacticalSVGs {
			if parsed, err := ui.ParseSVG([]byte(raw)); err == nil {
				svgIcons[key] = parsed
			}
		}

		entries, _ := fs.ReadDir(assetsFS, "assets/icons")
		for _, ent := range entries {
			if ent.IsDir() {
				continue
			}
			name := ent.Name()
			full := path.Join("assets/icons", name)
			data, err := assetsFS.ReadFile(full)
			if err != nil {
				continue
			}
			if strings.ToLower(path.Ext(name)) == ".svg" {
				parsed, pErr := ui.ParseSVG(data)
				if pErr != nil {
					slog.Warn("hud parse svg failed", "icon", name, "err", pErr)
					continue
				}
				svgIcons[name] = parsed
			}
		}
	})
}

func hudTheme() *ui.Theme {
	t := ui.DarkTheme()
	accent := ui.Hex("#ffe900")
	t.Background = ui.RGBA(15, 19, 24, 0.88)
	t.Surface = ui.RGBA(18, 23, 29, 0.86)
	t.SurfaceHover = ui.RGBA(24, 31, 39, 0.92)
	t.SurfacePressed = ui.RGBA(28, 36, 46, 0.96)
	t.Border = ui.RGBA(255, 255, 255, 0.10)
	t.Text = ui.Hex("#f5f8fb")
	t.TextMuted = ui.RGBA(235, 242, 248, 0.56)
	t.Accent = accent
	t.AccentHover = accent
	t.AccentPressed = accent
	t.AccentText = ui.Hex("#0b0f14")
	// Critical: overlay uses ShowInactive(), so InactiveAccent MUST equal Accent.
	t.InactiveAccent = accent
	t.Danger = ui.Hex("#ff3b30")
	t.Success = ui.Hex("#42d674")
	t.Warning = accent
	t.Radius = 0
	t.Font = "FS Sinclair"
	t.FontSize = 13
	return t
}

// Overlay owns the non-activating transparent GPU floating HUD window.
type Overlay struct {
	win       *mygo.Window
	snap      engine.Snapshot
	shown     bool
	closing   bool
	closeOnce sync.Once
}

// NewOverlay creates the click-through floating tactical HUD window.
func NewOverlay() *Overlay {
	ensureAssets()

	disp := mygo.Screen.DisplayNearestPoint(mygo.Screen.CursorScreenPoint())
	initX := disp.WorkArea.X + 14
	initY := disp.WorkArea.Y + 10

	o := &Overlay{
		snap: engine.Snapshot{
			Phase:   engine.PhaseIdle,
			Trusted: true,
			Loadout: catalog.DefaultSnapshot(),
		},
	}

	o.win = mygo.NewWindow(mygo.WindowOptions{
		Title:         "HellDriver Tactical HUD",
		X:             initX,
		Y:             initY,
		Width:         overlayWidth,
		Height:        overlayHeight,
		Hidden:        true,
		Frameless:     true,
		Transparent:   true,
		AlwaysOnTop:   true,
		Panel:         true,
		DisableResize: true,
		DisableShadow: true,
		SkipTaskbar:   true,
		Content: ui.View(func(c *ui.Context) {
			View(c, o.snap)
		}),
	})
	o.win.SetIgnoreMouseEvents(true)
	o.win.OnClose(func(e *mygo.CloseEvent) {
		if !o.closing {
			e.PreventDefault()
		}
	})
	return o
}

// Apply publishes a new Engine Snapshot onto the UI thread, repositioning onto
// the active cursor display whenever the HUD wakes from PhaseIdle.
func (o *Overlay) Apply(snap engine.Snapshot) {
	if o == nil || o.win == nil {
		return
	}
	o.win.Update(func() {
		if o.closing {
			return
		}
		wasIdle := o.snap.Phase == engine.PhaseIdle
		o.snap = snap

		if snap.Phase != engine.PhaseIdle {
			if wasIdle {
				disp := mygo.Screen.DisplayNearestPoint(mygo.Screen.CursorScreenPoint())
				o.win.SetPosition(disp.WorkArea.X+14, disp.WorkArea.Y+10)
			}
			if !o.shown {
				o.shown = true
				o.win.ShowInactive()
			}
		}
	})
}

// Close permanently destroys the HUD window on application shutdown.
func (o *Overlay) Close() {
	if o == nil || o.win == nil {
		return
	}
	o.closeOnce.Do(func() {
		o.win.Update(func() {
			o.closing = true
			o.win.Close()
		})
	})
}

// View is the pure read-only GPU HUD presentation function over engine.Snapshot.
func View(c *ui.Context, snap engine.Snapshot) {
	ensureAssets()
	t := hudTheme()
	c.SetTheme(t)
	c.Root().Background(ui.Transparent)

	visibleTarget := float32(1.0)
	if snap.Phase == engine.PhaseIdle {
		visibleTarget = 0.0
	}

	outer := ui.Column(c.Key("hud-shell")).Fill().Padding(18, 20)
	gpuReady := ui.Local(outer, "gpu-accel", func() bool { return false })
	svcs := c.Services()
	outer.Draw(func(p *ui.Painter, _ ui.Rect) {
		if acc := p.Accelerated(); acc != *gpuReady {
			*gpuReady = acc
			svcs.Invalidate()
		}
	})

	vis := outer.Animate("vis", visibleTarget, 160*time.Millisecond)
	if vis <= 0.01 && snap.Phase == engine.PhaseIdle {
		return
	}

	errProgress := outer.Animate("err-seq", float32(snap.ErrorSeq), 260*time.Millisecond)
	errFlash := clamp01(float32(snap.ErrorSeq) - errProgress)
	shakeX := float32(math.Sin(float64(errFlash)*math.Pi*4)) * 7.0 * errFlash

	stepProgress := outer.Animate("step-seq", float32(snap.StepSeq), 150*time.Millisecond)
	stepPulse := clamp01(float32(snap.StepSeq) - stepProgress)

	slideX := (1.0 - vis) * -34.0

	outer.Children(func() {
		hudStack := ui.Column(c.Key("visor-stack")).
			Width(404).
			Padding(8, 12, 10, 10).
			Gap(5).
			Opacity(vis)

		if snap.Loadout.Settings.VisorCurved {
			// NOTE: In mygo/ui, transforms chain post-multiply (Then):
			// RotateY / RotateX MUST precede Perspective(dist) so perspective
			// projects the rotated 3D plane! Plus visorShader adds true optical
			// helmet-glass spherical curvature & scanlines on Metal / D3D11.
			hudStack.
				TransformOrigin(0, 0.14).
				RotateY(11.5).
				RotateX(-1.2).
				Perspective(580).
				Translate(slideX+shakeX, 0)
			if *gpuReady && visorShader != nil {
				hudStack.Effect(visorShader.Effect(shader.Options{
					MaxSampleOffset: ui.Point{X: 28, Y: 28},
				}))
			}
		} else {
			hudStack.Translate(slideX+shakeX, 0)
		}

		hudStack.Children(func() {
			renderVisorCornerReticle(c)
			renderHeaderBar(c, t, snap, errFlash)

			if runtime.GOOS == "darwin" && !snap.Trusted {
				renderUntrustedBanner(c)
			}

			if snap.Phase == engine.PhaseArmed {
				renderArmedBanner(c)
			}

			renderStratagemStrips(c, t, snap, errFlash, stepPulse)
			renderLiveInputFooter(c, t, snap, stepPulse)
		})
	})
}

// renderVisorCornerReticle paints the floating optical L-bracket + tick `[▸`
// at the upper-left corner of the helmet HUD.
func renderVisorCornerReticle(c *ui.Context) {
	ui.Row(c.Key("visor-reticle")).
		Height(12).
		AlignItems(ui.Center).
		Translate(-6, 1).
		DrawOver(func(p *ui.Painter, r ui.Rect) {
			shadowCol := ui.RGBA(0, 0, 0, 0.72)
			bracketCol := ui.RGBA(255, 233, 0, 0.88)
			dimCol := ui.RGBA(255, 255, 255, 0.48)
			// Crisp dark shadow backing so reticle reads over both light & dark desktops
			p.Fill(ui.Rect{X: r.X + 1, Y: r.Y + 1, W: 9, H: 1.8}, shadowCol, 0)
			p.Fill(ui.Rect{X: r.X + 1, Y: r.Y + 1, W: 1.8, H: 16}, shadowCol, 0)
			// Top horizontal + left vertical + bottom subtle tick
			p.Fill(ui.Rect{X: r.X, Y: r.Y, W: 9, H: 1.6}, bracketCol, 0)
			p.Fill(ui.Rect{X: r.X, Y: r.Y, W: 1.6, H: 16}, bracketCol, 0)
			p.Fill(ui.Rect{X: r.X, Y: r.Y + 14.5, W: 6, H: 1.4}, dimCol, 0)
		}).
		Children(func() {
			ui.Box(c).Width(7)
			ui.Text(c, "▸").
				FontSize(9.5).
				Bold().
				TextColor(ui.RGBA(255, 233, 0, 0.90)).
				TextShadow(0, 1, 2, ui.RGBA(0, 0, 0, 0.90))
		})
}

func renderHeaderBar(c *ui.Context, t *ui.Theme, snap engine.Snapshot, errFlash float32) {
	leftBar := t.Accent
	hdrBg := ui.RGBA(15, 19, 24, 0.90)
	glowCol := ui.Transparent

	switch {
	case errFlash > 0.08:
		leftBar = ui.Hex("#ff3b30")
		hdrBg = ui.RGBA(88, 20, 20, 0.92)
		glowCol = ui.RGBA(255, 59, 48, 0.35)
	case snap.Phase == engine.PhaseDeployed:
		leftBar = ui.Hex("#ffffff")
		hdrBg = ui.RGBA(30, 38, 46, 0.95)
		glowCol = ui.RGBA(255, 233, 0, 0.32)
	case snap.Phase == engine.PhaseDeploying || snap.Phase == engine.PhaseArmed:
		leftBar = t.Accent
		hdrBg = ui.RGBA(24, 31, 39, 0.94)
		glowCol = ui.RGBA(255, 233, 0, 0.26)
	}

	hdr := ui.Row(c.Key("hdr-strip")).
		Padding(7, 10, 7, 12).
		AlignItems(ui.Center).
		Gap(8).
		Background(hdrBg).
		Border(1, ui.RGBA(255, 255, 255, 0.12)).
		Shadow(0, 6, 20, 0, ui.RGBA(0, 0, 0, 0.50))
	paintLeftBar(hdr, 3.5, leftBar)

	if glowCol != ui.Transparent {
		hdr.Shadow(0, 0, 18, 0, glowCol)
	}

	badgeText, badgeBg, badgeBorder, badgeFg := headerBadgeStyle(snap, errFlash)

	hdr.Children(func() {
		if emblemSVG != nil {
			ui.Image(c, emblemSVG).Size(20, 20)
		}
		ui.Text(c, "战略配备").
			FontSize(15.5).
			Bold().
			TextColor(ui.Hex("#ffffff")).
			TextShadow(0, 1, 3, ui.RGBA(0, 0, 0, 0.92))
		ui.Text(c, "STRATAGEMS").
			FontSize(11).
			Bold().
			TextColor(ui.RGBA(255, 255, 255, 0.50)).
			Grow(1)

		badge := ui.Row(c.Key("hdr-badge")).
			Padding(2, 8).
			Background(badgeBg).
			Border(1, badgeBorder)
		if snap.Phase == engine.PhaseDeploying || snap.Phase == engine.PhaseArmed {
			p := badge.Loop("badge-pulse", 850*time.Millisecond, ui.Linear)
			alpha := 0.78 + 0.22*float32(math.Sin(float64(p)*2*math.Pi))
			badge.Opacity(alpha).Shadow(0, 0, 10, 0, ui.RGBA(255, 233, 0, 0.45))
		}
		badge.Children(func() {
			ui.Text(c, badgeText).FontSize(11).Bold().TextColor(badgeFg)
		})
	})
}

func headerBadgeStyle(snap engine.Snapshot, errFlash float32) (label string, bg, border, fg ui.Color) {
	if errFlash > 0.08 {
		return "指令错误", ui.RGBA(255, 59, 48, 0.30), ui.Hex("#ff6b6b"), ui.Hex("#ffffff")
	}
	switch snap.Phase {
	case engine.PhaseArmed:
		return "信标已握持", ui.Hex("#ffe900"), ui.Hex("#ffffff"), ui.Hex("#0b0f14")
	case engine.PhaseDeploying:
		return "空投部署中", ui.RGBA(255, 233, 0, 0.28), ui.Hex("#ffe900"), ui.Hex("#fffbe0")
	case engine.PhaseDeployed:
		return "✓ 已就绪", ui.Hex("#ffe900"), ui.Hex("#ffffff"), ui.Hex("#0b0f14")
	case engine.PhasePreview:
		return "终端就绪", ui.RGBA(79, 179, 208, 0.22), ui.Hex("#7dd3fc"), ui.Hex("#e0f2fe")
	default:
		if len(snap.Typed) > 0 {
			return fmt.Sprintf("输入中 %d", len(snap.Typed)), ui.RGBA(255, 233, 0, 0.22), ui.Hex("#ffe900"), ui.Hex("#ffe900")
		}
		return "WASD // ↑↓←→", ui.RGBA(255, 233, 0, 0.16), ui.Hex("#ffe900"), ui.Hex("#ffe900")
	}
}

func renderUntrustedBanner(c *ui.Context) {
	b := ui.Row(c.Key("untrusted-banner")).
		Padding(6, 10, 6, 12).
		Gap(7).
		AlignItems(ui.Center).
		Background(ui.RGBA(145, 34, 28, 0.92)).
		Border(1, ui.RGBA(255, 255, 255, 0.14)).
		Shadow(0, 4, 14, 0, ui.RGBA(0, 0, 0, 0.44))
	paintLeftBar(b, 3, ui.Hex("#ff6b6b"))
	b.Children(func() {
		ui.Box(c).Size(7, 7).Radius(4).Background(ui.Hex("#ff6b6b"))
		ui.Text(c, "未获辅助功能权限：请在菜单栏点击「授予辅助功能权限…」").
			FontSize(11).
			TextColor(ui.Hex("#ffe4e4"))
	})
}

func renderArmedBanner(c *ui.Context) {
	banner := ui.Row(c.Key("armed-banner")).
		Padding(6, 10, 6, 12).
		Background(ui.RGBA(46, 40, 10, 0.94)).
		Border(1, ui.RGBA(255, 233, 0, 0.42)).
		Shadow(0, 0, 16, 0, ui.RGBA(255, 233, 0, 0.30))
	paintLeftBar(banner, 3.5, ui.Hex("#ffe900"))
	pulse := banner.Loop("armed-pulse", 900*time.Millisecond, ui.Linear)
	banner.Opacity(0.80 + 0.20*float32(math.Sin(float64(pulse)*2*math.Pi))).Children(func() {
		ui.Text(c, "⊕ 信标已握持 · 左键点击目标位置空投 · 右键/ESC 取消").
			FontSize(11.5).
			Bold().
			TextColor(ui.Hex("#ffe900"))
	})
}

func renderStratagemStrips(c *ui.Context, t *ui.Theme, snap engine.Snapshot, errFlash, stepPulse float32) {
	isLockedPhase := snap.Phase == engine.PhaseArmed || snap.Phase == engine.PhaseDeploying || snap.Phase == engine.PhaseDeployed

	ui.Column(c.Key("strat-list")).Gap(4).Children(func() {
		shown := 0
		for _, st := range snap.Loadout.Stratagems {
			if !st.Enabled {
				continue
			}
			shown++
			isTarget := isLockedPhase && snap.MatchedID == st.ID
			matchesPrefix := len(snap.Typed) == 0 || hasPrefix(st.Code, snap.Typed)

			dimmed := false
			if isLockedPhase {
				dimmed = !isTarget
			} else if len(snap.Typed) > 0 && !matchesPrefix {
				dimmed = true
			}

			targetShift := float32(0)
			if isTarget {
				targetShift = 6
			} else if len(snap.Typed) > 0 && matchesPrefix {
				targetShift = 3.5
			}

			targetOpacity := float32(1.0)
			if dimmed {
				targetOpacity = 0.24
			}

			strip := ui.Column(c.Key("strat-"+st.ID)).
				Padding(5, 10, 6, 9).
				Gap(4).
				Border(1, ui.RGBA(255, 255, 255, 0.08)).
				Shadow(0, 4, 14, 0, ui.RGBA(0, 0, 0, 0.45))

			shiftX := strip.Animate("shift", targetShift, 130*time.Millisecond)
			rowAlpha := strip.Animate("alpha", targetOpacity, 120*time.Millisecond)
			strip.Translate(shiftX, 0).Opacity(rowAlpha)

			switch {
			case errFlash > 0.08:
				strip.Background(ui.RGBA(128, 22, 22, 0.82))
				paintLeftBar(strip, 3, ui.Hex("#ff3b30"))
			case isTarget && snap.Phase == engine.PhaseDeployed:
				strip.LinearGradient(ui.LinearGradient{
					Angle: 90,
					Stops: []ui.GradientStop{
						{Color: ui.RGBA(255, 233, 0, 0.34), At: 0},
						{Color: ui.RGBA(28, 36, 46, 0.96), At: 0.68},
						{Color: ui.RGBA(22, 29, 38, 0.96), At: 1},
					},
				}).Shadow(0, 0, 22, 0, ui.RGBA(255, 233, 0, 0.36))
				paintLeftBar(strip, 4, ui.Hex("#ffffff"))
			case isTarget:
				strip.LinearGradient(ui.LinearGradient{
					Angle: 90,
					Stops: []ui.GradientStop{
						{Color: ui.RGBA(255, 233, 0, 0.22), At: 0},
						{Color: ui.RGBA(25, 33, 42, 0.96), At: 0.68},
						{Color: ui.RGBA(18, 24, 32, 0.96), At: 1},
					},
				}).Shadow(0, 0, 18, 0, ui.RGBA(255, 233, 0, 0.25))
				paintLeftBar(strip, 4, t.Accent)
			case len(snap.Typed) > 0 && matchesPrefix:
				strip.Background(ui.RGBA(28, 35, 44, 0.94))
				paintLeftBar(strip, 3, t.Accent)
			default:
				strip.Background(t.Surface)
			}

			strip.Children(func() {
				ui.Row(c).AlignItems(ui.Center).Gap(9).Children(func() {
					renderCategoryIconBox(c, st, isTarget)

					ui.Column(c).Grow(1).MinWidth(0).Gap(3).Children(func() {
						ui.Row(c).AlignItems(ui.Center).Gap(6).Children(func() {
							ui.Text(c, st.Name).
								FontSize(14).
								Bold().
								TextColor(ui.Hex("#ffffff")).
								TextShadow(0, 1, 3, ui.RGBA(0, 0, 0, 0.95)).
								SingleLine().
								Grow(1)

							renderRowSubtitle(c, st, isTarget, snap.Phase, t)
						})

						ui.Row(c).Gap(5).Height(18).AlignItems(ui.Center).Children(func() {
							for idx, dir := range st.Code {
								matched := isTarget || (matchesPrefix && idx < len(snap.Typed))
								justMatched := !isTarget && matchesPrefix && len(snap.Typed) > 0 && idx == len(snap.Typed)-1 && stepPulse > 0.02
								renderDirectionArrow(c.Key(idx), dir, 14.5, matched, justMatched, stepPulse)
							}
							if st.Delivery == catalog.DeliveryBeaconClick {
								ui.Spacer(c)
								ui.Text(c, "⊕信标").FontSize(10).Bold().TextColor(ui.Hex("#7dd3fc"))
							}
						})
					})
				})

				if isTarget && snap.Phase == engine.PhaseDeploying {
					track := ui.Row(c.Key("deploy-sweep")).Height(2.5).Background(ui.RGBA(255, 233, 0, 0.16))
					prog := track.Loop("sweep", 700*time.Millisecond, ui.Linear)
					track.Children(func() {
						ui.Box(c).
							WidthPercent(46).
							Height(2.5).
							LinearGradient(ui.LinearGradient{
								Angle: 90,
								Stops: []ui.GradientStop{
									{Color: ui.RGBA(255, 233, 0, 0.0), At: 0},
									{Color: ui.Hex("#ffe900"), At: 0.65},
									{Color: ui.Hex("#ffffff"), At: 1.0},
								},
							}).
							Shadow(0, 0, 8, 0, ui.RGBA(255, 233, 0, 0.90)).
							TranslatePercent(-100+prog*320, 0)
					})
				} else if isTarget && snap.Phase == engine.PhaseDeployed {
					ui.Box(c.Key("deploy-done")).
						Height(2.5).
						Background(t.Accent).
						Shadow(0, 0, 10, 0, ui.RGBA(255, 233, 0, 0.90))
				}
			})
		}

		if shown == 0 {
			ui.Row(c).Padding(10, 12).Background(t.Surface).Children(func() {
				ui.Text(c, "暂无已启用的战略配备，请在菜单栏打开「军械库」配置。").
					FontSize(12).
					TextColor(t.TextMuted)
			})
		}
	})
}

func renderCategoryIconBox(c *ui.Context, st catalog.Stratagem, isTarget bool) {
	border := categoryBorderColor(st.Category)
	if isTarget {
		border = ui.Hex("#ffe900")
	}
	box := ui.Row(c).
		Size(42, 42).
		Shrink(0).
		Center().
		Background(ui.RGBA(10, 13, 17, 0.95)).
		Border(2.5, border)
	if isTarget {
		box.Shadow(0, 0, 10, 0, ui.RGBA(255, 233, 0, 0.38))
	}
	box.Children(func() {
		if icon := lookupStratagemIcon(st.Icon); icon != nil {
			ui.Image(c, icon).Size(32, 32)
		} else {
			ui.Text(c, "✪").FontSize(16).Bold().TextColor(ui.Hex("#ffe900"))
		}
	})
}

func renderRowSubtitle(c *ui.Context, st catalog.Stratagem, isTarget bool, phase engine.Phase, t *ui.Theme) {
	if isTarget {
		switch phase {
		case engine.PhaseArmed:
			ui.Text(c, "等待左键投掷信标…").FontSize(10.5).Bold().TextColor(t.Accent).SingleLine()
			return
		case engine.PhaseDeploying:
			label := "正在唤醒窗口…"
			if len(st.Steps) > 0 && st.Steps[0].Kind == catalog.StepCloseActiveWindow {
				label = "正在执行清场…"
			}
			ui.Row(c.Key("spin-row")).AlignItems(ui.Center).Gap(5).Children(func() {
				if spinnerSVG != nil {
					sp := ui.Image(c.Key("sp"), spinnerSVG).Size(10, 10)
					turn := sp.Loop("turn", 580*time.Millisecond, ui.Linear)
					sp.Rotate(turn * 360)
				}
				ui.Text(c, label).FontSize(10.5).Bold().TextColor(t.Accent).SingleLine()
			})
			return
		case engine.PhaseDeployed:
			ui.Text(c, "✓ 已就绪 DEPLOYED").FontSize(10.5).Bold().TextColor(t.Accent).SingleLine()
			return
		}
	}
	ui.Text(c, st.Subtitle).FontSize(10.5).TextColor(t.TextMuted).SingleLine()
}

func renderLiveInputFooter(c *ui.Context, t *ui.Theme, snap engine.Snapshot, stepPulse float32) {
	footer := ui.Row(c.Key("footer-strip")).
		Padding(5, 10, 5, 12).
		AlignItems(ui.Center).
		Gap(8).
		Background(ui.RGBA(12, 16, 20, 0.90)).
		Border(1, ui.RGBA(255, 255, 255, 0.09)).
		Shadow(0, 4, 14, 0, ui.RGBA(0, 0, 0, 0.44))
	paintLeftBar(footer, 3, ui.RGBA(255, 233, 0, 0.72))
	footer.Children(func() {
		ui.Row(c).Grow(1).Gap(5).Height(18).AlignItems(ui.Center).Children(func() {
			ui.Text(c, "指令序列:").FontSize(11).TextColor(ui.RGBA(255, 255, 255, 0.55))
			if len(snap.Typed) == 0 {
				ui.Text(c, "等待输入方向指令…").FontSize(11).TextColor(ui.RGBA(255, 255, 255, 0.36))
			} else {
				for idx, dir := range snap.Typed {
					just := idx == len(snap.Typed)-1 && stepPulse > 0.02
					renderDirectionArrow(c.Key(idx), dir, 14, true, just, stepPulse)
				}
			}
		})
		ui.Text(c, footerHint(snap)).FontSize(10.5).Bold().TextColor(ui.RGBA(255, 233, 0, 0.82)).SingleLine()
	})
}

func paintLeftBar(el ui.Element, width float32, col ui.Color) {
	if width <= 0 || col.A == 0 {
		return
	}
	el.DrawOver(func(p *ui.Painter, r ui.Rect) {
		p.Fill(ui.Rect{X: r.X, Y: r.Y, W: width, H: r.H}, col, 0)
	})
}

func renderDirectionArrow(c *ui.Context, dir catalog.Dir, size float32, matched, justMatched bool, stepPulse float32) {
	col := ui.RGBA(255, 255, 255, 0.42)
	if matched {
		col = ui.Hex("#ffe900")
		if justMatched {
			col = col.Mix(ui.Hex("#ffffff"), stepPulse)
		}
	}

	if arrowSVG == nil {
		ui.Text(c, dir.Arrow()).FontSize(size).Bold().TextColor(col)
		return
	}

	var rot float32
	switch dir {
	case catalog.DirLeft:
		rot = 0
	case catalog.DirUp:
		rot = 90
	case catalog.DirRight:
		rot = 180
	case catalog.DirDown:
		rot = -90
	}

	el := ui.Icon(c, arrowSVG).FontSize(size).Rotate(rot).TextColor(col)
	if matched {
		el.TextShadow(0, 0, 6, ui.RGBA(255, 233, 0, 0.90))
		if justMatched {
			s := 1.0 + 0.36*stepPulse
			el.Scale(s, s)
		}
	} else {
		el.TextShadow(1, 2, 1, ui.RGBA(0, 0, 0, 0.88))
	}
}

func lookupStratagemIcon(name string) *ui.SVG {
	if svg, ok := svgIcons[name]; ok {
		return svg
	}
	if svg, ok := svgIcons["orbital-precision.svg"]; ok {
		return svg
	}
	return nil
}

func categoryBorderColor(cat catalog.Category) ui.Color {
	switch cat {
	case catalog.CategoryRed:
		return ui.Hex("#d56152")
	case catalog.CategoryGreen:
		return ui.Hex("#679452")
	case catalog.CategoryYellow:
		return ui.Hex("#c9b268")
	default:
		return ui.Hex("#4fb3d0")
	}
}

func footerHint(snap engine.Snapshot) string {
	switch snap.Phase {
	case engine.PhaseArmed:
		return "左键空投 · 右键/ESC 取消"
	case engine.PhaseDeploying:
		return "战备部署中 · 窗口出现后收起"
	case engine.PhaseDeployed:
		return "战略配备部署完成"
	case engine.PhasePreview:
		switch snap.Loadout.Settings.TriggerMode {
		case catalog.ModeHoldLeftCtrl:
			return "按住左 CTRL 唤出"
		case catalog.ModeRightCtrlOrAlt:
			return "右 CTRL / CTRL+ALT 唤出"
		default:
			return "双击并按住 CTRL 唤出"
		}
	default:
		return "松开 CTRL 取消 · ESC 退出"
	}
}

func hasPrefix(code, typed []catalog.Dir) bool {
	if len(typed) > len(code) {
		return false
	}
	for i := range typed {
		if code[i] != typed[i] {
			return false
		}
	}
	return true
}

func clamp01(v float32) float32 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}
