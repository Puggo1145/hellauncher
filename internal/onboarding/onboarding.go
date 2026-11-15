package onboarding

import (
	"fmt"
	"runtime"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/shader"
	"github.com/egoist/mygo/ui/video"

	"hellauncher/internal/catalog"
	"hellauncher/internal/engine"
	"hellauncher/internal/hud"
	"hellauncher/internal/i18n"
	"hellauncher/internal/sfx"
)

const (
	stageBroadcast = 0
	stageTheme     = 1
	stageTrigger   = 2
	stageLoadout   = 3
	stageDrill     = 4
)

var drillTargetCode = []catalog.Dir{
	catalog.DirDown,
	catalog.DirDown,
	catalog.DirUp,
	catalog.DirRight,
}

// Hooks connects the single-canvas fullscreen translucent Onboarding flow to
// the live global hotkey engine and OS permissions.
type Hooks struct {
	OnOpen             func()
	OnClose            func(completed bool)
	SetLiveDesktopHUD  func(active bool)
	GuidePermission    func()
	IsTrusted          func() bool
	GetOpenAtLogin     func() bool
	SetOpenAtLogin     func(open bool) error
	OnHelldiversStatus func(ready bool)
}

// Manager orchestrates HelLauncher's seamless single-layer desktop enlistment:
//   - Stage 0: Fullscreen translucent 1600x900 Helldivers 2 montage dissolving
//     into the authentic Super Earth Hero Logo & angular CTA bar.
//   - Stage 1: HUD Theme (1 focus, paired live with the top-left HUD).
//   - Stage 2: Summon Key (1 focus, paired live with the top-left HUD).
//   - Stage 3: Loadout Pods (1 focus, 1-click Stratagem grid updating the live HUD).
//   - Stage 4: Call In Resupply (1 focus, visual equation + live arrow feedback).
type Manager struct {
	cat   *catalog.Catalog
	spk   sfx.Speaker
	hooks Hooks

	win *mygo.Window

	stage              int
	broadcastStart     time.Time
	broadcastClockInit bool
	heroRevealed       bool
	muted              bool

	// Live calibration state synced immediately with Catalog
	lang        catalog.Language
	langIdx     int
	triggerMode catalog.TriggerMode
	hudTheme    catalog.HUDTheme
	shipName    string

	// Persistent GPU video player & bitmaps (UI thread)
	videoPlayer      *ui.VideoPlayer
	videoInitDone    bool
	posterBmp        *ui.Bitmap
	heroLogoBmp      *ui.Bitmap
	hudEnterPending  bool
	termEnterPending bool

	// Live engine & drill state (UI thread)
	engSnap     engine.Snapshot
	hasEngSnap  bool
	prevErrSeq  uint32
	drillStep   int
	drillDone   bool
	drillMiss   bool
	drillStepSq uint32
	drillErrSq  uint32

	audioMu    sync.Mutex
	audioTimer *time.Timer
}

// New constructs a single-canvas translucent Onboarding Manager backed by cat and spk.
func New(cat *catalog.Catalog, spk sfx.Speaker, hooks Hooks) *Manager {
	ensureMontageAssets()
	_ = hud.EmblemSVG() // Ensures FS Sinclair font & Stratagem SVGs are registered
	m := &Manager{
		cat:   cat,
		spk:   spk,
		hooks: hooks,
	}
	m.resetFromCatalog()
	return m
}

func (m *Manager) resetFromCatalog() {
	snap := m.cat.Snapshot()
	s := snap.Settings

	m.stage = stageBroadcast
	m.broadcastStart = time.Now()
	m.broadcastClockInit = false
	m.heroRevealed = false
	m.muted = s.Muted
	if s.Language == catalog.LangEN {
		m.lang = catalog.LangEN
		m.langIdx = 1
	} else {
		m.lang = catalog.LangZH
		m.langIdx = 0
	}
	if s.TriggerMode == "" {
		m.triggerMode = catalog.ModeDoubleHoldCtrl
	} else {
		m.triggerMode = s.TriggerMode
	}
	if s.HUDTheme == "" {
		m.hudTheme = catalog.ThemeClassic
	} else {
		m.hudTheme = s.HUDTheme
	}
	if strings.TrimSpace(s.ShipName) != "" {
		m.shipName = s.ShipName
	} else {
		m.shipName = catalog.DefaultShipName(m.lang)
	}

	m.hasEngSnap = false
	m.drillStep = 0
	m.drillDone = false
	m.drillMiss = false
}

// IsOpen reports whether the fullscreen onboarding layer is currently open.
func (m *Manager) IsOpen() bool {
	return m.win != nil
}

// Open launches the single fullscreen per-pixel translucent onboarding canvas.
func (m *Manager) Open() {
	mygo.App.SetActivationPolicy(mygo.ActivationPolicyRegular)
	if m.hooks.SetLiveDesktopHUD != nil {
		m.hooks.SetLiveDesktopHUD(false)
	}

	m.resetFromCatalog()

	disp := mygo.Screen.PrimaryDisplay()
	b := disp.Bounds
	if b.Width <= 0 || b.Height <= 0 {
		b = mygo.Rectangle{X: 0, Y: 0, Width: 1440, Height: 900}
	}

	if m.win != nil {
		if m.hooks.OnOpen != nil {
			m.hooks.OnOpen()
		}
		m.win.SetBounds(b)
		configureTranslucentWindow(m.win, true)
		m.win.Show()
		m.win.Focus()
		m.StartBroadcast()
		m.ensureVideoPlayer()
		return
	}

	if m.hooks.OnOpen != nil {
		m.hooks.OnOpen()
	}

	tr := i18n.For(m.lang)
	win := mygo.NewWindow(mygo.WindowOptions{
		Title:         tr.OnbWindowTitle,
		X:             b.X,
		Y:             b.Y,
		Width:         b.Width,
		Height:        b.Height,
		Frameless:     true,
		Transparent:   true,
		AlwaysOnTop:   true,
		DisableShadow: true,
		DisableResize: true,
		SkipTaskbar:   true,
		Content:       ui.View(m.View),
	})
	win.SetBounds(b)
	configureTranslucentWindow(win, true)
	m.win = win
	m.StartBroadcast()
	m.ensureVideoPlayer()

	win.OnClosed(func() {
		if m.win == win {
			m.win = nil
		}
		m.closeVideoPlayer()
		m.stopBroadcastAudio(220 * time.Millisecond)
		if m.hooks.SetLiveDesktopHUD != nil {
			m.hooks.SetLiveDesktopHUD(false)
		}
		mygo.App.SetActivationPolicy(mygo.ActivationPolicyAccessory)
		if m.hooks.OnClose != nil {
			m.hooks.OnClose(false)
		}
	})
}

// Close tears down the onboarding window and stops any active broadcast audio.
func (m *Manager) Close() {
	m.closeVideoPlayer()
	m.stopBroadcastAudio(150 * time.Millisecond)
	if m.hooks.SetLiveDesktopHUD != nil {
		m.hooks.SetLiveDesktopHUD(false)
	}
	if m.win != nil {
		w := m.win
		m.win = nil
		w.Update(func() {
			w.Close()
		})
	}
}

// StartBroadcast starts the Stage 0 Helldivers 2 theme music & voiceover cues.
func (m *Manager) StartBroadcast() {
	m.closeVideoPlayer()
	m.stopBroadcastAudio(0)
	m.broadcastStart = time.Now()
	m.broadcastClockInit = false
	m.heroRevealed = false
	if m.muted || m.spk == nil {
		return
	}
	m.spk.Play(sfx.CueOnboardingIntro)

	m.audioMu.Lock()
	m.audioTimer = time.AfterFunc(6750*time.Millisecond, func() {
		if !m.muted && m.spk != nil && m.stage == stageBroadcast && !m.heroRevealed {
			m.spk.Play(sfx.CueJoinHelldiversVO)
		}
	})
	m.audioMu.Unlock()
}

func (m *Manager) stopBroadcastAudio(fadeDur time.Duration) {
	m.audioMu.Lock()
	if m.audioTimer != nil {
		m.audioTimer.Stop()
		m.audioTimer = nil
	}
	m.audioMu.Unlock()

	if m.spk != nil {
		m.spk.StopMusic(fadeDur)
	}
}

// ObserveEngineSnapshot streams real-time global hotkey events into the live
// top-left HUD preview and Stage 4 Resupply drill.
func (m *Manager) ObserveEngineSnapshot(snap engine.Snapshot) {
	apply := func() {
		m.engSnap = snap
		m.engSnap.Typed = slices.Clone(snap.Typed)
		m.hasEngSnap = true

		if m.stage != stageDrill || m.drillDone {
			m.prevErrSeq = snap.ErrorSeq
			return
		}

		if snap.ErrorSeq != m.prevErrSeq {
			m.prevErrSeq = snap.ErrorSeq
			m.drillStep = 0
			m.drillMiss = true
			return
		}

		if len(snap.Typed) > 0 {
			prefixMatch := len(snap.Typed) <= len(drillTargetCode)
			for i := 0; prefixMatch && i < len(snap.Typed); i++ {
				if snap.Typed[i] != drillTargetCode[i] {
					prefixMatch = false
				}
			}
			if prefixMatch {
				m.drillStep = len(snap.Typed)
				m.drillMiss = false
				if m.drillStep == len(drillTargetCode) &&
					(snap.Phase == engine.PhaseDeploying || snap.Phase == engine.PhaseDeployed) {
					m.completeDrill()
				}
			}
		} else if (snap.Phase == engine.PhaseDeploying || snap.Phase == engine.PhaseDeployed) &&
			snap.MatchedID == "resupply-files" {
			m.completeDrill()
		}
	}

	if m.win != nil {
		m.win.Update(apply)
	} else {
		apply()
	}
}

func (m *Manager) completeDrill() {
	if m.drillDone {
		return
	}
	m.drillStep = len(drillTargetCode)
	m.drillDone = true
	m.drillMiss = false
	if !m.muted && m.spk != nil {
		m.spk.Play(sfx.CueMissionComplete)
	}
}

func (m *Manager) commitLiveCalibration() {
	ship := strings.TrimSpace(m.shipName)
	if ship == "" {
		ship = catalog.DefaultShipName(m.lang)
		m.shipName = ship
	}
	lang := m.lang
	_ = m.cat.Update(func(s *catalog.Snapshot) error {
		s.Settings.Language = lang
		s.Settings.TriggerMode = m.triggerMode
		s.Settings.HUDTheme = m.hudTheme
		s.Settings.Muted = m.muted
		if strings.TrimSpace(s.Settings.ShipName) == "" {
			s.Settings.ShipName = ship
		}
		for i := range s.Stratagems {
			if localized := localizedStarterPodName(s.Stratagems[i].ID, lang); localized != "" {
				s.Stratagems[i].Name = localized
			}
		}
		return nil
	})
}

func (m *Manager) ensureResupplyReadyForDrill() {
	_ = m.cat.Update(func(s *catalog.Snapshot) error {
		for i := range s.Stratagems {
			if s.Stratagems[i].ID == "resupply-files" {
				s.Stratagems[i].Enabled = true
				return nil
			}
		}
		return nil
	})
}

func (m *Manager) goToStage(next int) {
	if next < stageBroadcast || next > stageDrill || next == m.stage {
		return
	}
	prevStage := m.stage
	if prevStage == stageBroadcast && next != stageBroadcast {
		m.closeVideoPlayer()
		m.stopBroadcastAudio(520 * time.Millisecond)
	}

	m.stage = next
	if prevStage == stageBroadcast && next >= stageTheme {
		m.hudEnterPending = true
		m.termEnterPending = true
	}

	if next == stageBroadcast {
		if m.hooks.SetLiveDesktopHUD != nil {
			m.hooks.SetLiveDesktopHUD(false)
		}
		m.StartBroadcast()
		return
	}

	m.commitLiveCalibration()
	if next == stageDrill {
		m.ensureResupplyReadyForDrill()
		m.drillStep = 0
		m.drillDone = false
		m.drillMiss = false
	}

	if m.hooks.SetLiveDesktopHUD != nil {
		m.hooks.SetLiveDesktopHUD(true)
	}
	if !m.muted && m.spk != nil {
		switch next {
		case stageDrill:
			m.spk.Play(sfx.CueOpen)
		default:
			m.spk.Play(sfx.CueStep1)
		}
	}
}

func (m *Manager) finishOnboarding(completed bool) {
	m.closeVideoPlayer()
	m.stopBroadcastAudio(300 * time.Millisecond)
	if m.hooks.SetLiveDesktopHUD != nil {
		m.hooks.SetLiveDesktopHUD(false)
	}

	ship := strings.TrimSpace(m.shipName)
	if ship == "" {
		ship = catalog.DefaultShipName(m.lang)
	}
	lang := m.lang
	_ = m.cat.Update(func(s *catalog.Snapshot) error {
		s.Settings.Onboarded = true
		s.Settings.Language = lang
		s.Settings.TriggerMode = m.triggerMode
		s.Settings.HUDTheme = m.hudTheme
		s.Settings.Muted = m.muted
		if strings.TrimSpace(s.Settings.ShipName) == "" {
			s.Settings.ShipName = ship
		}
		for i := range s.Stratagems {
			if localized := localizedStarterPodName(s.Stratagems[i].ID, lang); localized != "" {
				s.Stratagems[i].Name = localized
			}
		}
		return nil
	})

	m.stage = stageBroadcast
	if m.win != nil {
		w := m.win
		m.win = nil
		w.OnClosed(func() {})
		w.Close()
	}
	mygo.App.SetActivationPolicy(mygo.ActivationPolicyAccessory)
	if m.hooks.OnClose != nil {
		m.hooks.OnClose(completed)
	}
}

func onboardingTheme(lang catalog.Language) *ui.Theme {
	t := ui.DarkTheme()
	accent := ui.Hex("#ffe900")
	t.Background = ui.Transparent
	t.Surface = ui.RGBA(14, 18, 25, 0.94)
	t.SurfaceHover = ui.RGBA(22, 28, 38, 0.96)
	t.SurfacePressed = ui.RGBA(28, 36, 48, 0.98)
	t.Border = ui.RGBA(255, 255, 255, 0.14)
	t.Text = ui.Hex("#f4f6fa")
	t.TextMuted = ui.Hex("#8e98aa")
	t.Accent = accent
	t.AccentHover = ui.Hex("#fff04d")
	t.AccentPressed = ui.Hex("#e6d200")
	t.AccentText = ui.Hex("#090b0f")
	t.InactiveAccent = accent
	t.Danger = ui.Hex("#ff453a")
	t.Success = ui.Hex("#42d674")
	t.Radius = 2
	if lang == catalog.LangEN {
		t.Font = "FS Sinclair"
	}
	t.FontSize = 13
	return t
}

// View renders the entire onboarding flow inside a single fullscreen translucent layer.
func (m *Manager) View(c *ui.Context) {
	t := onboardingTheme(m.lang)
	c.SetTheme(t)
	c.Root().Background(ui.Transparent)

	tr := i18n.For(m.lang)

	if m.stage == stageBroadcast {
		m.captureBroadcastShortcuts(c)
		m.renderFullscreenBroadcast(c, t, tr)
		return
	}

	m.captureStageShortcuts(c)
	m.renderFocusedSetupStage(c, t, tr)
}

func (m *Manager) broadcastElapsed(now time.Time) float64 {
	if !m.broadcastClockInit {
		m.broadcastStart = now
		m.broadcastClockInit = true
		return 0
	}
	d := now.Sub(m.broadcastStart).Seconds()
	if d < 0 {
		return 0
	}
	return d
}

func (m *Manager) captureBroadcastShortcuts(c *ui.Context) {
	elapsed := m.broadcastElapsed(c.Now())
	if c.Shortcut(0, ui.KeyEscape) {
		if !m.heroRevealed && elapsed < montageDurationSec()-0.6 {
			m.revealHeroTitleNow()
		} else {
			m.finishOnboarding(false)
		}
		return
	}
	if c.Shortcut(0, ui.KeySpace) {
		if !m.heroRevealed && !m.isHeroInteractive(elapsed) {
			m.revealHeroTitleNow()
		} else {
			m.goToStage(stageTheme)
		}
		return
	}
	if c.Shortcut(0, ui.KeyEnter) && m.isHeroInteractive(elapsed) {
		m.goToStage(stageTheme)
	}
}

func (m *Manager) captureStageShortcuts(c *ui.Context) {
	if c.FocusTakesText() {
		return
	}
	if c.Shortcut(0, ui.KeyEscape) {
		m.finishOnboarding(false)
		return
	}

	if m.stage == stageDrill {
		if m.drillDone {
			if c.Shortcut(0, ui.KeyEnter) || c.Shortcut(0, ui.KeySpace) {
				m.finishOnboarding(true)
			}
			return
		}
		switch {
		case c.Shortcut(0, ui.KeyW) || c.Shortcut(0, ui.KeyUp):
			m.feedDrillDir(catalog.DirUp)
		case c.Shortcut(0, ui.KeyS) || c.Shortcut(0, ui.KeyDown):
			m.feedDrillDir(catalog.DirDown)
		case c.Shortcut(0, ui.KeyA) || c.Shortcut(0, ui.KeyLeft):
			m.feedDrillDir(catalog.DirLeft)
		case c.Shortcut(0, ui.KeyD) || c.Shortcut(0, ui.KeyRight):
			m.feedDrillDir(catalog.DirRight)
		}
		return
	}

	if c.Shortcut(0, ui.KeyEnter) {
		m.goToStage(m.stage + 1)
	}
}

func (m *Manager) revealHeroTitleNow() {
	m.heroRevealed = true
	m.closeVideoPlayer()
	m.stopBroadcastAudio(600 * time.Millisecond)
}

func (m *Manager) isHeroInteractive(elapsed float64) bool {
	if m.heroRevealed {
		return true
	}
	return elapsed >= montageDurationSec()-0.9
}

func (m *Manager) ensureVideoPlayer() *ui.VideoPlayer {
	if m.win == nil {
		return nil
	}
	if m.videoPlayer != nil {
		return m.videoPlayer
	}
	if m.videoInitDone || len(rawMontageMP4) == 0 {
		return nil
	}
	m.videoInitDone = true
	p, err := video.OpenData(rawMontageMP4)
	if err != nil || p == nil {
		return nil
	}
	p.SetMuted(true).SetLoop(false).Play()
	m.videoPlayer = p
	return m.videoPlayer
}

func (m *Manager) closeVideoPlayer() {
	if m.videoPlayer != nil {
		p := m.videoPlayer
		m.videoPlayer = nil
		p.Close()
	}
	m.videoInitDone = false
}

func (m *Manager) ensurePosterBitmap() *ui.Bitmap {
	if m.posterBmp != nil {
		return m.posterBmp
	}
	img := montagePosterImage()
	if img == nil {
		return nil
	}
	m.posterBmp = ui.NewBitmap(img)
	return m.posterBmp
}

func (m *Manager) ensureHeroLogoBitmap() *ui.Bitmap {
	if m.heroLogoBmp != nil {
		return m.heroLogoBmp
	}
	img := heroLogoImage()
	if img == nil {
		return nil
	}
	m.heroLogoBmp = ui.NewBitmap(img)
	return m.heroLogoBmp
}

func smoothStep01(x float64) float32 {
	if x <= 0 {
		return 0
	}
	if x >= 1 {
		return 1
	}
	return float32(x * x * (3 - 2*x))
}

// renderFullscreenBroadcast renders the HD 2560x1200 @ 30fps Helldivers 2 MP4
// broadcast with zero text clutter, dissolving smoothly into the Hero Logo.
func (m *Manager) renderFullscreenBroadcast(c *ui.Context, t *ui.Theme, tr i18n.Strings) {
	totalDur := montageDurationSec()
	elapsed := m.broadcastElapsed(c.Now())
	if !m.heroRevealed && m.videoPlayer != nil {
		if vd := m.videoPlayer.Duration(); vd > 3*time.Second {
			totalDur = vd.Seconds()
		}
		vpPos := m.videoPlayer.Position().Seconds()
		if m.videoPlayer.Playing() && vpPos > 0.05 {
			elapsed = vpPos
		} else if m.videoPlayer.Ended() {
			elapsed = totalDur + 0.25
		}
	}

	var videoReveal float32
	var heroAlpha float32
	stillAnimating := false

	if m.heroRevealed {
		videoReveal = 0
		heroAlpha = 1
	} else {
		fadeIn := float32(0.32) + 0.68*smoothStep01(elapsed/0.70)
		fadeOut := 1.0 - smoothStep01((elapsed-(totalDur-1.65))/1.55)
		videoReveal = fadeIn * fadeOut
		heroAlpha = smoothStep01((elapsed - (totalDur - 1.25)) / 1.05)
		if elapsed < totalDur+0.20 {
			stillAnimating = true
		} else {
			m.heroRevealed = true
			m.closeVideoPlayer()
			videoReveal = 0
			heroAlpha = 1
		}
	}

	if stillAnimating {
		c.After(33 * time.Millisecond)
	}

	logoBmp := m.ensureHeroLogoBitmap()
	posterBmp := m.ensurePosterBitmap()

	canvas := ui.Box(c.Key("onb-fullscreen-broadcast")).
		Fill().
		Draw(func(p *ui.Painter, r ui.Rect) {
			// Translucent dark cinema veil over the user's real desktop
			veilAlpha := float32(0.86)
			if videoReveal > 0.05 {
				veilAlpha = 0.78 + 0.10*(1.0-videoReveal)
			}
			p.Fill(r, ui.RGBA(5, 7, 11, veilAlpha), 0)
		})

	canvas.Children(func() {
		if videoReveal > 0.004 {
			vidLayer := ui.Box(c.Key("onb-broadcast-video-layer")).
				Absolute().
				Top(0).
				Right(0).
				Bottom(0).
				Left(0).
				ZIndex(0).
				HitTest(func(float32, float32, ui.Rect) bool { return false })
			if imprintShader != nil {
				vidLayer.Effect(imprintShader.Effect(shader.Options{
					MaxSampleOffset: ui.Point{X: 2, Y: 2},
					Uniforms: map[string]any{
						"Time":   float32(elapsed),
						"Reveal": videoReveal,
					},
				}))
			}
			vidLayer.Children(func() {
				if posterBmp != nil {
					ui.Image(c.Key("onb-montage-poster"), posterBmp).
						Absolute().
						Top(0).
						Right(0).
						Bottom(0).
						Left(0).
						Fit(ui.Cover)
				}
				if vp := m.ensureVideoPlayer(); vp != nil {
					ui.Video(c.Key("onb-montage-video"), vp).
						Absolute().
						Top(0).
						Right(0).
						Bottom(0).
						Left(0).
						Fit(ui.Cover)
				}
			})
		}

		ui.Column(c.Key("onb-fs-fg")).
			Fill().
			ZIndex(2).
			Children(func() {
				// Discreet Top-Right Controls ONLY — zero banner or caption clutter!
				ui.Row(c.Key("onb-fs-topbar")).
					Padding(20, 28).
					AlignItems(ui.Center).
					Gap(8).
					Children(func() {
						ui.Spacer(c)
						m.renderLangAndMuteControls(c, t)
						if !m.isHeroInteractive(elapsed) {
							if ui.Button(c.Key("onb-btn-skip-video"), tr.OnbSkipVideoBtn).Clicked() {
								m.revealHeroTitleNow()
							}
						}
						if ui.Button(c.Key("onb-btn-skip-all"), tr.OnbSkipAll).Clicked() {
							m.finishOnboarding(false)
						}
					})

				ui.Spacer(c)

				if heroAlpha > 0.02 {
					riseY := (1.0 - heroAlpha) * 14.0
					ui.Row(c.Key("onb-hero-center")).
						Justify(ui.Center).
						Padding(0, 28).
						Opacity(heroAlpha).
						Translate(0, riseY).
						Children(func() {
							ui.Column(c.Key("onb-hero-stack")).
								AlignItems(ui.Center).
								Gap(34).
								Children(func() {
									if logoBmp != nil {
										ui.Image(c.Key("onb-hero-logo"), logoBmp).
											Size(760, 228).
											Fit(ui.Contain)
									} else {
										ui.Text(c, "HELLAUNCHER").
											Font("FS Sinclair").
											FontSize(56).
											Bold().
											TextColor(ui.Hex("#ffffff")).
											SingleLine()
									}

									cta := ui.ButtonBase(c.Key("onb-btn-enlist-now")).
										Height(48).
										MinWidth(264).
										Padding(0, 34).
										Radius(2).
										Center().
										Background(t.Accent).
										Border(2, ui.Hex("#fff36b")).
										Shadow(0, 6, 26, 0, ui.RGBA(0, 0, 0, 0.65)).
										Cursor(ui.CursorPointer)
									if cta.Hovered() {
										cta.Background(t.AccentHover)
									}
									if cta.Pressed() {
										cta.Background(t.AccentPressed)
									}
									cta.Children(func() {
										ui.Row(c).AlignItems(ui.Center).Gap(12).Children(func() {
											ui.Box(c).Size(6, 16).Background(ui.RGBA(9, 11, 15, 0.32))
											ui.Text(c, tr.OnbEnlistNowBtn).
												FontSize(16.5).
												Bold().
												TextColor(t.AccentText).
												SingleLine()
											ui.Box(c).Size(6, 16).Background(ui.RGBA(9, 11, 15, 0.32))
										})
									})
									if cta.Clicked() {
										m.goToStage(stageTheme)
									}
								})
						})
				}

				ui.Spacer(c)
				ui.Box(c).Height(36)
			})
	})
}

func (m *Manager) liveHUDSnapshot() engine.Snapshot {
	loadout := m.cat.Snapshot()
	loadout.Settings.Language = m.lang
	loadout.Settings.HUDTheme = m.hudTheme
	loadout.Settings.TriggerMode = m.triggerMode
	loadout.Settings.Muted = m.muted

	trusted := true
	if m.hooks.IsTrusted != nil {
		trusted = m.hooks.IsTrusted()
	}
	hdReady := false
	if m.hasEngSnap {
		hdReady = m.engSnap.HelldiversReady
	}

	snap := engine.Snapshot{
		Phase:           engine.PhaseOpen,
		Trusted:         trusted,
		HelldiversReady: hdReady,
		Loadout:         loadout,
	}

	if m.stage == stageDrill {
		if m.drillDone {
			snap.Phase = engine.PhaseDeployed
			snap.MatchedID = "resupply-files"
			snap.Typed = slices.Clone(drillTargetCode)
			snap.StepSeq = m.drillStepSq
			return snap
		}
		if m.drillStep > 0 && m.drillStep <= len(drillTargetCode) {
			snap.Typed = slices.Clone(drillTargetCode[:m.drillStep])
			snap.StepSeq = m.drillStepSq
		}
		snap.ErrorSeq = m.drillErrSq
		return snap
	}

	if m.hasEngSnap && len(m.engSnap.Typed) > 0 {
		snap.Phase = m.engSnap.Phase
		if snap.Phase == engine.PhaseIdle {
			snap.Phase = engine.PhaseOpen
		}
		snap.Typed = slices.Clone(m.engSnap.Typed)
		snap.MatchedID = m.engSnap.MatchedID
		snap.StepSeq = m.engSnap.StepSeq
		snap.ErrorSeq = m.engSnap.ErrorSeq
	}
	return snap
}

// renderFocusedSetupStage renders Stages 1..4 on the single fullscreen
// translucent canvas: the live top-left HUD at (24, 22) + a sharp centered
// Super Destroyer terminal focused on exactly ONE concept per step.
func (m *Manager) renderFocusedSetupStage(c *ui.Context, t *ui.Theme, tr i18n.Strings) {
	hudSnap := m.liveHUDSnapshot()

	ui.Box(c.Key("onb-setup-canvas")).
		Fill().
		Background(ui.RGBA(6, 9, 14, 0.74)).
		Children(func() {
			// 1. Live Real Stratagem HUD anchored at its authentic upper-left desktop position
			ui.Box(c.Key("onb-live-hud-anchor")).
				Absolute().
				Left(24).
				Top(22).
				ZIndex(1).
				HitTest(func(float32, float32, ui.Rect) bool { return false }).
				Children(func() {
					hud.PreviewAnimated(c, hudSnap, &m.hudEnterPending)
				})

			// 2. Foreground interactive column (Top-Right Controls + Centered Terminal)
			ui.Column(c.Key("onb-setup-fg")).
				Fill().
				ZIndex(2).
				Children(func() {
					ui.Row(c.Key("onb-setup-topbar")).
						Padding(20, 28).
						AlignItems(ui.Center).
						Gap(8).
						Children(func() {
							ui.Spacer(c)
							m.renderLangAndMuteControls(c, t)
							if ui.Button(c.Key("onb-btn-skip-all"), tr.OnbSkipAll).Clicked() {
								m.finishOnboarding(false)
							}
						})

					ui.Spacer(c)

					ui.Row(c.Key("onb-terminal-center")).
						Justify(ui.Center).
						Padding(0, 24).
						Children(func() {
							m.renderStepTerminal(c, t, tr)
						})

					ui.Spacer(c)
					ui.Box(c).Height(28)
				})
		})
}

func (m *Manager) stepTitle(tr i18n.Strings) string {
	switch m.stage {
	case stageTheme:
		return tr.OnbStepThemeTitle
	case stageTrigger:
		return tr.OnbStepTriggerTitle
	case stageLoadout:
		return tr.OnbStepLoadoutTitle
	default:
		return tr.OnbStepDrillTitle
	}
}

func (m *Manager) renderStepTerminal(c *ui.Context, t *ui.Theme, tr i18n.Strings) {
	bdrCol := ui.RGBA(255, 233, 0, 0.42)
	if m.stage == stageDrill {
		if m.drillDone {
			bdrCol = t.Accent
		} else if m.drillMiss {
			bdrCol = t.Danger
		}
	}

	termCol := ui.Column(c.Key("onb-step-terminal")).
		Width(548).
		Radius(2).
		Clip().
		Background(ui.RGBA(11, 15, 21, 0.95)).
		Border(1.5, bdrCol).
		Shadow(0, 18, 48, 0, ui.RGBA(0, 0, 0, 0.62))
	if m.termEnterPending {
		m.termEnterPending = false
		termCol.Jump("enter", 0)
	}
	enter := termCol.Animate("enter", 1.0, 200*time.Millisecond)
	termCol.Opacity(enter).Translate(0, (1.0-enter)*14.0)

	termCol.Children(func() {
		// Top 3px Helldivers Gold tactical rule
		ui.Box(c).Height(3).Background(t.Accent)

		// 4-Segment Progress Bar + Crisp Step Header (zero subtitle!)
		ui.Column(c.Key("onb-term-hdr")).
			Padding(18, 24, 14, 24).
			Gap(12).
			Children(func() {
				ui.Row(c.Key("onb-step-bars")).Gap(6).Children(func() {
					for s := stageTheme; s <= stageDrill; s++ {
						col := ui.RGBA(255, 255, 255, 0.14)
						if s <= m.stage {
							col = t.Accent
						}
						ui.Box(c.Key(s)).Grow(1).Height(3).Background(col)
					}
				})

				ui.Row(c).AlignItems(ui.Center).Gap(10).Children(func() {
					if emb := hud.EmblemSVG(); emb != nil {
						ui.Image(c, emb).Size(18, 18)
					}
					ui.Text(c, fmt.Sprintf("0%d / 04", m.stage)).
						Font("FS Sinclair").
						FontSize(13.5).
						Bold().
						TextColor(t.Accent).
						SingleLine()
					ui.Box(c).Size(1.5, 14).Background(ui.RGBA(255, 255, 255, 0.18))
					ui.Text(c, m.stepTitle(tr)).
						FontSize(18.5).
						Bold().
						TextColor(ui.Hex("#ffffff")).
						SingleLine().
						Grow(1)

					if m.stage == stageDrill && m.drillDone {
						ui.Row(c.Key("onb-drill-ready-pill")).
							Padding(3, 10).
							Radius(2).
							Background(ui.RGBA(255, 233, 0, 0.18)).
							Border(1.5, t.Accent).
							Children(func() {
								ui.Text(c, tr.OnbDrillReady).
									FontSize(12.5).
									Bold().
									TextColor(t.Accent).
									SingleLine()
							})
					}
				})
			})

		ui.Divider(c)

		// Single-Focus Step Body
		ui.Column(c.Key("onb-term-body")).
			Padding(20, 24).
			Children(func() {
				switch m.stage {
				case stageTheme:
					m.renderStage1ThemeBody(c, t, tr)
				case stageTrigger:
					m.renderStage2TriggerBody(c, t, tr)
				case stageLoadout:
					m.renderStage3LoadoutBody(c, t, tr)
				case stageDrill:
					m.renderStage4DrillBody(c, t)
				}
			})

		ui.Divider(c)

		// Crisp Two-Button Footer
		ui.Row(c.Key("onb-term-footer")).
			Padding(13, 24).
			AlignItems(ui.Center).
			Background(ui.RGBA(8, 11, 16, 0.96)).
			Children(func() {
				if m.stage > stageTheme {
					if ui.Button(c.Key("onb-back"), tr.OnbBackBtn).Clicked() {
						m.goToStage(m.stage - 1)
					}
				}

				ui.Spacer(c)

				if m.stage < stageDrill {
					if ui.PrimaryButton(c.Key("onb-next"), tr.OnbNextBtn).Clicked() {
						m.goToStage(m.stage + 1)
					}
				} else if m.drillDone {
					if ui.PrimaryButton(c.Key("onb-btn-finish-dive"), tr.OnbFinishDiveBtn).Clicked() {
						m.finishOnboarding(true)
					}
				} else {
					if ui.Button(c.Key("onb-btn-finish-dive"), tr.OnbFinishDiveBtn).Clicked() {
						m.finishOnboarding(true)
					}
				}
			})
	})
}

// Stage 1: HUD Theme — 2 pure visual schematic cards, zero descriptions.
func (m *Manager) renderStage1ThemeBody(c *ui.Context, t *ui.Theme, tr i18n.Strings) {
	ui.Row(c.Key("onb-theme-row")).Gap(14).AlignItems(ui.Stretch).Children(func() {
		m.renderVisualThemeCard(c, t, "onb-theme-classic", catalog.ThemeClassic, tr.ThemeClassic, true)
		m.renderVisualThemeCard(c, t, "onb-theme-tactical", catalog.ThemeTactical, tr.ThemeTactical, false)
	})
}

func (m *Manager) renderVisualThemeCard(
	c *ui.Context,
	t *ui.Theme,
	key string,
	theme catalog.HUDTheme,
	title string,
	isClassic bool,
) {
	selected := m.hudTheme == theme
	bg := t.Surface
	bdr := t.Border
	if selected {
		bg = ui.RGBA(255, 233, 0, 0.09)
		bdr = t.Accent
	}

	// Invariant 2px border & Bold text prevent any selection jitter.
	card := ui.ButtonBase(c.Key(key)).
		Grow(1).
		MinWidth(0).
		Padding(12).
		Radius(2).
		Background(bg).
		Border(2, bdr).
		Cursor(ui.CursorPointer)
	if !selected && card.Hovered() {
		card.Background(t.SurfaceHover)
	}

	card.Children(func() {
		ui.Column(c).Grow(1).Gap(12).Children(func() {
			ui.Box(c).
				Height(112).
				Radius(2).
				Clip().
				Border(1, ui.RGBA(255, 255, 255, 0.09)).
				Draw(func(p *ui.Painter, r ui.Rect) {
					if isClassic {
						p.Fill(r, ui.Hex("#3d4f5e"), 0)
						p.Fill(ui.Rect{X: r.X + 12, Y: r.Y + 10, W: r.W * 0.54, H: 52}, ui.RGBA(236, 218, 188, 0.36), 2)
						p.Fill(ui.Rect{X: r.X + r.W*0.36, Y: r.Y + 42, W: r.W * 0.52, H: 54}, ui.RGBA(96, 174, 220, 0.34), 2)

						panel := ui.Rect{X: r.X + 16, Y: r.Y + 14, W: r.W - 32, H: r.H - 28}
						p.Fill(panel, ui.RGBA(34, 37, 42, 0.80), 0)

						p.Fill(ui.Rect{X: panel.X + 10, Y: panel.Y + 11, W: 26, H: 26}, ui.RGBA(146, 52, 46, 0.58), 0)
						p.Stroke(ui.Rect{X: panel.X + 10, Y: panel.Y + 11, W: 26, H: 26}, ui.Hex("#de6356"), 0, 2)
						p.Fill(ui.Rect{X: panel.X + 44, Y: panel.Y + 13, W: 84, H: 7}, ui.RGBA(246, 248, 250, 0.92), 1)
						p.Fill(ui.Rect{X: panel.X + 44, Y: panel.Y + 26, W: 8, H: 8}, ui.Hex("#ffffff"), 1)
						p.Fill(ui.Rect{X: panel.X + 56, Y: panel.Y + 26, W: 8, H: 8}, ui.Hex("#ffffff"), 1)
						p.Fill(ui.Rect{X: panel.X + 68, Y: panel.Y + 26, W: 8, H: 8}, ui.Hex("#9da3ab"), 1)

						p.Fill(ui.Rect{X: panel.X + 10, Y: panel.Y + 46, W: 26, H: 26}, ui.RGBA(116, 98, 42, 0.58), 0)
						p.Stroke(ui.Rect{X: panel.X + 10, Y: panel.Y + 46, W: 26, H: 26}, ui.Hex("#c9b268"), 0, 2)
						p.Fill(ui.Rect{X: panel.X + 44, Y: panel.Y + 48, W: 98, H: 7}, ui.RGBA(246, 248, 250, 0.92), 1)
						p.Fill(ui.Rect{X: panel.X + 44, Y: panel.Y + 61, W: 8, H: 8}, ui.Hex("#9da3ab"), 1)
						p.Fill(ui.Rect{X: panel.X + 56, Y: panel.Y + 61, W: 8, H: 8}, ui.Hex("#9da3ab"), 1)
						p.Fill(ui.Rect{X: panel.X + 68, Y: panel.Y + 61, W: 8, H: 8}, ui.Hex("#9da3ab"), 1)
					} else {
						p.Fill(r, ui.Hex("#141922"), 0)
						panel := ui.Rect{X: r.X + 16, Y: r.Y + 14, W: r.W - 32, H: r.H - 28}
						p.Fill(panel, ui.RGBA(10, 13, 18, 0.95), 0)
						p.Stroke(panel, ui.RGBA(255, 233, 0, 0.42), 0, 1.5)
						p.Fill(ui.Rect{X: panel.X, Y: panel.Y, W: panel.W, H: 3}, ui.Hex("#ffe900"), 0)

						p.Fill(ui.Rect{X: panel.X + 10, Y: panel.Y + 13, W: 26, H: 26}, ui.RGBA(146, 52, 46, 0.58), 0)
						p.Stroke(ui.Rect{X: panel.X + 10, Y: panel.Y + 13, W: 26, H: 26}, ui.Hex("#de6356"), 0, 2)
						p.Fill(ui.Rect{X: panel.X + 44, Y: panel.Y + 15, W: 84, H: 7}, ui.RGBA(246, 248, 250, 0.92), 1)
						p.Fill(ui.Rect{X: panel.X + 44, Y: panel.Y + 28, W: 8, H: 8}, ui.Hex("#ffe900"), 1)
						p.Fill(ui.Rect{X: panel.X + 56, Y: panel.Y + 28, W: 8, H: 8}, ui.Hex("#ffe900"), 1)
						p.Fill(ui.Rect{X: panel.X + 68, Y: panel.Y + 28, W: 8, H: 8}, ui.RGBA(255, 255, 255, 0.35), 1)

						p.Fill(ui.Rect{X: panel.X + 10, Y: panel.Y + 47, W: 26, H: 26}, ui.RGBA(116, 98, 42, 0.58), 0)
						p.Stroke(ui.Rect{X: panel.X + 10, Y: panel.Y + 47, W: 26, H: 26}, ui.Hex("#c9b268"), 0, 2)
						p.Fill(ui.Rect{X: panel.X + 44, Y: panel.Y + 49, W: 94, H: 7}, ui.RGBA(246, 248, 250, 0.92), 1)
						p.Fill(ui.Rect{X: panel.X + 44, Y: panel.Y + 62, W: 8, H: 8}, ui.RGBA(255, 255, 255, 0.35), 1)
						p.Fill(ui.Rect{X: panel.X + 56, Y: panel.Y + 62, W: 8, H: 8}, ui.RGBA(255, 255, 255, 0.35), 1)
					}
				})

			ui.Row(c).AlignItems(ui.Center).Gap(9).Children(func() {
				indBdr := ui.RGBA(255, 255, 255, 0.28)
				if selected {
					indBdr = t.Accent
				}
				ui.Row(c).Size(15, 15).Center().Border(1.5, indBdr).Children(func() {
					if selected {
						ui.Box(c).Size(7, 7).Background(t.Accent)
					}
				})
				ui.Text(c, title).FontSize(15).Bold().TextColor(ui.Hex("#ffffff")).SingleLine()
			})
		})
	})

	if card.Clicked() && m.hudTheme != theme {
		m.hudTheme = theme
		m.hudEnterPending = true
		m.commitLiveCalibration()
		if !m.muted && m.spk != nil {
			m.spk.Play(sfx.CueStep2)
		}
	}
}

// Stage 2: Summon Key — 3 clean keycap rows, zero descriptions.
func (m *Manager) renderStage2TriggerBody(c *ui.Context, t *ui.Theme, tr i18n.Strings) {
	ui.Column(c.Key("onb-trigger-col")).Gap(10).Children(func() {
		m.renderTriggerKeycapCard(c, t, "trig-double", catalog.ModeDoubleHoldCtrl, "CTRL × 2", tr.OnbTrigDouble, tr.OnbRecBadge)
		m.renderTriggerKeycapCard(c, t, "trig-left", catalog.ModeHoldLeftCtrl, "L-CTRL", tr.OnbTrigLeft, "")
		m.renderTriggerKeycapCard(c, t, "trig-right", catalog.ModeRightCtrlOrAlt, "R-CTRL", tr.OnbTrigRight, "")

		if runtime.GOOS == "darwin" && m.hooks.GuidePermission != nil {
			trusted := true
			if m.hooks.IsTrusted != nil {
				trusted = m.hooks.IsTrusted()
			}
			if !trusted {
				ui.Row(c.Key("onb-mac-ax")).
					Padding(10, 14).
					Radius(2).
					AlignItems(ui.Center).
					Gap(10).
					Background(t.Surface).
					Border(1.5, ui.RGBA(255, 233, 0, 0.38)).
					Children(func() {
						ui.Text(c, tr.PermissionLabel).FontSize(13).Bold().TextColor(t.Text).Grow(1)
						if ui.PrimaryButton(c.Key("onb-btn-grant-ax"), tr.PermissionGrantBtn).Clicked() {
							m.hooks.GuidePermission()
						}
					})
			}
		}
	})
}

func (m *Manager) renderTriggerKeycapCard(
	c *ui.Context,
	t *ui.Theme,
	key string,
	mode catalog.TriggerMode,
	keycap string,
	title string,
	badge string,
) {
	selected := m.triggerMode == mode
	bg := t.Surface
	bdr := t.Border
	if selected {
		bg = ui.RGBA(255, 233, 0, 0.09)
		bdr = t.Accent
	}

	card := ui.ButtonBase(c.Key(key)).
		Height(54).
		Padding(0, 14).
		Radius(2).
		Background(bg).
		Border(2, bdr).
		Cursor(ui.CursorPointer)
	if !selected && card.Hovered() {
		card.Background(t.SurfaceHover)
	}

	card.Children(func() {
		ui.Row(c).Grow(1).MinWidth(0).AlignItems(ui.Center).Gap(14).Children(func() {
			ui.Row(c).
				Size(92, 32).
				Shrink(0).
				Center().
				Radius(2).
				Background(ui.Hex("#080b10")).
				Border(1.5, ui.RGBA(255, 233, 0, 0.52)).
				Children(func() {
					ui.Text(c, keycap).Font("FS Sinclair").FontSize(12.5).Bold().TextColor(t.Accent).SingleLine()
				})

			ui.Text(c, title).
				FontSize(15).
				Bold().
				TextColor(ui.Hex("#ffffff")).
				SingleLine().
				Grow(1)

			if badge != "" {
				ui.Row(c).
					Padding(2, 7).
					Radius(2).
					Background(ui.RGBA(255, 233, 0, 0.16)).
					Border(1, ui.RGBA(255, 233, 0, 0.50)).
					Children(func() {
						ui.Text(c, badge).FontSize(10.5).Bold().TextColor(t.Accent).SingleLine()
					})
			}

			ui.Row(c).Size(18, 18).Shrink(0).Center().Children(func() {
				if selected {
					ui.Text(c, "✓").FontSize(14).Bold().TextColor(t.Accent)
				}
			})
		})
	})

	if card.Clicked() && m.triggerMode != mode {
		m.triggerMode = mode
		m.commitLiveCalibration()
		if !m.muted && m.spk != nil {
			m.spk.Play(sfx.CueStep1)
		}
	}
}

type starterPodSpec struct {
	key    string
	icon   string
	cat    catalog.Category
	arrows []catalog.Dir
	label  string
	locked bool
}

func localizedStarterPodName(id string, lang catalog.Language) string {
	isEN := lang == catalog.LangEN
	switch id {
	case "onb-win-left":
		if isEN {
			return "Eagle Strafing: Tile Left"
		}
		return "“飞鹰”扫射：窗口左半屏"
	case "onb-win-right":
		if isEN {
			return "Eagle Airstrike: Tile Right"
		}
		return "“飞鹰”空袭：窗口右半屏"
	case "onb-browser":
		if isEN {
			return "Recon Vehicle: Default Browser"
		}
		return "快速侦察车：默认浏览器"
	case "onb-screenshot":
		if isEN {
			return "Orbital Flare: Region Screenshot"
		}
		return "轨道照明弹：区域截图"
	case "onb-lock":
		if isEN {
			return "Shield Relay: Lock Screen"
		}
		return "护盾中继器：立即锁屏"
	default:
		return ""
	}
}

func starterPodItems(podKey string, lang catalog.Language) []catalog.Stratagem {
	switch podKey {
	case "split":
		return []catalog.Stratagem{
			{
				ID:       "onb-win-left",
				Name:     localizedStarterPodName("onb-win-left", lang),
				Category: catalog.CategoryRed,
				Icon:     "eagle-strafing-run.svg",
				Enabled:  true,
				Code:     []catalog.Dir{catalog.DirUp, catalog.DirRight, catalog.DirLeft},
				Delivery: catalog.DeliveryImmediate,
				Steps:    []catalog.Step{{Kind: catalog.StepSystemAction, Target: string(catalog.SysWinLeft)}},
			},
			{
				ID:       "onb-win-right",
				Name:     localizedStarterPodName("onb-win-right", lang),
				Category: catalog.CategoryRed,
				Icon:     "eagle-airstrike.svg",
				Enabled:  true,
				Code:     []catalog.Dir{catalog.DirUp, catalog.DirRight, catalog.DirRight},
				Delivery: catalog.DeliveryImmediate,
				Steps:    []catalog.Step{{Kind: catalog.StepSystemAction, Target: string(catalog.SysWinRight)}},
			},
		}
	case "browser":
		return []catalog.Stratagem{{
			ID:       "onb-browser",
			Name:     localizedStarterPodName("onb-browser", lang),
			Category: catalog.CategoryBlue,
			Icon:     "fast-recon-vehicle.svg",
			Enabled:  true,
			Code:     []catalog.Dir{catalog.DirLeft, catalog.DirDown, catalog.DirRight, catalog.DirUp},
			Delivery: catalog.DeliveryImmediate,
			Steps:    []catalog.Step{{Kind: catalog.StepLaunchApp, Target: "browser"}},
		}}
	case "shot":
		return []catalog.Stratagem{{
			ID:       "onb-screenshot",
			Name:     localizedStarterPodName("onb-screenshot", lang),
			Category: catalog.CategoryYellow,
			Icon:     "orbital-illumination-flare.svg",
			Enabled:  true,
			Code:     []catalog.Dir{catalog.DirRight, catalog.DirRight, catalog.DirLeft, catalog.DirLeft},
			Delivery: catalog.DeliveryImmediate,
			Steps:    []catalog.Step{{Kind: catalog.StepSystemAction, Target: string(catalog.SysScreenshot)}},
		}}
	case "lock":
		return []catalog.Stratagem{{
			ID:       "onb-lock",
			Name:     localizedStarterPodName("onb-lock", lang),
			Category: catalog.CategoryGreen,
			Icon:     "shield-generator-relay.svg",
			Enabled:  true,
			Code:     []catalog.Dir{catalog.DirDown, catalog.DirDown, catalog.DirLeft, catalog.DirRight},
			Delivery: catalog.DeliveryImmediate,
			Steps:    []catalog.Step{{Kind: catalog.StepSystemAction, Target: string(catalog.SysLockScreen)}},
		}}
	default:
		return nil
	}
}

func (m *Manager) isStarterPodEquipped(podKey string, snap catalog.Snapshot) bool {
	switch podKey {
	case "close":
		for _, st := range snap.Stratagems {
			if st.ID == "orbital-close-win" {
				return st.Enabled
			}
		}
		return false
	case "files":
		return true
	default:
		items := starterPodItems(podKey, m.lang)
		if len(items) == 0 {
			return false
		}
		wantID := items[0].ID
		for _, st := range snap.Stratagems {
			if st.ID == wantID && st.Enabled {
				return true
			}
		}
		return false
	}
}

func (m *Manager) toggleStarterPod(podKey string) {
	if podKey == "files" {
		return
	}
	snap := m.cat.Snapshot()
	equipped := m.isStarterPodEquipped(podKey, snap)

	if podKey == "close" {
		_ = m.cat.Update(func(s *catalog.Snapshot) error {
			for i := range s.Stratagems {
				if s.Stratagems[i].ID == "orbital-close-win" {
					s.Stratagems[i].Enabled = !equipped
					return nil
				}
			}
			return nil
		})
	} else {
		items := starterPodItems(podKey, m.lang)
		if len(items) == 0 {
			return
		}
		wantRemove := make(map[string]bool, len(items))
		for _, it := range items {
			wantRemove[it.ID] = true
		}

		_ = m.cat.Update(func(s *catalog.Snapshot) error {
			if equipped {
				filtered := make([]catalog.Stratagem, 0, len(s.Stratagems))
				for _, st := range s.Stratagems {
					if !wantRemove[st.ID] {
						filtered = append(filtered, st)
					}
				}
				s.Stratagems = filtered
				return nil
			}

			userList := make([]catalog.Stratagem, 0, len(s.Stratagems)+len(items))
			var sysList []catalog.Stratagem
			for _, st := range s.Stratagems {
				if catalog.IsSystemStratagem(st.ID) {
					sysList = append(sysList, st)
				} else {
					userList = append(userList, st)
				}
			}
			for _, cand := range items {
				conflict := false
				for _, ex := range userList {
					if ex.ID == cand.ID || (ex.Enabled && cand.Enabled && catalog.CodesOverlap(ex.Code, cand.Code)) {
						conflict = true
						break
					}
				}
				for _, ex := range sysList {
					if catalog.CodesOverlap(ex.Code, cand.Code) {
						conflict = true
						break
					}
				}
				if !conflict {
					userList = append(userList, cand)
				}
			}
			s.Stratagems = append(userList, sysList...)
			return nil
		})
	}

	if !m.muted && m.spk != nil {
		m.spk.Play(sfx.CueStep2)
	}
}

// Stage 3: Loadout — 2-column Helldivers Stratagem Pod Grid updating the live HUD in real time.
func (m *Manager) renderStage3LoadoutBody(c *ui.Context, t *ui.Theme, tr i18n.Strings) {
	snap := m.cat.Snapshot()
	pods := []starterPodSpec{
		{
			key:    "close",
			icon:   "orbital-precision.svg",
			cat:    catalog.CategoryRed,
			arrows: []catalog.Dir{catalog.DirRight, catalog.DirRight, catalog.DirUp},
			label:  tr.OnbPodCloseWin,
		},
		{
			key:    "files",
			icon:   "resupply.svg",
			cat:    catalog.CategoryYellow,
			arrows: []catalog.Dir{catalog.DirDown, catalog.DirDown, catalog.DirUp, catalog.DirRight},
			label:  tr.OnbPodFiles,
			locked: true,
		},
		{
			key:    "split",
			icon:   "eagle-strafing-run.svg",
			cat:    catalog.CategoryRed,
			arrows: []catalog.Dir{catalog.DirUp, catalog.DirRight, catalog.DirLeft},
			label:  tr.OnbPodSplit,
		},
		{
			key:    "browser",
			icon:   "fast-recon-vehicle.svg",
			cat:    catalog.CategoryBlue,
			arrows: []catalog.Dir{catalog.DirLeft, catalog.DirDown, catalog.DirRight, catalog.DirUp},
			label:  tr.OnbPodBrowser,
		},
		{
			key:    "shot",
			icon:   "orbital-illumination-flare.svg",
			cat:    catalog.CategoryYellow,
			arrows: []catalog.Dir{catalog.DirRight, catalog.DirRight, catalog.DirLeft, catalog.DirLeft},
			label:  tr.OnbPodScreenshot,
		},
		{
			key:    "lock",
			icon:   "shield-generator-relay.svg",
			cat:    catalog.CategoryGreen,
			arrows: []catalog.Dir{catalog.DirDown, catalog.DirDown, catalog.DirLeft, catalog.DirRight},
			label:  tr.OnbPodLock,
		},
	}

	ui.Grid(c.Key("onb-loadout-grid")).Columns(2).Gap(10).Children(func() {
		for _, spec := range pods {
			m.renderPodSlotCard(c, t, spec, m.isStarterPodEquipped(spec.key, snap))
		}
	})
}

func (m *Manager) renderPodSlotCard(c *ui.Context, t *ui.Theme, spec starterPodSpec, active bool) {
	bg := t.Surface
	bdr := t.Border
	if active {
		bg = ui.RGBA(255, 233, 0, 0.08)
		bdr = t.Accent
	}

	cursor := ui.CursorPointer
	if spec.locked {
		cursor = ui.CursorDefault
	}

	card := ui.ButtonBase(c.Key("onb-pod-"+spec.key)).
		Grow(1).
		MinWidth(0).
		Height(62).
		Padding(0, 12).
		Radius(2).
		Background(bg).
		Border(1.5, bdr).
		Cursor(cursor)
	if !active && !spec.locked && card.Hovered() {
		card.Background(t.SurfaceHover)
	}

	card.Children(func() {
		ui.Row(c).Grow(1).MinWidth(0).AlignItems(ui.Center).Gap(11).Children(func() {
			iconBdr := hud.CategoryBorderColor(spec.cat)
			iconFill := hud.CategoryFillColor(spec.cat)
			if !active {
				iconBdr = ui.RGBA(180, 186, 196, 0.36)
				iconFill = ui.RGBA(36, 40, 48, 0.55)
			}
			ui.Row(c).
				Size(38, 38).
				Shrink(0).
				Center().
				Background(iconFill).
				Border(1.5, iconBdr).
				Children(func() {
					if svg := hud.IconSVG(spec.icon); svg != nil {
						img := ui.Image(c, svg).Size(27, 27)
						if !active {
							img.Opacity(0.55)
						}
					}
				})

			ui.Column(c).Grow(1).MinWidth(0).Gap(3).Children(func() {
				txtCol := ui.RGBA(242, 245, 250, 0.72)
				if active {
					txtCol = ui.Hex("#ffffff")
				}
				ui.Text(c, spec.label).FontSize(13.5).Bold().TextColor(txtCol).SingleLine()

				ui.Row(c).Gap(4).AlignItems(ui.Center).Children(func() {
					arrCol := ui.RGBA(255, 255, 255, 0.35)
					if active {
						arrCol = ui.RGBA(255, 233, 0, 0.90)
					}
					for idx, d := range spec.arrows {
						hud.RenderArrow(c.Key(idx), d, 11.5, arrCol)
					}
				})
			})

			ui.Row(c).Size(18, 18).Shrink(0).Center().Children(func() {
				if active {
					ui.Text(c, "✓").FontSize(13.5).Bold().TextColor(t.Accent)
				} else {
					ui.Text(c, "+").FontSize(15).Bold().TextColor(t.TextMuted)
				}
			})
		})
	})

	if card.Clicked() && !spec.locked {
		m.toggleStarterPod(spec.key)
	}
}

func (m *Manager) triggerKeycapGlyph() string {
	switch m.triggerMode {
	case catalog.ModeHoldLeftCtrl:
		return "L-CTRL"
	case catalog.ModeRightCtrlOrAlt:
		return "R-CTRL"
	default:
		return "CTRL × 2"
	}
}

// Stage 4: Call In Resupply — Pure visual equation [CTRL] + [↓] [↓] [↑] [→], zero prose!
func (m *Manager) renderStage4DrillBody(c *ui.Context, t *ui.Theme) {
	hotkeyHeld := m.hasEngSnap && m.engSnap.Phase != engine.PhaseIdle

	ui.Column(c.Key("onb-drill-body")).
		Padding(14, 0).
		AlignItems(ui.Center).
		Justify(ui.Center).
		Children(func() {
			ui.Row(c.Key("onb-drill-equation")).
				AlignItems(ui.Center).
				Justify(ui.Center).
				Gap(12).
				Children(func() {
					// Keycap Pill
					kcBg := ui.Hex("#080b10")
					kcBdr := ui.RGBA(255, 233, 0, 0.55)
					kcTxt := t.Accent
					if hotkeyHeld || m.drillDone {
						kcBg = ui.RGBA(255, 233, 0, 0.20)
						kcBdr = t.Accent
						kcTxt = ui.Hex("#ffffff")
					}
					ui.Row(c.Key("drill-keycap")).
						Height(58).
						Padding(0, 18).
						Radius(2).
						Center().
						Background(kcBg).
						Border(2, kcBdr).
						Children(func() {
							ui.Text(c, m.triggerKeycapGlyph()).
								Font("FS Sinclair").
								FontSize(15.5).
								Bold().
								TextColor(kcTxt).
								SingleLine()
						})

					ui.Text(c, "+").
						FontSize(22).
						Bold().
						TextColor(ui.RGBA(255, 255, 255, 0.45))

					// 4 Interactive Arrow Tiles (↓↓↑→)
					ui.Row(c.Key("onb-drill-arrows")).AlignItems(ui.Center).Gap(10).Children(func() {
						for idx, dir := range drillTargetCode {
							hit := idx < m.drillStep
							cur := !m.drillDone && idx == m.drillStep
							boxBg := ui.Hex("#111620")
							boxBdr := ui.RGBA(255, 255, 255, 0.18)
							arrowCol := ui.RGBA(255, 255, 255, 0.40)
							if hit {
								boxBg = ui.RGBA(255, 233, 0, 0.22)
								boxBdr = t.Accent
								arrowCol = t.Accent
							} else if m.drillMiss && idx == 0 {
								boxBdr = t.Danger
								arrowCol = ui.Hex("#ff8b80")
							} else if cur {
								boxBdr = ui.RGBA(255, 233, 0, 0.65)
								arrowCol = ui.Hex("#ffffff")
							}

							tile := ui.ButtonBase(c.Key(fmt.Sprintf("drill-step-%d", idx))).
								Size(58, 58).
								Radius(2).
								Center().
								Background(boxBg).
								Border(2, boxBdr).
								Cursor(ui.CursorPointer)
							tile.Children(func() {
								hud.RenderArrow(c, dir, 28, arrowCol)
							})
							if tile.Clicked() {
								m.feedDrillDir(dir)
							}
						}
					})
				})
		})
}

func (m *Manager) feedDrillDir(d catalog.Dir) {
	if m.drillDone {
		return
	}
	expected := drillTargetCode[m.drillStep]
	if d != expected {
		m.drillStep = 0
		m.drillMiss = true
		m.drillErrSq++
		if !m.muted && m.spk != nil {
			m.spk.Play(sfx.CueFail)
		}
		return
	}

	m.drillMiss = false
	m.drillStep++
	m.drillStepSq++
	if m.drillStep >= len(drillTargetCode) {
		m.completeDrill()
		return
	}
	if !m.muted && m.spk != nil {
		m.spk.Play(sfx.StepCue(m.drillStep))
	}
}

func (m *Manager) renderLangAndMuteControls(c *ui.Context, t *ui.Theme) {
	muteKey := "speaker_on"
	if m.muted {
		muteKey = "speaker_off"
	}
	muteBtn := ui.ButtonBase(c.Key("onb-btn-mute")).
		Size(32, 30).
		Radius(2).
		Center().
		Background(t.Surface).
		Border(1, t.Border).
		Cursor(ui.CursorPointer)
	if muteBtn.Hovered() {
		muteBtn.Background(t.SurfaceHover)
	}
	muteBtn.Children(func() {
		if svg := hud.UIIconSVG(muteKey); svg != nil {
			ui.Image(c, svg).Size(16, 16)
		}
	})
	if muteBtn.Clicked() {
		m.muted = !m.muted
		if m.spk != nil {
			m.spk.SetMuted(m.muted)
			if m.muted {
				m.spk.StopMusic(150 * time.Millisecond)
			}
		}
		if m.stage != stageBroadcast {
			m.commitLiveCalibration()
		}
	}

	if ui.Segmented(c.Key("onb-seg-lang"), &m.langIdx, "中文", "EN").Changed() {
		prevDefault := catalog.DefaultShipName(m.lang)
		if m.langIdx == 1 {
			m.lang = catalog.LangEN
		} else {
			m.lang = catalog.LangZH
		}
		if strings.TrimSpace(m.shipName) == "" || m.shipName == prevDefault {
			m.shipName = catalog.DefaultShipName(m.lang)
		}
		if m.win != nil {
			m.win.SetTitle(i18n.For(m.lang).OnbWindowTitle)
		}
		if m.stage != stageBroadcast {
			m.commitLiveCalibration()
		}
	}
}
