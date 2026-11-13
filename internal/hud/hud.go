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

	"helldriver/internal/catalog"
	"helldriver/internal/engine"
)

//go:embed assets/fonts/*.otf assets/icons/*
var assetsFS embed.FS

const (
	overlayWidth  = 440
	overlayHeight = 560
)

var (
	assetsOnce  sync.Once
	arrowSVG    *ui.SVG
	skullBitmap *ui.Bitmap
	svgIcons    map[string]*ui.SVG
)

const arrowPolygonSVG = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 20 20" fill="currentColor"><polygon points="13 20 13 15 20 15 20 5 13 5 13 0 0 10 13 20"/></svg>`

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
			switch strings.ToLower(path.Ext(name)) {
			case ".svg":
				parsed, pErr := ui.ParseSVG(data)
				if pErr != nil {
					slog.Warn("hud parse svg failed", "icon", name, "err", pErr)
					continue
				}
				svgIcons[name] = parsed
			case ".png":
				if name == "skull-header.png" {
					if bmp, bErr := ui.DecodeBitmap(data); bErr == nil {
						skullBitmap = bmp
					}
				}
			}
		}
	})
}

func hudTheme() *ui.Theme {
	t := ui.DarkTheme()
	accent := ui.Hex("#ffe900")
	t.Background = ui.RGBA(11, 14, 19, 0.92)
	t.Surface = ui.RGBA(20, 25, 33, 0.94)
	t.SurfaceHover = ui.RGBA(28, 35, 46, 0.96)
	t.SurfacePressed = ui.RGBA(34, 42, 54, 0.98)
	t.Border = ui.RGBA(255, 233, 0, 0.34)
	t.Text = ui.Hex("#f3f5f8")
	t.TextMuted = ui.Hex("#939cab")
	t.Accent = accent
	t.AccentHover = accent
	t.AccentPressed = accent
	t.AccentText = ui.Hex("#0b0d12")
	// Critical: overlay uses ShowInactive(), so InactiveAccent MUST equal Accent.
	t.InactiveAccent = accent
	t.Danger = ui.Hex("#ff453a")
	t.Success = ui.Hex("#42d674")
	t.Warning = accent
	t.Radius = 4
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
	initX := disp.WorkArea.X + 16
	initY := disp.WorkArea.Y + 12

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
				o.win.SetPosition(disp.WorkArea.X+16, disp.WorkArea.Y+12)
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

	outer := ui.Column(c.Key("hud-shell")).Fill().Padding(14, 16)
	vis := outer.Animate("vis", visibleTarget, 150*time.Millisecond)
	if vis <= 0.01 && snap.Phase == engine.PhaseIdle {
		return
	}

	errProgress := outer.Animate("err-seq", float32(snap.ErrorSeq), 260*time.Millisecond)
	errFlash := clamp01(float32(snap.ErrorSeq) - errProgress)
	shakeX := float32(math.Sin(float64(errFlash)*math.Pi*4)) * 7.0 * errFlash
	slideX := (1.0 - vis) * -28.0

	outer.Children(func() {
		cardBorder := ui.RGBA(255, 233, 0, 0.42)
		switch {
		case errFlash > 0.05:
			cardBorder = ui.Hex("#ff453a")
		case snap.Phase == engine.PhaseArmed:
			cardBorder = ui.Hex("#ffe900")
		case snap.Phase == engine.PhaseDeployed:
			cardBorder = ui.Hex("#42d674")
		}

		card := ui.Column(c.Key("visor-card")).
			Width(392).
			Background(t.Background).
			Border(1.5, cardBorder).
			BorderStartWidth(4).
			Radius(4).
			Opacity(vis).
			Padding(12, 14).
			Gap(8)

		if snap.Loadout.Settings.VisorCurved {
			card.TransformOrigin(0, 0.5).
				Perspective(920).
				RotateY(8).
				Translate(slideX+shakeX, 0)
		} else {
			card.Translate(slideX+shakeX, 0)
		}

		card.Children(func() {
			renderHeader(c, t, snap, errFlash)

			if runtime.GOOS == "darwin" && !snap.Trusted {
				renderUntrustedBanner(c)
			}

			if snap.Phase == engine.PhaseArmed {
				renderArmedBanner(c)
			}

			renderStratagemRows(c, t, snap)
			renderLiveTypedFooter(c, t, snap)
		})
	})
}

func renderHeader(c *ui.Context, t *ui.Theme, snap engine.Snapshot, errFlash float32) {
	badgeText, badgeBg, badgeFg := headerBadge(snap, errFlash)

	ui.Row(c.Key("hdr")).AlignItems(ui.Center).Gap(8).Children(func() {
		if skullBitmap != nil {
			ui.Image(c, skullBitmap).Size(22, 22)
		}
		ui.Column(c).Grow(1).Gap(1).Children(func() {
			ui.Text(c, "STRATAGEMS // 战略配备").FontSize(14).Bold().TextColor(t.Accent)
			ui.Text(c, "SUPER EARTH TACTICAL TERMINAL").FontSize(9).TextColor(t.TextMuted)
		})
		ui.Row(c).
			Padding(3, 8).
			Radius(2).
			Background(badgeBg).
			Children(func() {
				ui.Text(c, badgeText).FontSize(11).Bold().TextColor(badgeFg)
			})
	})
	ui.Box(c).Height(1).Background(ui.RGBA(255, 233, 0, 0.22))
}

func headerBadge(snap engine.Snapshot, errFlash float32) (label string, bg, fg ui.Color) {
	if errFlash > 0.1 {
		return "指令错误", ui.Hex("#ff453a"), ui.Hex("#ffffff")
	}
	switch snap.Phase {
	case engine.PhaseArmed:
		return "信标已握持", ui.Hex("#ffe900"), ui.Hex("#0b0d12")
	case engine.PhaseDeploying:
		return "空投部署中", ui.RGBA(255, 233, 0, 0.22), ui.Hex("#ffe900")
	case engine.PhaseDeployed:
		return "✓ 已就绪", ui.RGBA(66, 214, 116, 0.24), ui.Hex("#42d674")
	case engine.PhasePreview:
		return "终端就绪", ui.RGBA(255, 255, 255, 0.10), ui.Hex("#e5e8ef")
	default:
		if len(snap.Typed) > 0 {
			return fmt.Sprintf("输入中 %d", len(snap.Typed)), ui.RGBA(255, 233, 0, 0.20), ui.Hex("#ffe900")
		}
		return "WASD // ↑↓←→", ui.RGBA(255, 255, 255, 0.08), ui.Hex("#cfd5df")
	}
}

func renderUntrustedBanner(c *ui.Context) {
	ui.Row(c.Key("untrusted-banner")).
		Padding(6, 10).
		Radius(3).
		Background(ui.RGBA(255, 69, 58, 0.18)).
		Border(1, ui.RGBA(255, 69, 58, 0.55)).
		Children(func() {
			ui.Text(c, "⚠ 未获辅助功能权限：请在菜单栏点击「授予辅助功能权限…」").
				FontSize(11).
				TextColor(ui.Hex("#ffb4ae"))
		})
}

func renderArmedBanner(c *ui.Context) {
	banner := ui.Row(c.Key("armed-banner")).
		Padding(7, 10).
		Radius(3).
		Background(ui.RGBA(255, 233, 0, 0.16)).
		Border(1, ui.Hex("#ffe900"))
	pulse := banner.Loop("armed-pulse", 900*time.Millisecond, ui.Linear)
	alpha := 0.72 + 0.28*float32(math.Sin(float64(pulse)*2*math.Pi))
	banner.Opacity(alpha).Children(func() {
		ui.Text(c, "⊕ 信标已握持 · 左键点击目标位置空投 · 右键/ESC 取消").
			FontSize(11.5).
			Bold().
			TextColor(ui.Hex("#ffe900"))
	})
}

func renderStratagemRows(c *ui.Context, t *ui.Theme, snap engine.Snapshot) {
	isLockedPhase := snap.Phase == engine.PhaseArmed || snap.Phase == engine.PhaseDeploying || snap.Phase == engine.PhaseDeployed

	ui.Column(c.Key("strat-list")).Gap(6).Children(func() {
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

			rowBg := ui.RGBA(22, 27, 36, 0.86)
			rowBorder := ui.RGBA(255, 255, 255, 0.08)
			switch {
			case isTarget && snap.Phase == engine.PhaseDeployed:
				rowBg = ui.RGBA(22, 52, 36, 0.90)
				rowBorder = ui.Hex("#42d674")
			case isTarget:
				rowBg = ui.RGBA(54, 49, 16, 0.90)
				rowBorder = ui.Hex("#ffe900")
			case len(snap.Typed) > 0 && matchesPrefix:
				rowBg = ui.RGBA(32, 37, 46, 0.95)
				rowBorder = ui.RGBA(255, 233, 0, 0.45)
			}

			rowOpacity := float32(1.0)
			if dimmed {
				rowOpacity = 0.34
			}

			row := ui.Column(c.Key("strat-"+st.ID)).
				Padding(7, 9).
				Gap(5).
				Radius(3).
				Background(rowBg).
				Border(1, rowBorder).
				Opacity(rowOpacity)

			row.Children(func() {
				ui.Row(c).AlignItems(ui.Center).Gap(9).Children(func() {
					renderCategoryIconBox(c, st)

					ui.Column(c).Grow(1).Gap(3).Children(func() {
						ui.Row(c).AlignItems(ui.Center).Gap(6).Children(func() {
							ui.Text(c, st.Name).FontSize(13).Bold().TextColor(t.Text).SingleLine().Grow(1)
							subText, subCol := rowSubtitle(st, isTarget, snap.Phase, t)
							ui.Text(c, subText).FontSize(10.5).TextColor(subCol).SingleLine()
						})

						ui.Row(c).Gap(5).AlignItems(ui.Center).Children(func() {
							for idx, dir := range st.Code {
								matched := isTarget || (matchesPrefix && idx < len(snap.Typed))
								col := ui.Hex("#d8dde6")
								if matched {
									col = t.Accent
								}
								renderDirectionArrow(c.Key(idx), dir, 13, col)
							}
							if st.Delivery == catalog.DeliveryBeaconClick {
								ui.Text(c, "⊕信标").FontSize(9.5).TextColor(ui.Hex("#9ad7ff"))
							}
						})
					})
				})

				if isTarget && snap.Phase == engine.PhaseDeploying {
					track := ui.Row(c.Key("deploy-sweep")).Height(3).Radius(2).Background(ui.RGBA(255, 233, 0, 0.18))
					prog := track.Loop("sweep", 650*time.Millisecond, ui.Linear)
					track.Children(func() {
						ui.Box(c).
							WidthPercent(42).
							Height(3).
							Radius(2).
							Background(t.Accent).
							TranslatePercent(prog*140, 0)
					})
				} else if isTarget && snap.Phase == engine.PhaseDeployed {
					ui.Box(c.Key("deploy-done")).Height(3).Radius(2).Background(ui.Hex("#42d674"))
				}
			})
		}

		if shown == 0 {
			ui.Text(c, "暂无已启用的战略配备，请在菜单栏打开「军械库」配置。").
				FontSize(12).
				TextColor(t.TextMuted).
				Padding(10, 4)
		}
	})
}

func renderCategoryIconBox(c *ui.Context, st catalog.Stratagem) {
	bg, border := categoryPalette(st.Category)
	ui.Row(c).
		Size(36, 36).
		Center().
		Radius(3).
		Background(bg).
		Border(1, border).
		Children(func() {
			if icon := lookupStratagemIcon(st.Icon); icon != nil {
				ui.Image(c, icon).Size(24, 24)
			} else {
				ui.Text(c, "✪").FontSize(15).Bold().TextColor(ui.Hex("#ffe900"))
			}
		})
}

func renderLiveTypedFooter(c *ui.Context, t *ui.Theme, snap engine.Snapshot) {
	ui.Box(c).Height(1).Background(ui.RGBA(255, 255, 255, 0.10))
	ui.Row(c.Key("footer")).AlignItems(ui.Center).Gap(8).Children(func() {
		ui.Row(c).Grow(1).Gap(5).AlignItems(ui.Center).Children(func() {
			if len(snap.Typed) == 0 {
				ui.Text(c, "等待输入方向指令…").FontSize(11).TextColor(t.TextMuted)
			} else {
				for idx, dir := range snap.Typed {
					renderDirectionArrow(c.Key(idx), dir, 14, t.Accent)
				}
			}
		})
		ui.Text(c, footerHint(snap)).FontSize(10.5).TextColor(t.TextMuted)
	})
}

func renderDirectionArrow(c *ui.Context, dir catalog.Dir, size float32, col ui.Color) {
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
	ui.Icon(c, arrowSVG).FontSize(size).Rotate(rot).TextColor(col)
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

func categoryPalette(cat catalog.Category) (bg, border ui.Color) {
	switch cat {
	case catalog.CategoryRed:
		return ui.RGBA(145, 38, 34, 0.36), ui.Hex("#e05347")
	case catalog.CategoryGreen:
		return ui.RGBA(56, 118, 58, 0.36), ui.Hex("#67b85c")
	case catalog.CategoryYellow:
		return ui.RGBA(150, 126, 24, 0.36), ui.Hex("#e5c538")
	default:
		return ui.RGBA(36, 98, 134, 0.36), ui.Hex("#4fb3d0")
	}
}

func rowSubtitle(st catalog.Stratagem, isTarget bool, phase engine.Phase, t *ui.Theme) (string, ui.Color) {
	if isTarget {
		switch phase {
		case engine.PhaseArmed:
			return "等待左键投掷信标…", t.Accent
		case engine.PhaseDeploying:
			if len(st.Steps) > 0 && st.Steps[0].Kind == catalog.StepCloseActiveWindow {
				return "正在执行清场…", t.Accent
			}
			return "正在唤醒窗口…", t.Accent
		case engine.PhaseDeployed:
			return "✓ 已就绪 DEPLOYED", ui.Hex("#42d674")
		}
	}
	return st.Subtitle, t.TextMuted
}

func footerHint(snap engine.Snapshot) string {
	switch snap.Phase {
	case engine.PhaseArmed:
		return "左键空投 · 右键/ESC 取消"
	case engine.PhaseDeploying:
		return "战备空投部署中…"
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
