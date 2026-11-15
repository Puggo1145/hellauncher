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
	"github.com/egoist/mygo/plugins/glass"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/shader"

	"hellauncher/internal/catalog"
	"hellauncher/internal/engine"
	"hellauncher/internal/i18n"
)

//go:embed assets/fonts/*.otf assets/icons/*.svg
var assetsFS embed.FS

const (
	tacticalWidth  = 476
	tacticalHeight = 590

	classicWidth     = 308
	classicPadTop    = 10
	classicPadBottom = 10
	classicRowHeight = 46
	classicRowGap    = 6
)

var (
	assetsOnce        sync.Once
	emblemSVG         *ui.SVG
	spinnerSVG        *ui.SVG
	classicSpinnerSVG *ui.SVG
	svgIcons          map[string]*ui.SVG
	visorShader       *shader.Shader
)

const superEarthEmblemSVG = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 128 128">
  <path fill="#ffe900" fill-rule="evenodd" d="M64 8 L112 27 L109 69 L124 85 L105 106 L90 100 L82 120 L64 126 L46 120 L38 100 L23 106 L4 85 L19 69 L16 27 Z M42 60.5 A13.5 13.5 0 1 0 42 87.5 A13.5 13.5 0 1 0 42 60.5 Z M86 60.5 A13.5 13.5 0 1 0 86 87.5 A13.5 13.5 0 1 0 86 60.5 Z M64 89 L54.5 103.5 L59.5 106.5 L64 100.5 L68.5 106.5 L73.5 103.5 Z"/>
</svg>`

const tacticalSpinnerSVG = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 20 20" fill="none">
  <circle cx="10" cy="10" r="7.5" stroke="#ffe900" stroke-opacity="0.28" stroke-width="2.6"/>
  <path d="M10 2.5 A7.5 7.5 0 0 1 17.5 10" stroke="#ffffff" stroke-width="2.6" stroke-linecap="square"/>
</svg>`

const vanillaSpinnerSVG = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 20 20" fill="none">
  <circle cx="10" cy="10" r="7.5" stroke="#ffffff" stroke-opacity="0.28" stroke-width="2.6"/>
  <path d="M10 2.5 A7.5 7.5 0 0 1 17.5 10" stroke="#ffffff" stroke-width="2.6" stroke-linecap="square"/>
</svg>`

// visorLensShaderSrc runs natively on Metal / Direct3D 11 via mygo/ui/shader:
// it projects the upper-left HUD onto a spherical helmet visor dome (optical
// center to the right-middle of the panel), adding curved visor barrel
// geometry, subtle peripheral lens dispersion, and fine holographic scanlines.
const visorLensShaderSrc = `package main

func LayerEffect(pos vec2) vec4 {
	sz := rectSize()
	if sz.x <= 1.0 || sz.y <= 1.0 {
		return layerSample(pos)
	}
	nx := pos.x / sz.x
	ny := pos.y / sz.y

	vx := 1.08 - nx
	vy := 0.52 - ny
	r2 := vx*vx + vy*vy

	offX := (r2 - 0.85) * vx * 3.6
	offY := (r2 - 0.45) * vy * 4.2
	src := vec2(pos.x-offX, pos.y-offY)

	disp := vx * vx * 0.18
	cR := layerSample(src + vec2(disp, 0))
	cG := layerSample(src)
	cB := layerSample(src - vec2(disp, 0))
	a := max(cG.a, max(cR.a, cB.a))
	col := vec4(cR.r, cG.g, cB.b, a)

	scan := 0.968 + 0.032*sin(pos.y*2.35)
	return vec4(col.rgb*scan, col.a)
}
`

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

		emblemSVG = ui.MustParseSVG([]byte(superEarthEmblemSVG))
		spinnerSVG = ui.MustParseSVG([]byte(tacticalSpinnerSVG))
		classicSpinnerSVG = ui.MustParseSVG([]byte(vanillaSpinnerSVG))
		visorShader = shader.MustCompile(visorLensShaderSrc)
		svgIcons = make(map[string]*ui.SVG, 128)

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

// HasIconSVG reports whether an exact embedded official Stratagem SVG exists for name.
func HasIconSVG(name string) bool {
	ensureAssets()
	_, ok := svgIcons[name]
	return ok
}

// EmblemSVG returns the Super Earth tactical emblem vector icon.
func EmblemSVG() *ui.SVG {
	ensureAssets()
	return emblemSVG
}

// IconSVG returns the parsed embedded SVG for an icon key (falling back to
// orbital-precision.svg), shared by HUD and Arsenal views.
func IconSVG(name string) *ui.SVG {
	ensureAssets()
	return lookupStratagemIcon(name)
}

// RenderArrow paints a chunky Helldivers directional arrow vector at size.
func RenderArrow(c *ui.Context, dir catalog.Dir, size float32, color ui.Color) {
	ensureAssets()
	paintArrowElement(c, dir, size, color, ui.Transparent, 1)
}

// CategoryBorderColor returns the canonical Super Earth border color for cat.
func CategoryBorderColor(cat catalog.Category) ui.Color {
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

// CategoryFillColor returns the dark category-tinted background fill inside a
// Stratagem square icon frame (matching the Helldivers 2 vanilla HUD).
func CategoryFillColor(cat catalog.Category) ui.Color {
	switch cat {
	case catalog.CategoryRed:
		return ui.RGBA(96, 36, 30, 0.78)
	case catalog.CategoryGreen:
		return ui.RGBA(38, 62, 32, 0.78)
	case catalog.CategoryYellow:
		return ui.RGBA(74, 62, 28, 0.78)
	default:
		return ui.RGBA(26, 58, 76, 0.78)
	}
}

func hudTheme() *ui.Theme {
	t := ui.DarkTheme()
	accent := ui.Hex("#ffe900")
	t.Background = ui.Transparent
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

func isClassicTheme(snap engine.Snapshot) bool {
	return snap.Loadout.Settings.HUDTheme != catalog.ThemeTactical
}

func classicHeight(snap engine.Snapshot) int {
	enabled := 0
	for _, st := range snap.Loadout.Stratagems {
		if st.Enabled {
			enabled++
		}
	}
	h := classicPadTop + classicPadBottom
	if runtime.GOOS == "darwin" && !snap.Trusted {
		h += 30 + classicRowGap
	}
	if enabled == 0 {
		return h + 42
	}
	return h + enabled*classicRowHeight + (enabled-1)*classicRowGap
}

func classicShellPad() (padY, padX int) {
	if runtime.GOOS == "darwin" {
		return 0, 0
	}
	return 8, 22
}

func overlayDimensions(snap engine.Snapshot) (int, int) {
	if isClassicTheme(snap) {
		padY, padX := classicShellPad()
		return classicWidth + 2*padX, classicHeight(snap) + 2*padY
	}
	return tacticalWidth, tacticalHeight
}

func overlayOffsets(classic bool) (int, int) {
	if classic {
		padY, padX := classicShellPad()
		return 22 - padX, 18 - padY
	}
	return 14, 10
}

// Overlay owns the non-activating transparent GPU floating HUD window.
type Overlay struct {
	win          *mygo.Window
	snap         engine.Snapshot
	shown        bool
	backdropOn   bool
	enterPending bool
	appliedX     int
	appliedY     int
	appliedW     int
	appliedH     int
	backdropBmp  *ui.Bitmap
	backdropMeta *backdropCapture
	capturedWinX int
	capturedWinY int
	hideSeq      uint64
	closing      bool
	closeOnce    sync.Once
}

// NewOverlay creates the click-through floating HUD window.
func NewOverlay() *Overlay {
	ensureAssets()

	initSnap := engine.Snapshot{
		Phase:   engine.PhaseIdle,
		Trusted: true,
		Loadout: catalog.DefaultSnapshot(),
	}
	classic := isClassicTheme(initSnap)
	initW, initH := overlayDimensions(initSnap)
	offX, offY := overlayOffsets(classic)

	disp := mygo.Screen.DisplayNearestPoint(mygo.Screen.CursorScreenPoint())
	initX := disp.WorkArea.X + offX
	initY := disp.WorkArea.Y + offY

	o := &Overlay{
		snap:     initSnap,
		appliedX: initX,
		appliedY: initY,
		appliedW: initW,
		appliedH: initH,
	}

	o.win = mygo.NewWindow(mygo.WindowOptions{
		Title:         "HelLauncher Tactical HUD",
		X:             initX,
		Y:             initY,
		Width:         initW,
		Height:        initH,
		Hidden:        true,
		Frameless:     true,
		Transparent:   true,
		AlwaysOnTop:   true,
		Panel:         true,
		DisableResize: true,
		DisableShadow: true,
		SkipTaskbar:   true,
		Content: ui.View(func(c *ui.Context) {
			o.renderView(c)
		}),
	})
	o.win.SetIgnoreMouseEvents(true)
	configureOverlayWindow(o.win)
	if classic {
		o.backdropOn = true
		setOverlayBackdrop(o.win, true)
	}
	o.win.OnClose(func(e *mygo.CloseEvent) {
		if !o.closing {
			e.PreventDefault()
		}
	})
	return o
}

func (o *Overlay) refreshBackdrop() {
	cap := captureWindowBackdrop(o.win, 540)
	if cap == nil || cap.img == nil {
		return
	}
	if o.backdropBmp == nil {
		o.backdropBmp = ui.NewBitmap(cap.img)
	} else {
		o.backdropBmp.Update(cap.img)
	}
	o.backdropMeta = cap
	o.capturedWinX = o.appliedX
	o.capturedWinY = o.appliedY
}

func (o *Overlay) renderView(c *ui.Context) {
	shiftX := float32(o.capturedWinX - o.appliedX)
	shiftY := float32(o.capturedWinY - o.appliedY)
	viewInternal(c, o.snap, &o.enterPending, o.backdropBmp, o.backdropMeta, shiftX, shiftY)
}

// Apply publishes a new Engine Snapshot onto the UI thread, resizing,
// refreshing Gaussian frosted backdrop when appearing, and animating cleanly
// in and out across both Classic and Tactical themes.
func (o *Overlay) Apply(snap engine.Snapshot) {
	if o == nil || o.win == nil {
		return
	}
	o.win.Update(func() {
		if o.closing {
			return
		}
		wasIdle := o.snap.Phase == engine.PhaseIdle
		wasClassic := isClassicTheme(o.snap)
		o.snap = snap
		classic := isClassicTheme(snap)

		if snap.Phase == engine.PhaseIdle {
			o.hideSeq++
			seq := o.hideSeq
			time.AfterFunc(200*time.Millisecond, func() {
				o.win.Update(func() {
					if !o.closing && o.hideSeq == seq && o.snap.Phase == engine.PhaseIdle && o.shown {
						o.shown = false
						o.win.Hide()
					}
				})
			})
			return
		}

		o.hideSeq++
		if o.backdropOn != classic {
			o.backdropOn = classic
			setOverlayBackdrop(o.win, classic)
		}

		wantW, wantH := overlayDimensions(snap)
		offX, offY := overlayOffsets(classic)
		if wasIdle || !o.shown || wasClassic != classic || o.appliedW != wantW || o.appliedH != wantH {
			disp := mygo.Screen.DisplayNearestPoint(mygo.Screen.CursorScreenPoint())
			wantX := disp.WorkArea.X + offX
			wantY := disp.WorkArea.Y + offY
			o.appliedX = wantX
			o.appliedY = wantY
			o.appliedW = wantW
			o.appliedH = wantH
			o.win.SetContentBounds(mygo.Rectangle{
				X:      wantX,
				Y:      wantY,
				Width:  wantW,
				Height: wantH,
			})
		}

		if !o.shown {
			if classic || o.backdropBmp == nil {
				o.refreshBackdrop()
			}
			o.enterPending = true
			o.shown = true
			o.win.ShowInactive()
		} else if wasClassic != classic {
			o.enterPending = true
		}
	})
}

// HideImmediate hides the floating HUD window immediately without waiting for
// fade-out timers (used when fullscreen desktop onboarding takes over).
func (o *Overlay) HideImmediate() {
	if o == nil || o.win == nil {
		return
	}
	o.win.Update(func() {
		if o.closing {
			return
		}
		o.hideSeq++
		o.snap.Phase = engine.PhaseIdle
		if o.shown {
			o.shown = false
			o.win.Hide()
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
	viewInternal(c, snap, nil, nil, nil, 0, 0)
}

func viewInternal(
	c *ui.Context,
	snap engine.Snapshot,
	enterSeed *bool,
	backdropBmp *ui.Bitmap,
	backdropMeta *backdropCapture,
	winShiftX, winShiftY float32,
) {
	ensureAssets()
	t := hudTheme()
	c.SetTheme(t)
	c.Root().Background(ui.Transparent)

	if isClassicTheme(snap) {
		renderClassicHUD(c, t, snap, false, enterSeed, backdropBmp, backdropMeta, winShiftX, winShiftY)
		return
	}
	renderTacticalHUD(c, t, snap, false, enterSeed)
}

// Preview renders a live, in-window preview of the HUD inside a parent UI
// (such as the Arsenal configurator), using GPU backdrop blur for the Classic
// frosted glass theme without mutating the host window's theme.
// Preview renders an interactive non-windowed HUD panel (used in Arsenal & static previews).
func Preview(c *ui.Context, snap engine.Snapshot) {
	PreviewAnimated(c, snap, nil)
}

// PreviewAnimated renders the in-app HUD panel and, when enterSeed points to
// true, replays a pop-up slide-in and fade-in entrance animation.
func PreviewAnimated(c *ui.Context, snap engine.Snapshot, enterSeed *bool) {
	ensureAssets()
	t := hudTheme()
	if snap.Phase == engine.PhaseIdle {
		snap.Phase = engine.PhaseOpen
	}
	ui.Themed(c.Key("hud-preview-theme"), t).Background(ui.Transparent).Children(func() {
		if isClassicTheme(snap) {
			renderClassicHUD(c, t, snap, true, enterSeed, nil, nil, 0, 0)
		} else {
			renderTacticalHUD(c, t, snap, true, enterSeed)
		}
	})
}

// renderClassicHUD renders the Helldivers 2 vanilla frosted Gaussian-blur panel:
// borderless translucent smoked glass, category-framed square icons on the left,
// crisp 2-line [Name / Gray-to-White Arrows or Status] on the right, and smooth
// slide + fade entry/exit transitions.
func renderClassicHUD(
	c *ui.Context,
	t *ui.Theme,
	snap engine.Snapshot,
	inAppPreview bool,
	enterSeed *bool,
	backdropBmp *ui.Bitmap,
	backdropMeta *backdropCapture,
	winShiftX, winShiftY float32,
) {
	if inAppPreview {
		// Material and LinearGradient share e.fill on a single node in mygo/ui, so
		// paint the Gaussian backdrop blur on an outer layer and the smoked frosted
		// gradient tint on the inner panel.
		wrap := ui.Column(c.Key("classic-blur-wrap")).
			Width(classicWidth).
			Clip().
			Material(glass.Blur{Radius: 16})
		if enterSeed != nil {
			if *enterSeed {
				*enterSeed = false
				wrap.Jump("vis", 0)
			}
			vis := wrap.Animate("vis", 1.0, 185*time.Millisecond)
			wrap.Opacity(vis).Translate((1.0-vis)*-24.0, (1.0-vis)*-6.0)
		}
		wrap.Children(func() {
			renderClassicPanelBody(c, snap, 0)
		})
		return
	}

	visibleTarget := float32(1.0)
	if snap.Phase == engine.PhaseIdle {
		visibleTarget = 0.0
	}

	padY, padX := classicShellPad()
	outer := ui.Column(c.Key("classic-shell")).
		Fill().
		Padding(float32(padY), float32(padX))

	if enterSeed != nil && *enterSeed && snap.Phase != engine.PhaseIdle {
		*enterSeed = false
		outer.Jump("vis", 0)
	}
	vis := outer.Animate("vis", visibleTarget, 165*time.Millisecond)
	if vis <= 0.01 && snap.Phase == engine.PhaseIdle {
		return
	}

	errProgress := outer.Animate("err-seq", float32(snap.ErrorSeq), 240*time.Millisecond)
	errFlash := clamp01(float32(snap.ErrorSeq) - errProgress)
	shakeX := float32(math.Sin(float64(errFlash)*math.Pi*4)) * 5.5 * errFlash

	slideDist := float32(-22.0)
	if runtime.GOOS == "darwin" {
		slideDist = -14.0
	}
	slideX := (1.0 - vis) * slideDist
	totalOffsetX := slideX + shakeX

	outer.Children(func() {
		card := ui.Column(c.Key("classic-card")).
			Width(classicWidth).
			Clip().
			Opacity(vis).
			Translate(totalOffsetX, 0)

		card.Children(func() {
			if backdropBmp != nil && backdropMeta != nil {
				bx := backdropMeta.offsetX + winShiftX - float32(padX) - totalOffsetX
				by := backdropMeta.offsetY + winShiftY - float32(padY)
				ui.Box(c.Key("win-backdrop")).
					Absolute().
					ZIndex(-1).
					Left(bx).
					Top(by).
					Size(backdropMeta.width, backdropMeta.height).
					Children(func() {
						ui.Image(c, backdropBmp).
							Size(backdropMeta.width, backdropMeta.height).
							Fit(ui.FillBox)
					})
			}
			renderClassicPanelBody(c, snap, errFlash)
		})
	})
}

func renderClassicPanelBody(c *ui.Context, snap engine.Snapshot, errFlash float32) {
	tr := i18n.For(snap.Loadout.Settings.Language)

	panel := ui.Column(c.Key("classic-hud")).
		Width(classicWidth).
		Padding(classicPadTop, 14, classicPadBottom, 10).
		Gap(classicRowGap).
		Clip()

	stepProgress := panel.Animate("step-seq", float32(snap.StepSeq), 140*time.Millisecond)
	stepPulse := clamp01(float32(snap.StepSeq) - stepProgress)

	if errFlash > 0.08 {
		panel.LinearGradient(ui.LinearGradient{
			Angle: 90,
			Stops: []ui.GradientStop{
				{Color: ui.RGBA(104, 28, 26, 0.58), At: 0},
				{Color: ui.RGBA(68, 22, 20, 0.38), At: 0.65},
				{Color: ui.RGBA(34, 20, 20, 0.16), At: 1},
			},
		})
	} else {
		panel.LinearGradient(ui.LinearGradient{
			Angle: 90,
			Stops: []ui.GradientStop{
				{Color: ui.RGBA(28, 31, 36, 0.50), At: 0},
				{Color: ui.RGBA(28, 31, 36, 0.36), At: 0.66},
				{Color: ui.RGBA(28, 31, 36, 0.14), At: 1},
			},
		})
	}

	isLockedPhase := snap.Phase == engine.PhaseArmed || snap.Phase == engine.PhaseDeploying || snap.Phase == engine.PhaseDeployed

	panel.Children(func() {
		if runtime.GOOS == "darwin" && !snap.Trusted {
			ui.Row(c.Key("classic-untrusted")).
				Height(30).
				Padding(0, 8).
				AlignItems(ui.Center).
				Background(ui.RGBA(145, 34, 28, 0.85)).
				Children(func() {
					ui.Text(c, tr.HUDUntrustedMac).FontSize(11).TextColor(ui.Hex("#ffe4e4")).SingleLine()
				})
		}

		list := ui.Column(c.Key("classic-list")).Gap(classicRowGap)
		list.Children(func() {
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

				targetOpacity := float32(1.0)
				if dimmed {
					targetOpacity = 0.32
				}

				row := ui.Row(c.Key("c-strat-" + st.ID)).
					Height(classicRowHeight).
					AlignItems(ui.Center).
					Gap(11)

				alpha := row.Animate("alpha", targetOpacity, 110*time.Millisecond)
				row.Opacity(alpha)

				isSystemOffline := st.ID == catalog.SystemJoinHelldiversID && !snap.HelldiversReady

				row.Children(func() {
					renderClassicIconBox(c, st, isTarget, dimmed, isSystemOffline, errFlash)

					nameCol := ui.Hex("#ffffff")
					if isSystemOffline && !isTarget && !(matchesPrefix && len(snap.Typed) > 0) {
						nameCol = ui.RGBA(240, 242, 245, 0.78)
					}
					ui.Column(c).Grow(1).MinWidth(0).Gap(2).Children(func() {
						renderCrispLabel(c.Key("name"), st.Name, 14.5, true, nameCol, ui.RGBA(0, 0, 0, 0.72))

						renderClassicSecondLine(c, st, snap, tr, isTarget, matchesPrefix, isSystemOffline, stepPulse)
					})
				})
			}

			if shown == 0 {
				ui.Row(c.Key("classic-empty")).Height(42).AlignItems(ui.Center).Children(func() {
					renderCrispLabel(c.Key("empty"), tr.HUDEmptyLoadout, 13, false, ui.RGBA(245, 245, 240, 0.85), ui.RGBA(0, 0, 0, 0.65))
				})
			}
		})
	})
}

func renderClassicIconBox(c *ui.Context, st catalog.Stratagem, isTarget, dimmed, systemOffline bool, errFlash float32) {
	border := CategoryBorderColor(st.Category)
	fill := CategoryFillColor(st.Category)
	iconAlpha := float32(1.0)

	switch {
	case errFlash > 0.08:
		border = ui.Hex("#ff5548")
		fill = ui.RGBA(110, 28, 24, 0.80)
	case isTarget:
		border = ui.Hex("#ffffff")
	case dimmed:
		border = ui.RGBA(186, 190, 196, 0.52)
		fill = ui.RGBA(48, 50, 54, 0.45)
	case systemOffline:
		// Matches vanilla Helldivers 2 "Unavailable" desaturated square frame.
		border = ui.RGBA(224, 224, 216, 0.72)
		fill = ui.RGBA(168, 168, 162, 0.24)
		iconAlpha = 0.52
	}

	box := ui.Row(c.Key("icon-box")).
		Size(42, 42).
		Shrink(0).
		Center().
		Background(fill).
		Border(2, border)

	if isTarget {
		box.Shadow(0, 0, 8, 0, ui.RGBA(255, 255, 255, 0.36))
	}

	box.Children(func() {
		if icon := lookupStratagemIcon(st.Icon); icon != nil {
			img := ui.Image(c, icon).Size(31, 31)
			if iconAlpha < 1 {
				img.Opacity(iconAlpha)
			}
		} else {
			ui.Text(c, "✪").FontSize(16).Bold().TextColor(ui.Hex("#ffffff"))
		}

		if st.Delivery == catalog.DeliveryBeaconClick {
			ui.Row(c.Key("beacon-tag")).
				Absolute().
				Right(-2).
				Bottom(-2).
				Size(13, 13).
				Center().
				Background(ui.Hex("#eae6d6")).
				Children(func() {
					ui.Text(c, "⊕").FontSize(9.5).Bold().TextColor(ui.Hex("#181a1c"))
				})
		}
	})
}

func renderClassicSecondLine(
	c *ui.Context,
	st catalog.Stratagem,
	snap engine.Snapshot,
	tr i18n.Strings,
	isTarget bool,
	matchesPrefix bool,
	systemOffline bool,
	stepPulse float32,
) {
	if isTarget {
		switch snap.Phase {
		case engine.PhaseArmed:
			row := ui.Row(c.Key("c-armed")).Height(18).AlignItems(ui.Center)
			pulse := row.Loop("armed-p", 900*time.Millisecond, ui.Linear)
			row.Opacity(0.78 + 0.22*float32(math.Sin(float64(pulse)*2*math.Pi))).Children(func() {
				renderCrispLabel(c.Key("txt"), tr.HUDArmedHint, 12, true, ui.Hex("#ffffff"), ui.RGBA(0, 0, 0, 0.75))
			})
			return

		case engine.PhaseDeploying:
			label := tr.HUDCallingIn
			if len(st.Steps) > 0 {
				switch st.Steps[0].Kind {
				case catalog.StepCloseActiveWindow:
					label = tr.HUDClosingWindow
				case catalog.StepLaunchHelldivers:
					label = tr.HUDHellpodLaunching
				case catalog.StepSystemAction:
					label = i18n.SystemActionName(catalog.SystemAction(st.Steps[0].Target), snap.Loadout.Settings.Language)
				}
			}
			ui.Row(c.Key("c-deploying")).Height(18).AlignItems(ui.Center).Gap(5).Children(func() {
				if classicSpinnerSVG != nil {
					sp := ui.Image(c.Key("sp"), classicSpinnerSVG).Size(11, 11)
					turn := sp.Loop("turn", 580*time.Millisecond, ui.Linear)
					sp.Rotate(turn * 360)
				}
				renderCrispLabel(c.Key("txt"), label, 12.5, true, ui.Hex("#ffffff"), ui.RGBA(0, 0, 0, 0.75))
			})
			return

		case engine.PhaseDeployed:
			ui.Row(c.Key("c-deployed")).Height(18).AlignItems(ui.Center).Children(func() {
				renderCrispLabel(c.Key("txt"), tr.HUDDeployedShort, 12.5, true, ui.Hex("#ffffff"), ui.RGBA(0, 0, 0, 0.75))
			})
			return
		}
	}

	if systemOffline && len(snap.Typed) == 0 {
		ui.Row(c.Key("c-offline")).Height(18).AlignItems(ui.Center).Children(func() {
			renderCrispLabel(c.Key("txt"), tr.HUDSystemOffline, 12, false, ui.RGBA(236, 238, 240, 0.88), ui.RGBA(0, 0, 0, 0.70))
		})
		return
	}

	ui.Row(c.Key("c-arrows")).Gap(5).Height(18).AlignItems(ui.Center).Children(func() {
		for idx, dir := range st.Code {
			matched := isTarget || (matchesPrefix && idx < len(snap.Typed))
			justMatched := !isTarget && matchesPrefix && len(snap.Typed) > 0 && idx == len(snap.Typed)-1 && stepPulse > 0.02
			renderDirectionArrow(c.Key(idx), dir, 14.5, matched, justMatched, stepPulse, true)
		}
	})
}

// renderTacticalHUD renders the Super Earth helmet-visor tactical theme.
func renderTacticalHUD(c *ui.Context, t *ui.Theme, snap engine.Snapshot, inAppPreview bool, enterSeed *bool) {
	tr := i18n.For(snap.Loadout.Settings.Language)

	visibleTarget := float32(1.0)
	if snap.Phase == engine.PhaseIdle && !inAppPreview {
		visibleTarget = 0.0
	}

	outer := ui.Column(c.Key("hud-shell"))
	if inAppPreview {
		// Fit cleanly inside the Arsenal live-preview stage while preserving exact proportions.
		outer.Width(328)
	} else {
		outer.Fill().Padding(18, 20)
	}

	gpuReady := ui.Local(outer, "gpu-accel", func() bool { return false })
	svcs := c.Services()
	outer.Draw(func(p *ui.Painter, _ ui.Rect) {
		if acc := p.Accelerated(); acc != *gpuReady {
			*gpuReady = acc
			svcs.Invalidate()
		}
	})

	if enterSeed != nil && *enterSeed && snap.Phase != engine.PhaseIdle {
		*enterSeed = false
		outer.Jump("vis", 0)
	}
	vis := outer.Animate("vis", visibleTarget, 165*time.Millisecond)
	if vis <= 0.01 && snap.Phase == engine.PhaseIdle && !inAppPreview {
		return
	}

	errProgress := outer.Animate("err-seq", float32(snap.ErrorSeq), 260*time.Millisecond)
	errFlash := clamp01(float32(snap.ErrorSeq) - errProgress)
	shakeX := float32(math.Sin(float64(errFlash)*math.Pi*4)) * 7.0 * errFlash

	stepProgress := outer.Animate("step-seq", float32(snap.StepSeq), 150*time.Millisecond)
	stepPulse := clamp01(float32(snap.StepSeq) - stepProgress)

	slideX := (1.0 - vis) * -34.0
	if inAppPreview {
		if enterSeed != nil {
			slideX = (1.0 - vis) * -28.0
		} else {
			slideX = 0
		}
	}

	cardWidth := float32(404)
	if inAppPreview {
		cardWidth = 324
	}

	outer.Children(func() {
		hudStack := ui.Column(c.Key("visor-stack")).
			Width(cardWidth).
			Padding(8, 12, 10, 10).
			Gap(5).
			Opacity(vis)

		if snap.Loadout.Settings.VisorCurved {
			hudStack.
				TransformOrigin(0, 0.16).
				RotateY(4.6).
				RotateX(-0.7).
				Perspective(1020).
				Translate(slideX+shakeX, 0)
			if *gpuReady && visorShader != nil {
				hudStack.Effect(visorShader.Effect(shader.Options{
					MaxSampleOffset: ui.Point{X: 18, Y: 18},
				}))
			}
		} else {
			hudStack.Translate(slideX+shakeX, 0)
		}

		hudStack.Children(func() {
			renderVisorCornerReticle(c)
			renderHeaderBar(c, t, snap, tr, errFlash)

			if runtime.GOOS == "darwin" && !snap.Trusted {
				renderUntrustedBanner(c, tr)
			}

			if snap.Phase == engine.PhaseArmed {
				renderArmedBanner(c, tr)
			}

			renderStratagemStrips(c, t, snap, tr, errFlash, stepPulse)
			renderLiveInputFooter(c, t, snap, tr, stepPulse)
		})
	})
}

func renderVisorCornerReticle(c *ui.Context) {
	ui.Row(c.Key("visor-reticle")).
		Height(12).
		AlignItems(ui.Center).
		Translate(-6, 1).
		DrawOver(func(p *ui.Painter, r ui.Rect) {
			shadowCol := ui.RGBA(0, 0, 0, 0.72)
			bracketCol := ui.RGBA(255, 233, 0, 0.88)
			dimCol := ui.RGBA(255, 255, 255, 0.48)
			p.Fill(ui.Rect{X: r.X + 1, Y: r.Y + 1, W: 9, H: 1.8}, shadowCol, 0)
			p.Fill(ui.Rect{X: r.X + 1, Y: r.Y + 1, W: 1.8, H: 16}, shadowCol, 0)
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

func renderHeaderBar(c *ui.Context, t *ui.Theme, snap engine.Snapshot, tr i18n.Strings, errFlash float32) {
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

	badgeText, badgeBg, badgeBorder, badgeFg := headerBadgeStyle(snap, tr, errFlash)

	hdr.Children(func() {
		if emblemSVG != nil {
			ui.Image(c, emblemSVG).Size(19, 19)
		}
		ui.Text(c, tr.HUDTitle).
			FontSize(15).
			Bold().
			TextColor(ui.Hex("#ffffff")).
			TextShadow(0, 1, 3, ui.RGBA(0, 0, 0, 0.92)).
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

func headerBadgeStyle(snap engine.Snapshot, tr i18n.Strings, errFlash float32) (label string, bg, border, fg ui.Color) {
	if errFlash > 0.08 {
		return tr.HUDBadgeError, ui.RGBA(255, 59, 48, 0.30), ui.Hex("#ff6b6b"), ui.Hex("#ffffff")
	}
	switch snap.Phase {
	case engine.PhaseArmed:
		return tr.HUDBadgeArmed, ui.Hex("#ffe900"), ui.Hex("#ffffff"), ui.Hex("#0b0f14")
	case engine.PhaseDeploying:
		return tr.HUDBadgeDeploying, ui.RGBA(255, 233, 0, 0.28), ui.Hex("#ffe900"), ui.Hex("#fffbe0")
	case engine.PhaseDeployed:
		return tr.HUDBadgeDeployed, ui.Hex("#ffe900"), ui.Hex("#ffffff"), ui.Hex("#0b0f14")
	case engine.PhasePreview:
		return tr.HUDBadgePreview, ui.RGBA(79, 179, 208, 0.22), ui.Hex("#7dd3fc"), ui.Hex("#e0f2fe")
	default:
		if len(snap.Typed) > 0 {
			return fmt.Sprintf(tr.HUDInputCountFmt, len(snap.Typed)), ui.RGBA(255, 233, 0, 0.22), ui.Hex("#ffe900"), ui.Hex("#ffe900")
		}
		return "WASD // ↑↓←→", ui.RGBA(255, 233, 0, 0.16), ui.Hex("#ffe900"), ui.Hex("#ffe900")
	}
}

func renderUntrustedBanner(c *ui.Context, tr i18n.Strings) {
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
		ui.Text(c, tr.HUDUntrustedMac).
			FontSize(11).
			TextColor(ui.Hex("#ffe4e4"))
	})
}

func renderArmedBanner(c *ui.Context, tr i18n.Strings) {
	banner := ui.Row(c.Key("armed-banner")).
		Padding(6, 10, 6, 12).
		Background(ui.RGBA(46, 40, 10, 0.94)).
		Border(1, ui.RGBA(255, 233, 0, 0.42)).
		Shadow(0, 0, 16, 0, ui.RGBA(255, 233, 0, 0.30))
	paintLeftBar(banner, 3.5, ui.Hex("#ffe900"))
	pulse := banner.Loop("armed-pulse", 900*time.Millisecond, ui.Linear)
	banner.Opacity(0.80 + 0.20*float32(math.Sin(float64(pulse)*2*math.Pi))).Children(func() {
		ui.Text(c, "⊕ "+tr.HUDArmedHint).
			FontSize(11.5).
			Bold().
			TextColor(ui.Hex("#ffe900"))
	})
}

func renderStratagemStrips(c *ui.Context, t *ui.Theme, snap engine.Snapshot, tr i18n.Strings, errFlash, stepPulse float32) {
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
				targetOpacity = 0.28
			}

			strip := ui.Column(c.Key("strat-"+st.ID)).
				Padding(5, 10, 7, 9).
				Gap(4).
				Clip().
				Border(1, ui.RGBA(255, 255, 255, 0.08)).
				Shadow(0, 4, 14, 0, ui.RGBA(0, 0, 0, 0.45))

			shiftX := strip.Animate("shift", targetShift, 130*time.Millisecond)
			contentAlpha := strip.Animate("alpha", targetOpacity, 120*time.Millisecond)
			strip.Translate(shiftX, 0)

			switch {
			case errFlash > 0.08:
				strip.Background(ui.RGBA(128, 22, 22, 0.86))
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
			case dimmed:
				strip.Background(ui.RGBA(14, 18, 23, 0.78))
			default:
				strip.Background(t.Surface)
			}

			isSystemOffline := st.ID == catalog.SystemJoinHelldiversID && !snap.HelldiversReady

			strip.Children(func() {
				ui.Row(c).AlignItems(ui.Center).Gap(9).Opacity(contentAlpha).Children(func() {
					renderCategoryIconBox(c, st, isTarget, isSystemOffline)

					ui.Column(c).Grow(1).MinWidth(0).Gap(3).Children(func() {
						ui.Row(c).AlignItems(ui.Center).Gap(6).Children(func() {
							ui.Text(c, st.Name).
								FontSize(14).
								Bold().
								TextColor(ui.Hex("#ffffff")).
								TextShadow(0, 1, 3, ui.RGBA(0, 0, 0, 0.95)).
								SingleLine().
								Grow(1)

							renderRowStatus(c, st, isTarget, isSystemOffline, snap.Phase, tr, t)
						})

						ui.Row(c).Gap(5).Height(18).AlignItems(ui.Center).Children(func() {
							for idx, dir := range st.Code {
								matched := isTarget || (matchesPrefix && idx < len(snap.Typed))
								justMatched := !isTarget && matchesPrefix && len(snap.Typed) > 0 && idx == len(snap.Typed)-1 && stepPulse > 0.02
								renderDirectionArrow(c.Key(idx), dir, 14.5, matched, justMatched, stepPulse, false)
							}
							if st.Delivery == catalog.DeliveryBeaconClick {
								ui.Spacer(c)
								ui.Text(c, "⊕").FontSize(11).Bold().TextColor(ui.Hex("#7dd3fc"))
							}
						})
					})
				})

				if isTarget && snap.Phase == engine.PhaseDeploying {
					track := ui.Row(c.Key("deploy-sweep")).
						Absolute().
						Left(0).
						Right(0).
						Bottom(0).
						Height(3).
						Clip().
						Background(ui.RGBA(255, 233, 0, 0.18))
					prog := track.Loop("sweep", 620*time.Millisecond, ui.Linear)
					track.Children(func() {
						ui.Box(c).
							WidthPercent(46).
							Height(3).
							LinearGradient(ui.LinearGradient{
								Angle: 90,
								Stops: []ui.GradientStop{
									{Color: ui.RGBA(255, 233, 0, 0.0), At: 0},
									{Color: ui.Hex("#ffe900"), At: 0.42},
									{Color: ui.Hex("#ffffff"), At: 1.0},
								},
							}).
							Shadow(0, 0, 8, 0, ui.RGBA(255, 233, 0, 0.92)).
							TranslatePercent(-100+prog*320, 0)
					})
				} else if isTarget && snap.Phase == engine.PhaseDeployed {
					ui.Box(c.Key("deploy-done")).
						Absolute().
						Left(0).
						Right(0).
						Bottom(0).
						Height(3).
						Background(t.Accent).
						Shadow(0, 0, 10, 0, ui.RGBA(255, 233, 0, 0.90))
				}
			})
		}

		if shown == 0 {
			ui.Row(c).Padding(10, 12).Background(t.Surface).Children(func() {
				ui.Text(c, tr.HUDEmptyLoadout).
					FontSize(12).
					TextColor(t.TextMuted)
			})
		}
	})
}

func renderCategoryIconBox(c *ui.Context, st catalog.Stratagem, isTarget, systemOffline bool) {
	border := CategoryBorderColor(st.Category)
	iconAlpha := float32(1.0)
	if isTarget {
		border = ui.Hex("#ffe900")
	} else if systemOffline {
		border = ui.RGBA(201, 178, 104, 0.45)
		iconAlpha = 0.56
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
			img := ui.Image(c, icon).Size(32, 32)
			if iconAlpha < 1 {
				img.Opacity(iconAlpha)
			}
		} else {
			ui.Text(c, "✪").FontSize(16).Bold().TextColor(ui.Hex("#ffe900"))
		}
	})
}

func renderRowStatus(c *ui.Context, st catalog.Stratagem, isTarget, systemOffline bool, phase engine.Phase, tr i18n.Strings, t *ui.Theme) {
	if !isTarget {
		if systemOffline {
			ui.Row(c.Key("offline-tag")).
				Padding(1, 5).
				Background(ui.RGBA(201, 178, 104, 0.16)).
				Border(1, ui.RGBA(201, 178, 104, 0.48)).
				Children(func() {
					ui.Text(c, tr.HUDSystemOfflineBadge).FontSize(9.5).Bold().TextColor(ui.Hex("#e6d18a")).SingleLine()
				})
		}
		return
	}
	switch phase {
	case engine.PhaseArmed:
		ui.Text(c, tr.HUDBadgeArmed).FontSize(10.5).Bold().TextColor(t.Accent).SingleLine()
	case engine.PhaseDeploying:
		label := tr.HUDCallingIn
		if len(st.Steps) > 0 {
			switch st.Steps[0].Kind {
			case catalog.StepCloseActiveWindow:
				label = tr.HUDClosingWindow
			case catalog.StepLaunchHelldivers:
				label = tr.HUDHellpodLaunching
			}
		}
		ui.Row(c.Key("spin-row")).AlignItems(ui.Center).Gap(5).Children(func() {
			if spinnerSVG != nil {
				sp := ui.Image(c.Key("sp"), spinnerSVG).Size(10, 10)
				turn := sp.Loop("turn", 580*time.Millisecond, ui.Linear)
				sp.Rotate(turn * 360)
			}
			ui.Text(c, label).FontSize(10.5).Bold().TextColor(t.Accent).SingleLine()
		})
	case engine.PhaseDeployed:
		ui.Text(c, tr.HUDDeployedShort).FontSize(10.5).Bold().TextColor(t.Accent).SingleLine()
	}
}

func renderLiveInputFooter(c *ui.Context, t *ui.Theme, snap engine.Snapshot, tr i18n.Strings, stepPulse float32) {
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
			ui.Text(c, tr.HUDInputLabel).FontSize(11).TextColor(ui.RGBA(255, 255, 255, 0.55))
			if len(snap.Typed) == 0 {
				ui.Text(c, tr.HUDWaitingInput).FontSize(11).TextColor(ui.RGBA(255, 255, 255, 0.36))
			} else {
				for idx, dir := range snap.Typed {
					just := idx == len(snap.Typed)-1 && stepPulse > 0.02
					renderDirectionArrow(c.Key(idx), dir, 14, true, just, stepPulse, false)
				}
			}
		})
		ui.Text(c, footerHint(snap, tr)).FontSize(10.5).Bold().TextColor(ui.RGBA(255, 233, 0, 0.82)).SingleLine()
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

func renderCrispLabel(c *ui.Context, text string, size float32, bold bool, fg, _ ui.Color) {
	main := ui.Text(c, text).FontSize(size).TextColor(fg).SingleLine()
	if bold {
		main.Bold()
	}
}

func drawArrowShape(p *ui.Painter, ox, oy, sz float32, dir catalog.Dir, col ui.Color) {
	if col.A == 0 || sz <= 0 {
		return
	}
	// Render the chunky Helldivers 90° arrowhead as an axis-clipped 45° SDF quad
	// plus a stem rect rather than FillPath, avoiding CPU path-mask atlas contention
	// when HUD and Arsenal swapchains render simultaneously on frame 0.
	const wingSide = 0.62225397 // 0.44 * sqrt(2)
	side := sz * wingSide
	half := side * 0.5

	var (
		stem   ui.Rect
		clip   ui.Rect
		cx, cy float32
	)
	switch dir {
	case catalog.DirUp:
		stem = ui.Rect{X: ox + sz*0.29, Y: oy + sz*0.52, W: sz * 0.42, H: sz * 0.42}
		clip = ui.Rect{X: ox, Y: oy, W: sz, H: sz * 0.52}
		cx, cy = ox+sz*0.50, oy+sz*0.52
	case catalog.DirDown:
		stem = ui.Rect{X: ox + sz*0.29, Y: oy + sz*0.06, W: sz * 0.42, H: sz * 0.42}
		clip = ui.Rect{X: ox, Y: oy + sz*0.48, W: sz, H: sz * 0.52}
		cx, cy = ox+sz*0.50, oy+sz*0.48
	case catalog.DirRight:
		stem = ui.Rect{X: ox + sz*0.06, Y: oy + sz*0.29, W: sz * 0.42, H: sz * 0.42}
		clip = ui.Rect{X: ox + sz*0.48, Y: oy, W: sz * 0.52, H: sz}
		cx, cy = ox+sz*0.48, oy+sz*0.50
	default: // DirLeft
		stem = ui.Rect{X: ox + sz*0.52, Y: oy + sz*0.29, W: sz * 0.42, H: sz * 0.42}
		clip = ui.Rect{X: ox, Y: oy, W: sz * 0.52, H: sz}
		cx, cy = ox+sz*0.52, oy+sz*0.50
	}

	p.Fill(stem, col, 0)
	p.Clip(clip, 0, func() {
		p.Transform(ui.Rotation(45), cx, cy, func() {
			p.Fill(ui.Rect{X: cx - half, Y: cy - half, W: side, H: side}, col, 0)
		})
	})
}

func paintArrowElement(c *ui.Context, dir catalog.Dir, size float32, fg, shadow ui.Color, scale float32) {
	el := ui.Box(c).Size(size, size).Shrink(0)
	if scale != 1 {
		el.Scale(scale, scale)
	}
	el.Draw(func(p *ui.Painter, r ui.Rect) {
		sz := min(r.W, r.H)
		if shadow.A > 0 {
			drawArrowShape(p, r.X+0.7, r.Y+1.0, sz, dir, shadow)
		}
		drawArrowShape(p, r.X, r.Y, sz, dir, fg)
	})
}

func renderDirectionArrow(c *ui.Context, dir catalog.Dir, size float32, matched, justMatched bool, stepPulse float32, classic bool) {
	if classic {
		// Vanilla Helldivers 2 HUD: untyped arrows are gray; matched arrows turn pure white.
		col := ui.Hex("#9da3ab")
		shadow := ui.RGBA(0, 0, 0, 0.48)
		scale := float32(1.0)
		if matched {
			col = ui.Hex("#ffffff")
			shadow = ui.RGBA(0, 0, 0, 0.72)
			if justMatched {
				scale = 1.0 + 0.30*stepPulse
			}
		}
		paintArrowElement(c, dir, size, col, shadow, scale)
		return
	}

	col := ui.RGBA(255, 255, 255, 0.44)
	shadow := ui.RGBA(0, 0, 0, 0.62)
	scale := float32(1.0)
	if matched {
		col = ui.Hex("#ffe900")
		shadow = ui.RGBA(0, 0, 0, 0.75)
		if justMatched {
			col = col.Mix(ui.Hex("#ffffff"), stepPulse)
			scale = 1.0 + 0.32*stepPulse
		}
	}
	paintArrowElement(c, dir, size, col, shadow, scale)
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

func footerHint(snap engine.Snapshot, tr i18n.Strings) string {
	switch snap.Phase {
	case engine.PhaseArmed:
		return tr.HUDFooterArmed
	case engine.PhaseDeploying:
		return tr.HUDFooterDeploy
	case engine.PhaseDeployed:
		return tr.HUDFooterDone
	case engine.PhasePreview:
		switch snap.Loadout.Settings.TriggerMode {
		case catalog.ModeHoldLeftCtrl:
			return tr.HUDFooterHoldLeft
		case catalog.ModeRightCtrlOrAlt:
			return tr.HUDFooterRightAlt
		default:
			return tr.HUDFooterDouble
		}
	default:
		return tr.HUDFooterCancel
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
