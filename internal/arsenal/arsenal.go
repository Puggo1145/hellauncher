package arsenal

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"hellauncher/internal/catalog"
	"hellauncher/internal/engine"
	"hellauncher/internal/hud"
	"hellauncher/internal/i18n"
	"hellauncher/internal/sys"
)

// AppOption describes an installed desktop application selectable in the Arsenal editor.
type AppOption struct {
	ID   string
	Name string
	Path string
}

// Hooks wires optional desktop actions into the Arsenal window without coupling
// package arsenal to OS input hooks, shell launchers, or overlay windows.
type Hooks struct {
	PreviewHUD         func()
	OpenOnboarding     func()
	OpenLogs           func()
	GuidePermission    func()
	IsTrusted          func() bool
	GetOpenAtLogin     func() bool
	SetOpenAtLogin     func(open bool) error
	ExportBackup       func(parent *mygo.Window, data []byte, onSaved func(path string, err error))
	ImportBackup       func(parent *mygo.Window, onPicked func(data []byte, err error))
	DetectHelldivers   func(customPath string) sys.HelldiversInstall
	BrowseHelldivers   func(parent *mygo.Window, currentPath string, onPicked func(string))
	LaunchHelldivers   func()
	OpenEnlistURL      func()
	OnHelldiversStatus func(ready bool)
}

const (
	tabStratagems = 0
	tabAppearance = 1
	tabSettings   = 2

	maxComboSteps = 5
)

var stepKindOrder = []catalog.StepKind{
	catalog.StepLaunchApp,
	catalog.StepOpenTarget,
	catalog.StepShellCommand,
	catalog.StepRunShortcut,
	catalog.StepCloseActiveWindow,
}

func stepKindToIndex(k catalog.StepKind) int {
	if k == catalog.StepSystemAction || k == catalog.StepCloseActiveWindow {
		return 4
	}
	for i, v := range stepKindOrder {
		if v == k {
			return i
		}
	}
	return 0
}

func indexToStepKind(idx int) catalog.StepKind {
	if idx < 0 || idx >= len(stepKindOrder) {
		return catalog.StepLaunchApp
	}
	return stepKindOrder[idx]
}

// Manager owns the on-demand native GPU Configurator window ("Stratagem Arsenal").
//
// It depends strictly on *catalog.Catalog (plus optional app-listing and desktop
// hooks) with zero coupling to OS hooks or audio playback.
type Manager struct {
	cat      *catalog.Catalog
	listApps func() []AppOption
	hooks    Hooks

	win       *mygo.Window
	unsub     func()
	closeOnce sync.Once

	// Active top-level navigation tab (UI thread)
	navTab int

	// Bound global settings state (UI thread)
	snap         catalog.Snapshot
	triggerIdx   int
	langIdx      int
	themeIdx     int
	soundOn      bool
	visorCurved  bool
	gameBypassOn bool
	openAtLogin  bool
	shipName     string

	// Selected / Draft Stratagem state (UI thread)
	selectedID     string
	isCreatingNew  bool
	recording      bool
	iconPickerOpen bool
	iconSearch     string
	iconCatFilter  int
	confirmReset   bool
	simIdx         int

	// Cached system Helldivers 2 installation status & file dialog guards (UI thread)
	hdStatus   sys.HelldiversInstall
	hdChecked  bool
	browsingHD bool
	exporting  bool
	importing  bool

	chordHandle ui.Handle

	draftID       string
	draftName     string
	draftSubtitle string
	draftCategory catalog.Category
	draftIcon     string
	draftEnabled  bool
	draftDelivery catalog.Delivery
	deliveryIdx   int
	draftCode     []catalog.Dir
	draftSteps    []catalog.Step
	activeStepIdx int
	stepKindIdx   int
	draftTarget   string

	// Installed app quick-picker state
	apps     []AppOption
	appQuery string

	// Feedback state
	errMsg string
	okMsg  string
}

// New constructs an Arsenal Manager backed by cat.
func New(cat *catalog.Catalog, listApps func() []AppOption) *Manager {
	m := &Manager{
		cat:      cat,
		listApps: listApps,
	}
	m.reloadSnapshot(cat.Snapshot(), true)
	m.unsub = cat.Subscribe(func(next catalog.Snapshot) {
		if m.win != nil {
			m.win.Update(func() {
				m.reloadSnapshot(next, false)
			})
		} else {
			m.reloadSnapshot(next, false)
		}
	})
	return m
}

// SetHooks configures optional system callbacks (HUD preview, logs folder, macOS permission guide, Helldivers 2 hooks).
func (m *Manager) SetHooks(h Hooks) {
	if h.PreviewHUD == nil && m.hooks.PreviewHUD != nil {
		h.PreviewHUD = m.hooks.PreviewHUD
	}
	m.hooks = h
	m.hdChecked = false
	if m.hooks.GetOpenAtLogin != nil {
		m.openAtLogin = m.hooks.GetOpenAtLogin()
	}
}

// SetPreviewFunc attaches an optional callback that triggers the live desktop HUD overlay.
func (m *Manager) SetPreviewFunc(fn func()) {
	m.hooks.PreviewHUD = fn
}

func (m *Manager) detectHelldivers(customPath string) sys.HelldiversInstall {
	if m.hooks.DetectHelldivers != nil {
		return m.hooks.DetectHelldivers(customPath)
	}
	return sys.DetectHelldivers(customPath)
}

func (m *Manager) refreshHelldiversStatus() {
	m.hdStatus = m.detectHelldivers(m.snap.Settings.HelldiversPath)
	m.hdChecked = true
	if m.hooks.OnHelldiversStatus != nil {
		m.hooks.OnHelldiversStatus(m.hdStatus.Found)
	}
}

// Open creates and focuses the Arsenal window on demand, or brings the existing
// window to the front.
func (m *Manager) Open() {
	mygo.App.SetActivationPolicy(mygo.ActivationPolicyRegular)
	if m.win != nil {
		m.refreshHelldiversStatus()
		m.win.Show()
		m.win.Focus()
		return
	}

	m.errMsg = ""
	m.okMsg = ""
	m.recording = false
	m.iconPickerOpen = false
	m.confirmReset = false
	m.reloadSnapshot(m.cat.Snapshot(), true)
	m.refreshHelldiversStatus()

	tr := i18n.For(m.currentLang())
	win := mygo.NewWindow(mygo.WindowOptions{
		Title:           tr.WindowTitle,
		Width:           1180,
		Height:          700,
		MinWidth:        980,
		MinHeight:       580,
		BackgroundColor: "#0e1116",
		TitleBarStyle:   mygo.TitleBarHiddenInset,
		TitleBarHeight:  48,
		Content:         ui.View(m.View),
	})
	win.SetWindowButtonsTheme(mygo.ThemeDark)
	m.win = win

	win.OnClosed(func() {
		if m.win == win {
			m.win = nil
		}
		mygo.App.SetActivationPolicy(mygo.ActivationPolicyAccessory)
	})

	if m.listApps != nil {
		fn := m.listApps
		go func() {
			found := fn()
			win.Update(func() {
				m.apps = found
			})
		}()
	}
}

// OpenStratagem opens the Arsenal window directly to the Stratagems tab with id selected.
func (m *Manager) OpenStratagem(id string) {
	m.Open()
	apply := func() {
		m.navTab = tabStratagems
		m.selectStratagem(id)
		m.refreshHelldiversStatus()
	}
	if m.win != nil {
		m.win.Update(func() {
			apply()
			m.win.Show()
			m.win.Focus()
		})
	} else {
		apply()
	}
}

// DismissWindow closes the active Arsenal window if open without detaching
// Catalog subscriptions (used when entering fullscreen desktop onboarding).
func (m *Manager) DismissWindow() {
	if m.win != nil {
		w := m.win
		m.win = nil
		w.Update(func() {
			w.Close()
		})
	}
}

// Close destroys the Arsenal window if open and detaches Catalog subscriptions.
func (m *Manager) Close() {
	m.closeOnce.Do(func() {
		if m.unsub != nil {
			m.unsub()
		}
	})
	if m.win != nil {
		w := m.win
		m.win = nil
		w.Update(func() {
			w.Close()
		})
	}
}

func (m *Manager) currentLang() catalog.Language {
	if m.langIdx == 1 {
		return catalog.LangEN
	}
	return catalog.LangZH
}

func (m *Manager) switchTab(idx int) {
	if m.navTab == idx {
		return
	}
	m.navTab = idx
	m.recording = false
	m.iconPickerOpen = false
	m.confirmReset = false
	m.errMsg = ""
	m.okMsg = ""
}

func arsenalTheme() *ui.Theme {
	t := ui.DarkTheme()
	accent := ui.Hex("#ffe900")
	t.Background = ui.Hex("#080b10")
	t.Surface = ui.Hex("#0f141d")
	t.SurfaceHover = ui.Hex("#171f2c")
	t.SurfacePressed = ui.Hex("#1f293a")
	t.Border = ui.RGBA(255, 255, 255, 0.12)
	t.Text = ui.Hex("#f2f4f8")
	t.TextMuted = ui.Hex("#8b94a5")
	t.Accent = accent
	t.AccentHover = ui.Hex("#fff04d")
	t.AccentPressed = ui.Hex("#e6d200")
	t.AccentText = ui.Hex("#0c0e12")
	t.InactiveAccent = accent
	t.Danger = ui.Hex("#ff453a")
	t.Success = ui.Hex("#42d674")
	t.Radius = 2
	t.FontSize = 13
	return t
}

func renderSectionHeader(c *ui.Context, t *ui.Theme, title string) {
	ui.Row(c).AlignItems(ui.Center).Gap(8).Children(func() {
		ui.Box(c).Size(3, 13).Radius(1).Background(t.Accent)
		ui.Text(c, title).FontSize(12).Bold().TextColor(t.TextMuted)
	})
}

// View renders the Stratagem Arsenal desktop application with unified dark chrome
// and top-level navigation.
func (m *Manager) View(c *ui.Context) {
	if m.win == nil && len(m.apps) == 0 && m.listApps != nil {
		m.apps = m.listApps()
	}
	if !m.hdChecked {
		m.refreshHelldiversStatus()
	}

	t := arsenalTheme()
	c.SetTheme(t)
	c.Root().Background(t.Background)

	lang := m.currentLang()
	tr := i18n.For(lang)

	if m.navTab == tabStratagems && (m.isCreatingNew || !catalog.IsSystemStratagem(m.selectedID)) {
		m.captureShortcuts(c)
	}

	ui.Column(c.Key("arsenal-root")).Fill().Background(t.Background).Children(func() {
		m.renderTopBar(c, t, tr)
		ui.Divider(c)

		switch m.navTab {
		case tabAppearance:
			m.renderAppearancePage(c, t, tr)
		case tabSettings:
			m.renderSettingsPage(c, t, tr)
		default:
			m.renderStratagemsPage(c, t, tr, lang)
		}
	})
}

func (m *Manager) captureShortcuts(c *ui.Context) {
	// Save shortcut: Ctrl+S / Cmd+S
	saveMod := ui.Ctrl
	if runtime.GOOS == "darwin" {
		saveMod = ui.Super
	}
	if c.Shortcut(saveMod, ui.KeyS) {
		m.saveDraft()
		return
	}

	if !m.recording {
		return
	}
	// Automatically exit recording if the user focuses a text input.
	if c.FocusTakesText() {
		m.recording = false
		return
	}

	appendDir := func(d catalog.Dir) {
		if len(m.draftCode) < 12 {
			m.draftCode = append(m.draftCode, d)
			m.errMsg = ""
			m.okMsg = ""
		}
	}
	switch {
	case c.Shortcut(0, ui.KeyW) || c.Shortcut(0, ui.KeyUp):
		appendDir(catalog.DirUp)
	case c.Shortcut(0, ui.KeyS) || c.Shortcut(0, ui.KeyDown):
		appendDir(catalog.DirDown)
	case c.Shortcut(0, ui.KeyA) || c.Shortcut(0, ui.KeyLeft):
		appendDir(catalog.DirLeft)
	case c.Shortcut(0, ui.KeyD) || c.Shortcut(0, ui.KeyRight):
		appendDir(catalog.DirRight)
	case c.Shortcut(0, ui.KeyBackspace):
		if len(m.draftCode) > 0 {
			m.draftCode = m.draftCode[:len(m.draftCode)-1]
			m.errMsg = ""
			m.okMsg = ""
		}
	case c.Shortcut(0, ui.KeyEscape) || c.Shortcut(0, ui.KeyEnter):
		m.recording = false
	}
}

func (m *Manager) renderTopBar(c *ui.Context, t *ui.Theme, tr i18n.Strings) {
	bar := c.TitleBar()
	barH := max(bar.Height, 48)

	ui.Row(c.Key("top-bar")).
		Height(barH).
		Shrink(0).
		Padding(0, bar.Right+16, 0, bar.Left+16).
		AlignItems(ui.Center).
		Gap(16).
		Background(t.Background).
		DragWindow().
		Children(func() {
			// App Brand
			ui.Row(c).AlignItems(ui.Center).Gap(8).Children(func() {
				if emb := hud.EmblemSVG(); emb != nil {
					ui.Image(c, emb).Size(16, 16)
				}
				ui.Text(c, tr.AppTitle).FontSize(13.5).Bold().TextColor(t.Text)
			})

			ui.Box(c).Size(1, 16).Background(ui.RGBA(255, 255, 255, 0.10))

			// Top Navigation Bar
			ui.Row(c.Key("top-nav")).AlignItems(ui.Center).Gap(4).Children(func() {
				m.renderNavTab(c, t, "nav-stratagems", tabStratagems, tr.NavStratagems)
				m.renderNavTab(c, t, "nav-appearance", tabAppearance, tr.NavAppearance)
				m.renderNavTab(c, t, "nav-settings", tabSettings, tr.NavSettings)
			})

			ui.Spacer(c)

			if ship := strings.TrimSpace(m.shipName); ship != "" {
				shipChip := ui.ButtonBase(c.Key("top-ship-badge")).
					Height(28).
					Padding(0, 10).
					Radius(2).
					Center().
					Background(ui.RGBA(255, 233, 0, 0.08)).
					Border(1, ui.RGBA(255, 233, 0, 0.30)).
					Cursor(ui.CursorPointer)
				shipChip.Children(func() {
					ui.Row(c).AlignItems(ui.Center).Gap(6).Children(func() {
						if emb := hud.EmblemSVG(); emb != nil {
							ui.Icon(c, emb).Size(12, 12).TextColor(t.Accent)
						}
						ui.Text(c, ship).FontSize(11.5).Bold().TextColor(t.Accent).SingleLine()
					})
				})
				if shipChip.Clicked() {
					m.switchTab(tabSettings)
				}
			}
		})
}

func (m *Manager) renderNavTab(c *ui.Context, t *ui.Theme, key string, idx int, label string) {
	active := m.navTab == idx
	bg := ui.Transparent
	borderCol := ui.Transparent
	txtCol := t.TextMuted

	if active {
		bg = ui.RGBA(255, 233, 0, 0.11)
		borderCol = ui.RGBA(255, 233, 0, 0.32)
		txtCol = t.Accent
	}

	btn := ui.ButtonBase(c.Key(key)).
		Height(32).
		Padding(0, 14).
		Radius(2).
		Center().
		Background(bg).
		Border(1, borderCol).
		Cursor(ui.CursorPointer)

	if active {
		btn.DrawOver(func(p *ui.Painter, r ui.Rect) {
			p.Fill(ui.Rect{X: r.X + 4, Y: r.Y + r.H - 2.5, W: r.W - 8, H: 2.5}, t.Accent, 1)
		})
	} else if btn.Hovered() {
		btn.Background(ui.RGBA(255, 255, 255, 0.05))
		txtCol = t.Text
	}

	btn.Children(func() {
		ui.Text(c, label).FontSize(13).Bold().TextColor(txtCol).SingleLine()
	})

	if btn.Clicked() {
		m.switchTab(idx)
	}
}

func (m *Manager) renderStratagemsPage(c *ui.Context, t *ui.Theme, tr i18n.Strings, lang catalog.Language) {
	ui.Row(c.Key("page-stratagems")).Grow(1).MinHeight(0).AlignItems(ui.Stretch).Children(func() {
		m.renderRosterSidebar(c, t, tr)
		ui.Divider(c)
		m.renderEditorPane(c, t, tr, lang)
		ui.Divider(c)
		m.renderPreviewStage(c, t, tr, 356)
	})
}

func (m *Manager) renderAppearancePage(c *ui.Context, t *ui.Theme, tr i18n.Strings) {
	showVisorToggle := m.themeIdx == 1
	showDesktopPreview := m.hooks.PreviewHUD != nil

	ui.Row(c.Key("page-appearance")).Grow(1).MinHeight(0).AlignItems(ui.Stretch).Children(func() {
		ui.Scroll(c.Key("appearance-scroll")).Grow(1).MinWidth(0).MinHeight(0).Children(func() {
			ui.Row(c).Justify(ui.Center).Padding(24, 28).Children(func() {
				ui.Column(c.Key("appearance-wrap")).Grow(1).MaxWidth(600).Gap(22).Children(func() {
					// Section 1: HUD Theme Cards
					ui.Column(c).Gap(10).Children(func() {
						renderSectionHeader(c, t, tr.SectionTheme)
						ui.Grid(c.Key("theme-cards")).Columns(2).Gap(14).Children(func() {
							m.renderThemeCard(c, t, "theme-card-classic", 0, tr.ThemeClassic, tr.ThemeClassicTag)
							m.renderThemeCard(c, t, "theme-card-tactical", 1, tr.ThemeTactical, tr.ThemeTacticalTag)
						})
					})

					// Section 2: Display Options (3D Helmet Visor Projection applies strictly to Tactical theme)
					if showVisorToggle || showDesktopPreview {
						ui.Column(c).Gap(10).Children(func() {
							renderSectionHeader(c, t, tr.SectionDisplay)
							ui.Column(c.Key("display-card")).
								Radius(2).
								Background(t.Surface).
								Border(1, t.Border).
								Children(func() {
									if showVisorToggle {
										ui.Row(c.Key("row-visor")).
											Padding(13, 16).
											AlignItems(ui.Center).
											Gap(12).
											Children(func() {
												lbl := ui.Text(c, tr.Visor3DLabel).FontSize(13).TextColor(t.Text).Grow(1)
												if ui.Switch(c.Key("sw-visor"), &m.visorCurved).LabelledBy(lbl).Changed() {
													m.commitGlobalSettings()
												}
											})
									}

									if showVisorToggle && showDesktopPreview {
										ui.Divider(c)
									}

									if showDesktopPreview {
										ui.Row(c.Key("row-desktop-preview")).
											Padding(12, 16).
											AlignItems(ui.Center).
											Gap(12).
											Children(func() {
												ui.Text(c, tr.DesktopPreviewLabel).FontSize(13).TextColor(t.Text).Grow(1)
												if ui.Button(c.Key("btn-desktop-preview"), tr.PreviewDesktopBtn).Clicked() {
													m.hooks.PreviewHUD()
												}
											})
									}
								})
						})
					}
				})
			})
		})

		ui.Divider(c)
		m.renderPreviewStage(c, t, tr, 392)
	})
}

func (m *Manager) renderThemeCard(
	c *ui.Context,
	t *ui.Theme,
	key string,
	idx int,
	title string,
	tag string,
) {
	selected := m.themeIdx == idx
	cardBg := t.Surface
	cardBorder := t.Border
	if selected {
		cardBg = ui.RGBA(255, 233, 0, 0.06)
		cardBorder = t.Accent
	}

	card := ui.ButtonBase(c.Key(key)).
		MinWidth(0).
		Padding(12).
		Radius(2).
		Background(cardBg).
		Border(1.5, cardBorder).
		Cursor(ui.CursorPointer)
	if selected {
		card.DrawOver(func(p *ui.Painter, r ui.Rect) {
			p.Fill(ui.Rect{X: r.X + 1, Y: r.Y + 1, W: r.W - 2, H: 2.5}, t.Accent, 0)
		})
	}

	card.Children(func() {
		ui.Column(c).Grow(1).Gap(10).Children(func() {
			// Visual Miniature Schematic
			ui.Box(c).
				Height(88).
				Radius(2).
				Clip().
				Border(1, ui.RGBA(255, 255, 255, 0.10)).
				Draw(func(p *ui.Painter, r ui.Rect) {
					if idx == 0 {
						// Classic Frosted Glass schematic
						p.Fill(r, ui.Hex("#4d6070"), 2)
						p.Fill(ui.Rect{X: r.X + 10, Y: r.Y + 8, W: r.W * 0.55, H: 40}, ui.RGBA(236, 218, 188, 0.45), 2)
						p.Fill(ui.Rect{X: r.X + r.W*0.35, Y: r.Y + 30, W: r.W * 0.55, H: 44}, ui.RGBA(96, 174, 220, 0.42), 2)

						panel := ui.Rect{X: r.X + 14, Y: r.Y + 11, W: r.W - 28, H: r.H - 22}
						p.Fill(panel, ui.RGBA(42, 44, 47, 0.78), 2)

						// Row 1
						p.Fill(ui.Rect{X: panel.X + 8, Y: panel.Y + 8, W: 20, H: 20}, ui.RGBA(146, 52, 46, 0.45), 2)
						p.Stroke(ui.Rect{X: panel.X + 8, Y: panel.Y + 8, W: 20, H: 20}, ui.Hex("#de6356"), 1, 2)
						p.Fill(ui.Rect{X: panel.X + 34, Y: panel.Y + 9, W: 64, H: 6}, ui.RGBA(244, 246, 248, 0.90), 1)
						p.Fill(ui.Rect{X: panel.X + 34, Y: panel.Y + 20, W: 7, H: 7}, ui.Hex("#ffffff"), 1)
						p.Fill(ui.Rect{X: panel.X + 45, Y: panel.Y + 20, W: 7, H: 7}, ui.Hex("#ffffff"), 1)
						p.Fill(ui.Rect{X: panel.X + 56, Y: panel.Y + 20, W: 7, H: 7}, ui.Hex("#9da3ab"), 1)

						// Row 2
						p.Fill(ui.Rect{X: panel.X + 8, Y: panel.Y + 34, W: 20, H: 20}, ui.RGBA(116, 98, 42, 0.52), 2)
						p.Stroke(ui.Rect{X: panel.X + 8, Y: panel.Y + 34, W: 20, H: 20}, ui.Hex("#c9b268"), 1, 2)
						p.Fill(ui.Rect{X: panel.X + 34, Y: panel.Y + 35, W: 82, H: 6}, ui.RGBA(244, 246, 248, 0.90), 1)
						p.Fill(ui.Rect{X: panel.X + 34, Y: panel.Y + 46, W: 7, H: 7}, ui.Hex("#9da3ab"), 1)
						p.Fill(ui.Rect{X: panel.X + 45, Y: panel.Y + 46, W: 7, H: 7}, ui.Hex("#9da3ab"), 1)
						p.Fill(ui.Rect{X: panel.X + 56, Y: panel.Y + 46, W: 7, H: 7}, ui.Hex("#9da3ab"), 1)
					} else {
						// Tactical Helmet Visor schematic
						p.Fill(r, ui.Hex("#161b24"), 2)
						panel := ui.Rect{X: r.X + 14, Y: r.Y + 11, W: r.W - 28, H: r.H - 22}
						p.Fill(panel, ui.RGBA(12, 15, 20, 0.94), 2)
						p.Stroke(panel, ui.RGBA(255, 233, 0, 0.36), 1, 2)
						// Top yellow accent bar
						p.Fill(ui.Rect{X: panel.X, Y: panel.Y, W: panel.W, H: 3}, ui.Hex("#ffe900"), 0)

						// Row 1
						p.Fill(ui.Rect{X: panel.X + 8, Y: panel.Y + 10, W: 20, H: 20}, ui.RGBA(146, 52, 46, 0.45), 2)
						p.Stroke(ui.Rect{X: panel.X + 8, Y: panel.Y + 10, W: 20, H: 20}, ui.Hex("#de6356"), 1, 2)
						p.Fill(ui.Rect{X: panel.X + 34, Y: panel.Y + 11, W: 64, H: 6}, ui.RGBA(242, 244, 248, 0.92), 1)
						p.Fill(ui.Rect{X: panel.X + 34, Y: panel.Y + 22, W: 7, H: 7}, ui.Hex("#ffe900"), 1)
						p.Fill(ui.Rect{X: panel.X + 45, Y: panel.Y + 22, W: 7, H: 7}, ui.Hex("#ffe900"), 1)
						p.Fill(ui.Rect{X: panel.X + 56, Y: panel.Y + 22, W: 7, H: 7}, ui.RGBA(255, 255, 255, 0.35), 1)

						// Row 2
						p.Fill(ui.Rect{X: panel.X + 8, Y: panel.Y + 35, W: 20, H: 20}, ui.RGBA(116, 98, 42, 0.52), 2)
						p.Stroke(ui.Rect{X: panel.X + 8, Y: panel.Y + 35, W: 20, H: 20}, ui.Hex("#c9b268"), 1, 2)
						p.Fill(ui.Rect{X: panel.X + 34, Y: panel.Y + 36, W: 78, H: 6}, ui.RGBA(242, 244, 248, 0.92), 1)
						p.Fill(ui.Rect{X: panel.X + 34, Y: panel.Y + 47, W: 7, H: 7}, ui.RGBA(255, 255, 255, 0.35), 1)
						p.Fill(ui.Rect{X: panel.X + 45, Y: panel.Y + 47, W: 7, H: 7}, ui.RGBA(255, 255, 255, 0.35), 1)
					}
				})

			// Title + Tag Row
			ui.Row(c).AlignItems(ui.Center).Gap(8).Children(func() {
				dotBorder := ui.RGBA(255, 255, 255, 0.28)
				if selected {
					dotBorder = t.Accent
				}
				ui.Row(c).Size(14, 14).Radius(2).Center().Border(1.5, dotBorder).Children(func() {
					if selected {
						ui.Box(c).Size(8, 8).Radius(1).Background(t.Accent)
					}
				})
				ui.Text(c, title).FontSize(13.5).Bold().TextColor(t.Text)
				ui.Spacer(c)
				ui.Text(c, tag).FontSize(11.5).TextColor(t.TextMuted).SingleLine()
			})
		})
	})

	if card.Clicked() && m.themeIdx != idx {
		m.themeIdx = idx
		m.commitGlobalSettings()
		if m.hooks.PreviewHUD != nil {
			m.hooks.PreviewHUD()
		}
	}
}

func (m *Manager) renderSettingsPage(c *ui.Context, t *ui.Theme, tr i18n.Strings) {
	ui.Scroll(c.Key("page-settings")).Grow(1).MinWidth(0).MinHeight(0).Children(func() {
		ui.Row(c).Justify(ui.Center).Padding(16, 28).Children(func() {
			ui.Column(c.Key("settings-wrap")).Grow(1).MaxWidth(620).Gap(15).Children(func() {
				// Super Destroyer Telemetry Header
				shipTitle := strings.TrimSpace(m.shipName)
				if shipTitle == "" {
					shipTitle = "SES"
				}
				triggerLabel := tr.TriggerDoubleCtrl
				switch m.triggerIdx {
				case 1:
					triggerLabel = tr.TriggerLeftCtrl
				case 2:
					triggerLabel = tr.TriggerRightCtrl
				}
				banner := ui.Row(c.Key("settings-ship-banner")).
					Padding(11, 16).
					Radius(2).
					Background(ui.Hex("#0d131d")).
					Border(1, ui.RGBA(255, 233, 0, 0.30)).
					AlignItems(ui.Center).
					Gap(12)
				banner.DrawOver(func(p *ui.Painter, r ui.Rect) {
					p.Fill(ui.Rect{X: r.X + 1, Y: r.Y + 1, W: r.W - 2, H: 2}, t.Accent, 0)
				})
				banner.Children(func() {
					ui.Row(c).
						Size(34, 34).
						Radius(2).
						Center().
						Background(ui.RGBA(255, 233, 0, 0.10)).
						Border(1, ui.RGBA(255, 233, 0, 0.35)).
						Children(func() {
							if emb := hud.EmblemSVG(); emb != nil {
								ui.Icon(c, emb).Size(19, 19).TextColor(t.Accent)
							}
						})
					ui.Column(c).Grow(1).Gap(3).Children(func() {
						ui.Text(c, shipTitle).FontSize(15).Bold().TextColor(ui.Hex("#ffffff")).SingleLine()
						ui.Row(c).AlignItems(ui.Center).Gap(10).Children(func() {
							ui.Text(c, triggerLabel).FontSize(11.5).Bold().TextColor(t.Accent).SingleLine()
							ui.Box(c).Size(1, 10).Background(ui.RGBA(255, 255, 255, 0.18))
							ui.Text(c, fmt.Sprintf("%s: %d", tr.NavStratagems, len(m.snap.Stratagems))).FontSize(11.5).TextColor(t.TextMuted).SingleLine()
						})
					})
				})

				// Group 1: Controls & Audio
				ui.Column(c).Gap(8).Children(func() {
					renderSectionHeader(c, t, tr.SectionControls)
					ui.Column(c.Key("card-controls")).
						Radius(2).
						Background(t.Surface).
						Border(1, t.Border).
						Children(func() {
							ui.Row(c.Key("row-shortcut")).
								Padding(10, 16).
								AlignItems(ui.Center).
								Gap(12).
								Wrap().
								Children(func() {
									ui.Text(c, tr.SummonShortcutLabel).FontSize(13).TextColor(t.Text).Grow(1)
									if ui.Segmented(
										c.Key("seg-trigger"),
										&m.triggerIdx,
										tr.TriggerDoubleCtrl,
										tr.TriggerLeftCtrl,
										tr.TriggerRightCtrl,
									).Changed() {
										m.commitGlobalSettings()
									}
								})

							ui.Divider(c)

							ui.Row(c.Key("row-sound")).
								Padding(10, 16).
								AlignItems(ui.Center).
								Gap(12).
								Children(func() {
									lbl := ui.Text(c, tr.SoundSettingLabel).FontSize(13).TextColor(t.Text).Grow(1)
									if ui.Switch(c.Key("sw-sound"), &m.soundOn).LabelledBy(lbl).Changed() {
										m.commitGlobalSettings()
									}
								})

							ui.Divider(c)

							ui.Row(c.Key("row-game-bypass")).
								Padding(10, 16).
								AlignItems(ui.Center).
								Gap(12).
								Children(func() {
									lbl := ui.Text(c, tr.GameBypassLabel).FontSize(13).TextColor(t.Text).Grow(1)
									if ui.Switch(c.Key("sw-game-bypass"), &m.gameBypassOn).LabelledBy(lbl).Changed() {
										m.commitGlobalSettings()
									}
								})
						})
				})

				// Group 2: General & System
				ui.Column(c).Gap(8).Children(func() {
					renderSectionHeader(c, t, tr.SectionSystem)
					ui.Column(c.Key("card-system")).
						Radius(2).
						Background(t.Surface).
						Border(1, t.Border).
						Children(func() {
							ui.Row(c.Key("row-lang")).
								Padding(10, 16).
								AlignItems(ui.Center).
								Gap(12).
								Children(func() {
									ui.Text(c, tr.LanguageLabel).FontSize(13).TextColor(t.Text).Grow(1)
									if ui.Segmented(c.Key("seg-lang"), &m.langIdx, "中文", "EN").Changed() {
										m.commitGlobalSettings()
										if m.win != nil {
											m.win.SetTitle(i18n.For(m.currentLang()).WindowTitle)
										}
									}
								})

							ui.Divider(c)

							ui.Row(c.Key("row-ship-name")).
								Padding(10, 16).
								AlignItems(ui.Center).
								Gap(12).
								Children(func() {
									ui.Text(c, tr.ShipNameLabel).FontSize(13).TextColor(t.Text).Grow(1).Shrink(0)
									ui.Row(c).AlignItems(ui.Center).Gap(8).Children(func() {
										if ui.TextInput(c.Key("inp-ship-name"), &m.shipName).
											Width(216).
											Radius(2).
											Placeholder(tr.ShipPlaceholder).
											Changed() {
											m.commitGlobalSettings()
										}
										rerollBtn := ui.ButtonBase(c.Key("btn-ship-random")).
											Padding(6, 11).
											Radius(2).
											Background(t.SurfaceHover).
											Border(1, ui.RGBA(255, 255, 255, 0.14)).
											Cursor(ui.CursorPointer)
										rerollBtn.Children(func() {
											ui.Row(c).AlignItems(ui.Center).Gap(5).Children(func() {
												ui.Icon(c, hud.UIIconSVG("reroll")).Size(13, 13).TextColor(t.Accent)
												ui.Text(c, tr.ShipRandomBtn).FontSize(12).Bold().TextColor(t.Text)
											})
										})
										if rerollBtn.Clicked() {
											m.shipName = catalog.RandomShipName(m.currentLang())
											m.commitGlobalSettings()
										}
									})
								})

							ui.Divider(c)

							ui.Row(c.Key("row-login")).
								Padding(10, 16).
								AlignItems(ui.Center).
								Gap(12).
								Children(func() {
									lbl := ui.Text(c, tr.LaunchAtLoginLabel).FontSize(13).TextColor(t.Text).Grow(1)
									if ui.Switch(c.Key("sw-login"), &m.openAtLogin).LabelledBy(lbl).Changed() {
										m.applyOpenAtLogin(m.openAtLogin)
									}
								})

							if m.hooks.OpenOnboarding != nil {
								ui.Divider(c)
								ui.Row(c.Key("row-onboarding")).
									Padding(10, 16).
									AlignItems(ui.Center).
									Gap(12).
									Children(func() {
										ui.Text(c, tr.OnboardingReplayLabel).FontSize(13).TextColor(t.Text).Grow(1)
										if ui.Button(c.Key("btn-replay-onboarding"), tr.OnboardingReplayBtn).Clicked() {
											m.hooks.OpenOnboarding()
										}
									})
							}

							ui.Divider(c)

							ui.Row(c.Key("row-backup")).
								Padding(10, 16).
								AlignItems(ui.Center).
								Gap(8).
								Wrap().
								Children(func() {
									ui.Text(c, tr.ConfigBackupLabel).FontSize(13).TextColor(t.Text).Grow(1)
									exportBtn := ui.ButtonBase(c.Key("btn-export-json")).
										Padding(6, 11).
										Radius(2).
										Background(t.SurfaceHover).
										Border(1, ui.RGBA(255, 255, 255, 0.14)).
										Disabled(m.exporting).
										Cursor(ui.CursorPointer)
									exportBtn.Children(func() {
										ui.Row(c).AlignItems(ui.Center).Gap(5).Children(func() {
											ui.Icon(c, hud.UIIconSVG("json_export")).Size(13, 13).TextColor(t.TextMuted)
											ui.Text(c, tr.ExportConfigBtn).FontSize(12).Bold().TextColor(t.Text)
										})
									})
									if exportBtn.Clicked() {
										m.exportBackupJSON()
									}
									importBtn := ui.ButtonBase(c.Key("btn-import-json")).
										Padding(6, 11).
										Radius(2).
										Background(t.SurfaceHover).
										Border(1, ui.RGBA(255, 255, 255, 0.14)).
										Disabled(m.importing).
										Cursor(ui.CursorPointer)
									importBtn.Children(func() {
										ui.Row(c).AlignItems(ui.Center).Gap(5).Children(func() {
											ui.Icon(c, hud.UIIconSVG("json_import")).Size(13, 13).TextColor(t.TextMuted)
											ui.Text(c, tr.ImportConfigBtn).FontSize(12).Bold().TextColor(t.Text)
										})
									})
									if importBtn.Clicked() {
										m.importBackupJSON()
									}
								})

							if runtime.GOOS == "darwin" && m.hooks.GuidePermission != nil {
								ui.Divider(c)
								trusted := true
								if m.hooks.IsTrusted != nil {
									trusted = m.hooks.IsTrusted()
								}
								ui.Row(c.Key("row-permission")).
									Padding(12, 16).
									AlignItems(ui.Center).
									Gap(10).
									Children(func() {
										ui.Text(c, tr.PermissionLabel).FontSize(13).TextColor(t.Text).Grow(1)
										statusTxt := tr.PermissionGranted
										statusCol := t.Success
										if !trusted {
											statusTxt = tr.PermissionMissing
											statusCol = t.Accent
										}
										ui.Text(c, statusTxt).FontSize(12).Bold().TextColor(statusCol)
										if ui.Button(c.Key("btn-grant-ax"), tr.PermissionGrantBtn).Clicked() {
											m.hooks.GuidePermission()
										}
									})
							}

							if m.hooks.OpenLogs != nil {
								ui.Divider(c)
								ui.Row(c.Key("row-logs")).
									Padding(12, 16).
									AlignItems(ui.Center).
									Gap(12).
									Children(func() {
										ui.Text(c, tr.LogsLabel).FontSize(13).TextColor(t.Text).Grow(1)
										if ui.Button(c.Key("btn-open-logs"), tr.OpenLogsBtn).Clicked() {
											m.hooks.OpenLogs()
										}
									})
							}

							ui.Divider(c)

							ui.Row(c.Key("row-reset")).
								Padding(10, 16).
								AlignItems(ui.Center).
								Gap(10).
								Children(func() {
									ui.Text(c, tr.ResetLoadoutLabel).FontSize(13).TextColor(t.Text).Grow(1)
									if !m.confirmReset {
										resetBtn := ui.ButtonBase(c.Key("btn-reset-defaults")).
											Padding(6, 11).
											Radius(2).
											Background(ui.RGBA(255, 69, 58, 0.10)).
											Border(1, ui.RGBA(255, 69, 58, 0.42)).
											Cursor(ui.CursorPointer)
										resetBtn.Children(func() {
											ui.Row(c).AlignItems(ui.Center).Gap(5).Children(func() {
												ui.Icon(c, hud.UIIconSVG("reset_warn")).Size(13, 13).TextColor(ui.Hex("#ff8078"))
												ui.Text(c, tr.ResetDefaults).FontSize(12).Bold().TextColor(ui.Hex("#ffb4ae"))
											})
										})
										if resetBtn.Clicked() {
											m.confirmReset = true
											m.okMsg = ""
										}
									} else {
										if ui.Button(c.Key("btn-cancel-reset"), tr.Cancel).Clicked() {
											m.confirmReset = false
										}
										confirmBtn := ui.ButtonBase(c.Key("btn-confirm-reset")).
											Padding(6, 12).
											Radius(2).
											Background(t.Danger).
											Border(1, ui.Hex("#ff736a")).
											Cursor(ui.CursorPointer)
										confirmBtn.Children(func() {
											ui.Text(c, tr.ConfirmReset).FontSize(12).Bold().TextColor(ui.Hex("#ffffff"))
										})
										if confirmBtn.Clicked() {
											m.confirmReset = false
											m.resetToDefaults()
										}
									}
								})
						})
				})

				if m.errMsg != "" {
					ui.Row(c.Key("settings-err")).Justify(ui.End).Children(func() {
						ui.Text(c, "⚠ "+m.errMsg).FontSize(12.5).Bold().TextColor(t.Danger)
					})
				} else if m.okMsg != "" {
					ui.Row(c.Key("settings-toast")).Justify(ui.End).Children(func() {
						ui.Text(c, m.okMsg).FontSize(12.5).Bold().TextColor(t.Success)
					})
				}
			})
		})
	})
}

func (m *Manager) renderRosterSidebar(c *ui.Context, t *ui.Theme, tr i18n.Strings) {
	canMoveUp, canMoveDown := m.selectedMoveBounds()

	ui.Column(c.Key("roster-col")).
		Width(236).
		Shrink(0).
		MinHeight(0).
		Padding(12).
		Gap(10).
		Background(ui.Hex("#0c1018")).
		Children(func() {
			ui.Row(c).Shrink(0).AlignItems(ui.Center).Gap(5).Children(func() {
				renderSectionHeader(c, t, tr.LoadoutHeader)
				ui.Spacer(c)
				if m.userStratagemCount() > 1 {
					m.renderCompactIconBtn(c, t, "btn-move-up", "▲", canMoveUp, func() {
						m.moveSelectedStratagem(-1)
					})
					m.renderCompactIconBtn(c, t, "btn-move-down", "▼", canMoveDown, func() {
						m.moveSelectedStratagem(1)
					})
				}
				if ui.Button(c.Key("btn-new-strat"), tr.NewStratagem).Clicked() {
					m.startNewDraft()
				}
			})

			ui.Scroll(c.Key("roster-scroll")).Grow(1).MinHeight(0).Children(func() {
				ui.Column(c).Gap(5).Children(func() {
					for _, st := range m.snap.Stratagems {
						isSelected := !m.isCreatingNew && m.selectedID == st.ID
						isSys := catalog.IsSystemStratagem(st.ID)
						rowBg := t.Surface
						rowBorder := ui.RGBA(255, 255, 255, 0.08)
						if isSelected {
							rowBg = ui.RGBA(255, 233, 0, 0.11)
							rowBorder = t.Accent
						} else if isSys {
							rowBorder = ui.RGBA(201, 178, 104, 0.32)
						}

						card := ui.ButtonBase(c.Key("roster-item-"+st.ID)).
							Padding(8, 10).
							Radius(2).
							Background(rowBg).
							Border(1.5, rowBorder).
							Cursor(ui.CursorPointer)
						if isSelected {
							card.DrawOver(func(p *ui.Painter, r ui.Rect) {
								p.Fill(ui.Rect{X: r.X + 1.5, Y: r.Y + 2, W: 3, H: r.H - 4}, t.Accent, 0)
							})
						}

						if !st.Enabled {
							card.Opacity(0.46)
						}

						card.Children(func() {
							ui.Row(c).Grow(1).MinWidth(0).AlignItems(ui.Center).Gap(9).Children(func() {
								ui.Row(c).
									Size(32, 32).
									Shrink(0).
									Center().
									Background(hud.CategoryFillColor(st.Category)).
									Border(1.5, hud.CategoryBorderColor(st.Category)).
									Children(func() {
										if svg := hud.IconSVG(st.Icon); svg != nil {
											ui.Image(c, svg).Size(22, 22)
										}
									})

								ui.Column(c).Grow(1).MinWidth(0).Gap(3).Children(func() {
									ui.Row(c).AlignItems(ui.Center).Gap(5).Children(func() {
										ui.Text(c, st.Name).Bold().FontSize(12.5).SingleLine().Grow(1)
										if isSys {
											ui.Row(c.Key("sys-pill")).
												Padding(1, 5).
												Radius(3).
												Shrink(0).
												Background(ui.RGBA(255, 233, 0, 0.14)).
												Border(1, ui.RGBA(255, 233, 0, 0.48)).
												Children(func() {
													ui.Text(c, tr.SystemBadge).FontSize(9.5).Bold().TextColor(t.Accent).SingleLine()
												})
										}
									})
									ui.Row(c).AlignItems(ui.Center).Gap(4).Height(14).Children(func() {
										arrowCol := t.Accent
										if !st.Enabled {
											arrowCol = t.TextMuted
										} else if isSys && !m.hdStatus.Found {
											arrowCol = ui.RGBA(255, 233, 0, 0.52)
										}
										for idx, dir := range st.Code {
											hud.RenderArrow(c.Key(idx), dir, 11.5, arrowCol)
										}
										if st.Delivery == catalog.DeliveryBeaconClick {
											ui.Spacer(c)
											ui.Text(c, "⊕").FontSize(10).Bold().TextColor(ui.Hex("#7dd3fc"))
										} else if isSys && !m.hdStatus.Found {
											ui.Spacer(c)
											ui.Box(c).Size(6, 6).Radius(3).Background(ui.Hex("#f59e0b"))
										}
									})
								})
							})
						})

						if card.Clicked() {
							m.selectStratagem(st.ID)
							if isSys {
								m.refreshHelldiversStatus()
							}
						}
					}
				})
			})
		})
}

func (m *Manager) renderCompactIconBtn(c *ui.Context, t *ui.Theme, key, glyph string, enabled bool, onClick func()) {
	bg := t.Surface
	fg := t.Text
	if !enabled {
		fg = ui.RGBA(255, 255, 255, 0.25)
	}
	btn := ui.ButtonBase(c.Key(key)).
		Size(26, 26).
		Shrink(0).
		Radius(2).
		Center().
		Background(bg).
		Border(1, t.Border).
		Disabled(!enabled)
	if enabled {
		btn.Cursor(ui.CursorPointer)
		if btn.Hovered() {
			btn.Background(t.SurfaceHover)
		}
	}
	btn.Children(func() {
		ui.Text(c, glyph).FontSize(10.5).Bold().TextColor(fg)
	})
	if enabled && btn.Clicked() {
		onClick()
	}
}

func (m *Manager) renderEditorPane(c *ui.Context, t *ui.Theme, tr i18n.Strings, lang catalog.Language) {
	if !m.isCreatingNew && catalog.IsSystemStratagem(m.selectedID) {
		m.renderSystemEnlistmentPane(c, t, tr)
		return
	}

	liveConflict := m.checkLiveConflict()

	ui.Column(c.Key("editor-col")).Grow(1).MinWidth(0).MinHeight(0).Children(func() {
		ui.Scroll(c.Key("editor-scroll")).Grow(1).MinWidth(0).MinHeight(0).Children(func() {
			ui.Column(c.Key("editor-form")).Padding(18, 22).Gap(16).Children(func() {
				// 1. Identity Row: [54x54 Icon Picker Button] + [Name Input & Category Swatches] + [Enable Switch]
				m.renderIdentitySection(c, t, tr, lang)

				// 2. Chord Sequence Box + Live Conflict Feedback
				m.renderChordSection(c, t, tr, lang, liveConflict)

				// 3. Action & Delivery Section
				m.renderActionSection(c, t, tr)

				// Validation error banner (for non-conflict errors upon Save)
				if m.errMsg != "" && liveConflict == nil {
					ui.Row(c.Key("banner-err")).
						Padding(8, 12).
						Radius(2).
						Background(ui.RGBA(255, 69, 58, 0.14)).
						Border(1, ui.RGBA(255, 69, 58, 0.55)).
						Children(func() {
							ui.Text(c, "⚠ "+m.errMsg).FontSize(12).Bold().TextColor(ui.Hex("#ffb4ae"))
						})
				}
			})
		})

		ui.Divider(c)

		// Sticky Footer Bar
		ui.Row(c.Key("editor-footer")).
			Height(52).
			Shrink(0).
			Padding(0, 22).
			Background(ui.Hex("#0c1018")).
			AlignItems(ui.Center).
			Gap(10).
			Children(func() {
				if !m.isCreatingNew && !catalog.IsSystemStratagem(m.selectedID) {
					delBtn := ui.ButtonBase(c.Key("btn-delete-strat")).
						Padding(6, 12).
						Radius(2).
						Background(ui.RGBA(255, 69, 58, 0.10)).
						Border(1, ui.RGBA(255, 69, 58, 0.42)).
						Cursor(ui.CursorPointer)
					delBtn.Children(func() {
						ui.Text(c, tr.Delete).FontSize(12.5).Bold().TextColor(ui.Hex("#ffb4ae"))
					})
					if delBtn.Clicked() {
						m.deleteSelectedStratagem()
					}
				}

				ui.Spacer(c)

				if m.okMsg != "" && !m.isDirty() {
					ui.Text(c.Key("toast-ok"), m.okMsg).FontSize(12).Bold().TextColor(t.Success)
				}

				if m.isDirty() {
					if ui.Button(c.Key("btn-revert"), tr.Revert).Clicked() {
						m.revertDraft()
					}
				}

				if ui.PrimaryButton(c.Key("btn-save"), tr.Save).Clicked() {
					m.saveDraft()
				}
			})
	})
}

func (m *Manager) renderSystemEnlistmentPane(c *ui.Context, t *ui.Theme, tr i18n.Strings) {
	ui.Column(c.Key("enlistment-col")).Grow(1).MinWidth(0).MinHeight(0).Children(func() {
		ui.Scroll(c.Key("enlistment-scroll")).Grow(1).MinWidth(0).MinHeight(0).Children(func() {
			ui.Column(c.Key("enlistment-body")).Padding(20, 24).Gap(16).Children(func() {
				// 1. Ministry of Truth Hero Terminal Card
				ui.Row(c.Key("enlist-hero")).
					Padding(16, 18).
					Radius(2).
					AlignItems(ui.Center).
					Gap(16).
					LinearGradient(ui.LinearGradient{
						Angle: 120,
						Stops: []ui.GradientStop{
							{Color: ui.RGBA(68, 56, 20, 0.36), At: 0},
							{Color: ui.Hex("#111722"), At: 0.65},
						},
					}).
					Border(1.5, ui.RGBA(201, 178, 104, 0.52)).
					Children(func() {
						ui.Row(c.Key("enlist-icon-frame")).
							Size(60, 60).
							Shrink(0).
							Radius(2).
							Center().
							Background(hud.CategoryFillColor(catalog.CategoryYellow)).
							Border(2, hud.CategoryBorderColor(catalog.CategoryYellow)).
							Shadow(0, 0, 14, 0, ui.RGBA(255, 233, 0, 0.22)).
							Children(func() {
								if svg := hud.IconSVG(catalog.SystemJoinHelldiversIcon); svg != nil {
									ui.Image(c, svg).Size(42, 42)
								}
							})

						ui.Column(c).Grow(1).MinWidth(0).Gap(5).Children(func() {
							ui.Row(c).AlignItems(ui.Center).Gap(8).Wrap().Children(func() {
								if emb := hud.EmblemSVG(); emb != nil {
									ui.Icon(c, emb).Size(12, 12).TextColor(t.Accent)
								}
								ui.Text(c, tr.EnlistBadge).
									FontSize(11).
									Bold().
									TextColor(t.Accent).
									SingleLine()
								if ship := strings.TrimSpace(m.snap.Settings.ShipName); ship != "" {
									ui.Row(c.Key("enlist-ship-pill")).
										Padding(2, 7).
										Radius(2).
										Background(ui.RGBA(255, 233, 0, 0.12)).
										Border(1, ui.RGBA(255, 233, 0, 0.36)).
										Children(func() {
											ui.Text(c, ship).FontSize(10.5).Bold().TextColor(t.Accent).SingleLine()
										})
								}
							})
							ui.Text(c, m.draftName).
								FontSize(18).
								Bold().
								TextColor(ui.Hex("#ffffff")).
								SingleLine()
						})
					})

				// 2. Fixed Hellpod Sequence Card (↑↓→←↑)
				ui.Row(c.Key("enlist-code-card")).
					Padding(13, 16).
					Radius(2).
					AlignItems(ui.Center).
					Gap(12).
					Wrap().
					Background(t.Surface).
					Border(1, t.Border).
					Children(func() {
						ui.Row(c).AlignItems(ui.Center).Gap(8).Grow(1).Children(func() {
							ui.Text(c, tr.EnlistLockedCodeLabel).FontSize(13).Bold().TextColor(t.Text)
							ui.Row(c.Key("enlist-locked-pill")).
								Padding(2, 6).
								Radius(2).
								Background(ui.RGBA(255, 255, 255, 0.06)).
								Border(1, ui.RGBA(255, 255, 255, 0.12)).
								Children(func() {
									ui.Text(c, tr.EnlistLockedTag).FontSize(10.5).Bold().TextColor(t.TextMuted).SingleLine()
								})
						})

						ui.Row(c.Key("enlist-arrows")).Gap(6).AlignItems(ui.Center).Children(func() {
							for idx, d := range catalog.SystemJoinHelldiversCode {
								ui.Row(c.Key(idx)).
									Size(30, 30).
									Center().
									Radius(2).
									Background(ui.RGBA(255, 233, 0, 0.14)).
									Border(1, ui.RGBA(255, 233, 0, 0.55)).
									Children(func() {
										hud.RenderArrow(c, d, 15, t.Accent)
									})
							}
						})
					})

				// 3. Hellpod Launch Coordinates Card (Search / Browse / Dive / Enlist)
				ui.Column(c.Key("enlist-coords-card")).
					Padding(16, 18).
					Radius(2).
					Gap(12).
					Background(t.Surface).
					Border(1, t.Border).
					Children(func() {
						// Title + Rescan button
						ui.Row(c).AlignItems(ui.Center).Gap(10).Children(func() {
							ui.Text(c, tr.EnlistCoordsTitle).FontSize(13).Bold().TextColor(t.Text).Grow(1)
							if ui.Button(c.Key("btn-hd-rescan"), tr.EnlistBtnRescan).Clicked() {
								m.rescanHelldivers(tr)
							}
						})

						// Status + Path / Empty State
						switch {
						case m.hdStatus.Found && m.hdStatus.Source == "steam":
							ui.Text(c, tr.EnlistStatusSteam).FontSize(12.5).Bold().TextColor(t.Success)
							m.renderCoordinatePathBox(c, m.hdStatus.Path, false)

							ui.Row(c.Key("enlist-actions-steam")).AlignItems(ui.Center).Gap(8).Wrap().Children(func() {
								diveBtn := ui.ButtonBase(c.Key("btn-hd-dive")).
									Height(32).
									Padding(0, 14).
									Radius(2).
									Center().
									Background(t.Accent).
									Border(1, ui.Hex("#fff37a")).
									Cursor(ui.CursorPointer)
								diveBtn.Children(func() {
									ui.Row(c).AlignItems(ui.Center).Gap(6).Children(func() {
										ui.Icon(c, hud.UIIconSVG("hellpod")).Size(14, 14).TextColor(t.AccentText)
										ui.Text(c, tr.EnlistBtnDiveNow).FontSize(12.5).Bold().TextColor(t.AccentText)
									})
								})
								if diveBtn.Clicked() {
									m.launchHelldiversNow()
								}
								if ui.Button(c.Key("btn-hd-browse"), tr.EnlistBtnChange).Disabled(m.browsingHD).Clicked() {
									m.browseHelldiversPath()
								}
							})

						case m.hdStatus.Found && m.hdStatus.Source == "custom":
							ui.Text(c, tr.EnlistStatusCustom).FontSize(12.5).Bold().TextColor(t.Success)
							m.renderCoordinatePathBox(c, m.hdStatus.Path, false)

							ui.Row(c.Key("enlist-actions-custom")).AlignItems(ui.Center).Gap(8).Wrap().Children(func() {
								diveBtn := ui.ButtonBase(c.Key("btn-hd-dive")).
									Height(32).
									Padding(0, 14).
									Radius(2).
									Center().
									Background(t.Accent).
									Border(1, ui.Hex("#fff37a")).
									Cursor(ui.CursorPointer)
								diveBtn.Children(func() {
									ui.Row(c).AlignItems(ui.Center).Gap(6).Children(func() {
										ui.Icon(c, hud.UIIconSVG("hellpod")).Size(14, 14).TextColor(t.AccentText)
										ui.Text(c, tr.EnlistBtnDiveNow).FontSize(12.5).Bold().TextColor(t.AccentText)
									})
								})
								if diveBtn.Clicked() {
									m.launchHelldiversNow()
								}
								if ui.Button(c.Key("btn-hd-browse"), tr.EnlistBtnChange).Disabled(m.browsingHD).Clicked() {
									m.browseHelldiversPath()
								}
								if ui.Button(c.Key("btn-hd-autodetect"), tr.EnlistBtnAutoDetect).Clicked() {
									m.setCustomHelldiversPath("")
								}
							})

						case m.hdStatus.CustomInvalid:
							ui.Text(c, tr.EnlistStatusBroken).FontSize(12.5).Bold().TextColor(t.Danger)
							m.renderCoordinatePathBox(c, m.hdStatus.CustomPath, true)

							ui.Row(c.Key("enlist-actions-broken")).AlignItems(ui.Center).Gap(8).Wrap().Children(func() {
								if ui.PrimaryButton(c.Key("btn-hd-browse"), tr.EnlistBtnBrowse).Disabled(m.browsingHD).Clicked() {
									m.browseHelldiversPath()
								}
								if ui.Button(c.Key("btn-hd-autodetect"), tr.EnlistBtnAutoDetect).Clicked() {
									m.setCustomHelldiversPath("")
								}
							})

						default:
							ui.Text(c, tr.EnlistStatusMissing).FontSize(12.5).Bold().TextColor(ui.Hex("#f59e0b"))

							ui.Row(c.Key("enlist-actions-missing")).AlignItems(ui.Center).Gap(8).Wrap().Children(func() {
								if ui.PrimaryButton(c.Key("btn-hd-browse"), tr.EnlistBtnBrowse).Disabled(m.browsingHD).Clicked() {
									m.browseHelldiversPath()
								}
								if m.hooks.OpenEnlistURL != nil {
									if ui.Button(c.Key("btn-hd-enlist"), tr.EnlistBtnSteamStore).Clicked() {
										m.hooks.OpenEnlistURL()
									}
								}
							})
						}
					})

				if m.errMsg != "" {
					ui.Row(c.Key("enlist-err")).
						Padding(8, 12).
						Radius(2).
						Background(ui.RGBA(255, 69, 58, 0.14)).
						Border(1, ui.RGBA(255, 69, 58, 0.55)).
						Children(func() {
							ui.Text(c, "⚠ "+m.errMsg).FontSize(12).Bold().TextColor(ui.Hex("#ffb4ae"))
						})
				} else if m.okMsg != "" {
					ui.Row(c.Key("enlist-ok")).Justify(ui.End).Children(func() {
						ui.Text(c, m.okMsg).FontSize(12).Bold().TextColor(t.Success)
					})
				}
			})
		})
	})
}

func (m *Manager) renderCoordinatePathBox(c *ui.Context, dispPath string, broken bool) {
	borderCol := ui.RGBA(255, 255, 255, 0.08)
	txtCol := ui.Hex("#dbe2ee")
	if broken {
		borderCol = ui.RGBA(255, 69, 58, 0.45)
		txtCol = ui.Hex("#ffb4ae")
	}
	ui.Row(c.Key("enlist-path-box")).
		Padding(9, 12).
		Radius(6).
		Background(ui.Hex("#11151d")).
		Border(1, borderCol).
		Children(func() {
			ui.Text(c, dispPath).FontSize(12).TextColor(txtCol).SingleLine().Grow(1)
		})
}

func (m *Manager) rescanHelldivers(tr i18n.Strings) {
	m.errMsg = ""
	m.refreshHelldiversStatus()
	if m.hdStatus.Found {
		m.okMsg = tr.EnlistScanFoundToast
	} else {
		m.okMsg = tr.EnlistScanMissingToast
	}
}

func (m *Manager) setCustomHelldiversPath(custom string) {
	trimmed := strings.TrimSpace(custom)
	lang := m.currentLang()
	tr := i18n.For(lang)
	if err := m.cat.Update(func(s *catalog.Snapshot) error {
		s.Settings.HelldiversPath = trimmed
		return nil
	}); err != nil {
		m.errMsg = i18n.FormatError(err, lang)
		m.okMsg = ""
		return
	}
	m.snap = m.cat.Snapshot()
	m.refreshHelldiversStatus()
	m.errMsg = ""
	if m.hdStatus.Found {
		m.okMsg = tr.SavedToast
	} else if trimmed == "" {
		m.okMsg = ""
	}
}

func (m *Manager) browseHelldiversPath() {
	if m.browsingHD {
		return
	}
	curPath := m.snap.Settings.HelldiversPath
	if curPath == "" && m.hdStatus.Path != "" {
		curPath = filepath.Dir(m.hdStatus.Path)
	}

	if m.hooks.BrowseHelldivers != nil {
		m.browsingHD = true
		m.hooks.BrowseHelldivers(m.win, curPath, func(picked string) {
			m.browsingHD = false
			if strings.TrimSpace(picked) != "" {
				m.setCustomHelldiversPath(picked)
			}
		})
		return
	}

	m.browsingHD = true
	win := m.win
	title := i18n.For(m.currentLang()).EnlistDialogTitle
	go func() {
		var filters []mygo.FileFilter
		if runtime.GOOS == "windows" {
			filters = []mygo.FileFilter{
				{Name: "HELLDIVERS 2", Extensions: []string{"exe", "lnk", "url"}},
				{Name: "All Files", Extensions: []string{"*"}},
			}
		} else if runtime.GOOS == "darwin" {
			filters = []mygo.FileFilter{
				{Name: "Applications", Extensions: []string{"app", "command", "sh"}},
			}
		}
		files, err := mygo.Dialog.Open(mygo.OpenDialogOptions{
			Parent:      win,
			Title:       title,
			DefaultPath: curPath,
			Filters:     filters,
		})
		applySelection := func() {
			m.browsingHD = false
			if err == nil && len(files) > 0 && strings.TrimSpace(files[0]) != "" {
				m.setCustomHelldiversPath(files[0])
			}
		}
		if win != nil {
			win.Update(applySelection)
		} else {
			applySelection()
		}
	}()
}

func (m *Manager) launchHelldiversNow() {
	if m.hooks.LaunchHelldivers != nil {
		m.hooks.LaunchHelldivers()
	}
}

func (m *Manager) renderIdentitySection(c *ui.Context, t *ui.Theme, tr i18n.Strings, lang catalog.Language) {
	namePlaceholder := tr.NamePlaceholderOther
	if runtime.GOOS == "windows" {
		namePlaceholder = tr.NamePlaceholderWin
	}

	ui.Row(c.Key("identity-row")).AlignItems(ui.Center).Gap(14).Children(func() {
		// Interactive 54x54 Stratagem Icon Tile -> opens visual Stratagem Icon Popover!
		iconBtn := ui.ButtonBase(c.Key("btn-pick-icon")).
			Size(54, 54).
			Shrink(0).
			Center().
			Background(hud.CategoryFillColor(m.draftCategory)).
			Border(2, hud.CategoryBorderColor(m.draftCategory)).
			Cursor(ui.CursorPointer)

		iconBtn.Children(func() {
			if svg := hud.IconSVG(m.draftIcon); svg != nil {
				ui.Image(c, svg).Size(36, 36)
			}
			ui.Row(c).
				Absolute().
				Right(2).
				Bottom(1).
				Children(func() {
					ui.Text(c, "▾").FontSize(9).Bold().TextColor(ui.RGBA(255, 255, 255, 0.72))
				})
		})
		if iconBtn.Clicked() {
			m.iconPickerOpen = !m.iconPickerOpen
		}

		m.renderIconPickerPopover(c, iconBtn, t, tr, lang)

		ui.Column(c).Grow(1).MinWidth(0).Gap(7).Children(func() {
			ui.Row(c).AlignItems(ui.Center).Gap(12).Children(func() {
				if ui.TextInput(c.Key("inp-name"), &m.draftName).Grow(1).Placeholder(namePlaceholder).Changed() {
					m.errMsg = ""
					m.okMsg = ""
				}
				if ui.Switch(c.Key("sw-enabled"), &m.draftEnabled).Changed() {
					m.errMsg = ""
					m.okMsg = ""
				}
			})

			// Category Color Swatches + Official Stratagem Icon Name Chip
			ui.Row(c.Key("cat-swatch-row")).AlignItems(ui.Center).Gap(7).Children(func() {
				categories := []catalog.Category{
					catalog.CategoryBlue,
					catalog.CategoryRed,
					catalog.CategoryGreen,
					catalog.CategoryYellow,
				}
				for _, cat := range categories {
					active := m.draftCategory == cat
					borderCol := ui.RGBA(255, 255, 255, 0.18)
					if active {
						borderCol = ui.Hex("#ffffff")
					}
					sw := ui.ButtonBase(c.Key("swatch-"+string(cat))).
						Size(18, 18).
						Radius(4).
						Background(hud.CategoryBorderColor(cat)).
						Border(2, borderCol).
						Cursor(ui.CursorPointer)
					if sw.Clicked() {
						m.draftCategory = cat
						m.okMsg = ""
					}
				}

				ui.Box(c).Size(1, 12).Background(ui.RGBA(255, 255, 255, 0.10))

				chip := ui.ButtonBase(c.Key("chip-icon-name")).
					Padding(2, 7).
					Radius(4).
					Background(ui.RGBA(255, 255, 255, 0.05)).
					Cursor(ui.CursorPointer)
				chip.Children(func() {
					ui.Text(c, i18n.IconName(m.draftIcon, lang)+" ▾").
						FontSize(11.5).
						TextColor(t.TextMuted).
						SingleLine()
				})
				if chip.Clicked() {
					m.iconPickerOpen = !m.iconPickerOpen
				}
			})
		})
	})
}

func (m *Manager) filteredIcons() []i18n.IconMeta {
	q := strings.ToLower(strings.TrimSpace(m.iconSearch))
	var wantCat catalog.Category
	switch m.iconCatFilter {
	case 1:
		wantCat = catalog.CategoryRed
	case 2:
		wantCat = catalog.CategoryBlue
	case 3:
		wantCat = catalog.CategoryGreen
	case 4:
		wantCat = catalog.CategoryYellow
	}

	out := make([]i18n.IconMeta, 0, len(i18n.Icons))
	for _, ic := range i18n.Icons {
		if wantCat != "" && ic.Category != wantCat {
			continue
		}
		if q != "" &&
			!strings.Contains(strings.ToLower(ic.NameZH), q) &&
			!strings.Contains(strings.ToLower(ic.NameEN), q) {
			continue
		}
		out = append(out, ic)
	}
	return out
}

func (m *Manager) renderIconPickerPopover(
	c *ui.Context,
	anchor ui.Element,
	t *ui.Theme,
	tr i18n.Strings,
	lang catalog.Language,
) {
	ui.PopoverBase(c.Key("pop-icons"), anchor, &m.iconPickerOpen, func(panel ui.Element) {
		panel.
			Width(452).
			MaxHeight(410).
			Padding(10).
			Radius(2).
			Background(ui.Hex("#181e28")).
			Border(1, ui.RGBA(255, 255, 255, 0.14)).
			Shadow(0, 12, 32, 0, ui.RGBA(0, 0, 0, 0.60)).
			Children(func() {
				ui.Column(c).Gap(8).Children(func() {
					ui.TextInput(c.Key("inp-icon-search"), &m.iconSearch).
						Placeholder(tr.SearchIconsPlaceholder)

					// Category filter tabs
					ui.Row(c.Key("icon-cat-tabs")).Gap(4).AlignItems(ui.Center).Children(func() {
						filters := []struct {
							idx   int
							label string
						}{
							{0, tr.CatAll},
							{1, tr.CatOffensive},
							{2, tr.CatSupply},
							{3, tr.CatDefensive},
							{4, tr.CatMission},
						}
						for _, f := range filters {
							active := m.iconCatFilter == f.idx
							bg := ui.RGBA(255, 255, 255, 0.04)
							bdr := ui.Transparent
							fg := t.TextMuted
							if active {
								bg = ui.RGBA(255, 233, 0, 0.14)
								bdr = ui.RGBA(255, 233, 0, 0.55)
								fg = t.Accent
							}
							pill := ui.ButtonBase(c.Key(fmt.Sprintf("icon-cat-%d", f.idx))).
								Height(24).
								Padding(0, 9).
								Radius(2).
								Center().
								Background(bg).
								Border(1, bdr).
								Cursor(ui.CursorPointer)
							pill.Children(func() {
								ui.Text(c, f.label).FontSize(11.5).Bold().TextColor(fg).SingleLine()
							})
							if pill.Clicked() {
								m.iconCatFilter = f.idx
							}
						}
					})

					filtered := m.filteredIcons()
					ui.Scroll(c.Key("icon-grid-scroll")).MaxHeight(304).Children(func() {
						ui.Grid(c.Key("icon-grid")).Columns(2).Gap(6).Children(func() {
							for _, ic := range filtered {
								ic := ic
								selected := m.draftIcon == ic.Key
								itemBg := ui.Hex("#121720")
								itemBorder := ui.RGBA(255, 255, 255, 0.08)
								if selected {
									itemBg = ui.RGBA(255, 233, 0, 0.14)
									itemBorder = t.Accent
								}
								btn := ui.ButtonBase(c.Key("pick-icon-"+ic.Key)).
									MinWidth(0).
									Padding(6, 8).
									Radius(2).
									Background(itemBg).
									Border(1.5, itemBorder).
									Cursor(ui.CursorPointer)
								btn.Children(func() {
									ui.Row(c).Grow(1).MinWidth(0).AlignItems(ui.Center).Gap(8).Children(func() {
										ui.Row(c).
											Size(30, 30).
											Shrink(0).
											Center().
											Background(hud.CategoryFillColor(ic.Category)).
											Border(1.5, hud.CategoryBorderColor(ic.Category)).
											Children(func() {
												if svg := hud.IconSVG(ic.Key); svg != nil {
													ui.Image(c, svg).Size(21, 21)
												}
											})
										ui.Column(c).Grow(1).MinWidth(0).Gap(2).Children(func() {
											ui.Text(c, ic.Name(lang)).
												FontSize(11.5).
												Bold().
												SingleLine()
											ui.Row(c).Gap(3).Height(11).AlignItems(ui.Center).Children(func() {
												for aIdx, d := range ic.Code {
													hud.RenderArrow(c.Key(aIdx), d, 9.5, ui.RGBA(255, 255, 255, 0.50))
												}
											})
										})
									})
								})
								if btn.Clicked() {
									m.draftIcon = ic.Key
									m.draftCategory = ic.Category
									if len(m.draftCode) == 0 && len(ic.Code) > 0 {
										m.draftCode = slices.Clone(ic.Code)
									}
									m.iconPickerOpen = false
									m.errMsg = ""
									m.okMsg = ""
								}
							}
						})
					})
				})
			})
	})
}

func (m *Manager) renderChordSection(
	c *ui.Context,
	t *ui.Theme,
	tr i18n.Strings,
	lang catalog.Language,
	liveConflict error,
) {
	borderCol := ui.RGBA(255, 255, 255, 0.10)
	if liveConflict != nil {
		borderCol = t.Danger
	} else if m.recording {
		borderCol = t.Accent
	}

	ui.Column(c.Key("chord-section")).Gap(8).Children(func() {
		chordBox := ui.ButtonBase(c.Key("chord-box")).
			Bind(&m.chordHandle).
			Height(52).
			Padding(0, 14).
			Radius(2).
			Background(ui.Hex("#0e131c")).
			Border(1.5, borderCol).
			Cursor(ui.CursorPointer)

		chordBox.Children(func() {
			ui.Row(c).Grow(1).AlignItems(ui.Center).Gap(7).Children(func() {
				if len(m.draftCode) == 0 {
					placeholder := tr.SequencePlaceholder
					col := t.TextMuted
					if m.recording {
						placeholder = tr.RecordingPlaceholder
						col = t.Accent
					}
					ui.Text(c, placeholder).FontSize(12.5).TextColor(col)
				} else {
					for idx, d := range m.draftCode {
						ui.Row(c.Key(idx)).
							Size(30, 30).
							Center().
							Radius(2).
							Background(ui.RGBA(255, 233, 0, 0.14)).
							Border(1, ui.RGBA(255, 233, 0, 0.55)).
							Children(func() {
								hud.RenderArrow(c, d, 15, t.Accent)
							})
					}
				}
				ui.Spacer(c)
				if m.recording {
					ui.Box(c).Size(8, 8).Radius(1).Background(t.Accent)
				}
			})
		})

		if chordBox.Clicked() {
			m.recording = !m.recording
			if m.recording {
				m.chordHandle.Focus(c)
			}
		}

		// Compact directional step bar
		ui.Row(c.Key("chord-pad")).Gap(6).AlignItems(ui.Center).Wrap().Children(func() {
			addStep := func(d catalog.Dir) {
				if len(m.draftCode) < 12 {
					m.draftCode = append(m.draftCode, d)
					m.errMsg = ""
					m.okMsg = ""
				}
			}
			if ui.Button(c.Key("pad-up"), "↑").Clicked() {
				addStep(catalog.DirUp)
			}
			if ui.Button(c.Key("pad-down"), "↓").Clicked() {
				addStep(catalog.DirDown)
			}
			if ui.Button(c.Key("pad-left"), "←").Clicked() {
				addStep(catalog.DirLeft)
			}
			if ui.Button(c.Key("pad-right"), "→").Clicked() {
				addStep(catalog.DirRight)
			}
			if ui.Button(c.Key("pad-back"), "⌫").Disabled(len(m.draftCode) == 0).Clicked() {
				m.draftCode = m.draftCode[:len(m.draftCode)-1]
				m.errMsg = ""
				m.okMsg = ""
			}
			if ui.Button(c.Key("pad-clear"), tr.ClearSequence).Disabled(len(m.draftCode) == 0).Clicked() {
				m.draftCode = nil
				m.errMsg = ""
				m.okMsg = ""
			}

			ui.Spacer(c)

			if ic, ok := i18n.FindIcon(m.draftIcon); ok && len(ic.Code) > 0 && !slices.Equal(m.draftCode, ic.Code) {
				if ui.Button(c.Key("btn-official-code"), tr.UseOfficialCodeBtn).Clicked() {
					m.draftCode = slices.Clone(ic.Code)
					m.errMsg = ""
					m.okMsg = ""
				}
			}

			recLabel := tr.RecordSequence
			if m.recording {
				recLabel = tr.StopRecording
			}
			if ui.Button(c.Key("btn-toggle-record"), recLabel).Clicked() {
				m.recording = !m.recording
				if m.recording {
					m.chordHandle.Focus(c)
				}
			}
		})

		if liveConflict != nil {
			ui.Row(c.Key("live-conflict")).
				Padding(7, 10).
				Radius(5).
				Background(ui.RGBA(255, 69, 58, 0.14)).
				Border(1, ui.RGBA(255, 69, 58, 0.55)).
				Children(func() {
					ui.Text(c, "⚠ "+i18n.FormatError(liveConflict, lang)).
						FontSize(12).
						Bold().
						TextColor(ui.Hex("#ffb4ae"))
				})
		}
	})
}

func (m *Manager) renderActionSection(c *ui.Context, t *ui.Theme, tr i18n.Strings) {
	lang := m.currentLang()
	m.ensureValidActiveStep()
	activeStep := &m.draftSteps[m.activeStepIdx]

	ui.Column(c.Key("action-section")).Gap(10).Children(func() {
		// Multi-step combo pipeline bar (shown when 2+ steps exist)
		if len(m.draftSteps) > 1 {
			ui.Row(c.Key("combo-step-bar")).AlignItems(ui.Center).Gap(5).Wrap().Children(func() {
				for idx, step := range m.draftSteps {
					isAct := idx == m.activeStepIdx
					bg := ui.RGBA(255, 255, 255, 0.04)
					bdr := ui.RGBA(255, 255, 255, 0.10)
					fg := t.TextMuted
					if isAct {
						bg = ui.RGBA(255, 233, 0, 0.15)
						bdr = t.Accent
						fg = t.Accent
					}
					pill := ui.ButtonBase(c.Key(fmt.Sprintf("step-pill-%d", idx))).
						Height(26).
						Padding(0, 9).
						Radius(2).
						Center().
						Background(bg).
						Border(1, bdr).
						Cursor(ui.CursorPointer)
					pill.Children(func() {
						label := fmt.Sprintf(tr.StepBadgeFmt, idx+1) + " · " + m.stepShortLabel(step, tr)
						ui.Text(c, label).FontSize(11.5).Bold().TextColor(fg).SingleLine()
					})
					if pill.Clicked() {
						m.selectActiveStep(idx)
					}
					if idx < len(m.draftSteps)-1 {
						ui.Text(c, "›").FontSize(12).TextColor(ui.RGBA(255, 255, 255, 0.30))
					}
				}

				if len(m.draftSteps) < maxComboSteps {
					if ui.Button(c.Key("btn-add-step"), "+").Clicked() {
						m.appendComboStep()
					}
				}

				ui.Spacer(c)

				if ui.Button(c.Key("btn-remove-step"), tr.RemoveStep).Clicked() {
					m.removeActiveStep()
					activeStep = &m.draftSteps[m.activeStepIdx]
				}
			})
		}

		ui.Row(c.Key("action-mode-row")).AlignItems(ui.Center).Gap(8).Wrap().Children(func() {
			if ui.Segmented(
				c.Key("seg-step-kind"),
				&m.stepKindIdx,
				tr.StepApp,
				tr.StepOpen,
				tr.StepShell,
				tr.StepShortcut,
				tr.StepSystem,
			).Changed() {
				if m.stepKindIdx == 4 {
					if activeStep.Kind != catalog.StepCloseActiveWindow && activeStep.Kind != catalog.StepSystemAction {
						activeStep.Kind = catalog.StepCloseActiveWindow
						activeStep.Target = ""
						m.draftTarget = ""
					}
				} else {
					prevWasSystem := activeStep.Kind == catalog.StepCloseActiveWindow || activeStep.Kind == catalog.StepSystemAction
					activeStep.Kind = indexToStepKind(m.stepKindIdx)
					if prevWasSystem {
						activeStep.Target = ""
						m.draftTarget = ""
					}
				}
				m.errMsg = ""
				m.okMsg = ""
			}

			ui.Spacer(c)

			if ui.Segmented(
				c.Key("seg-delivery"),
				&m.deliveryIdx,
				tr.DeliveryImmediate,
				tr.DeliveryBeacon,
			).Changed() {
				if m.deliveryIdx == 1 {
					m.draftDelivery = catalog.DeliveryBeaconClick
				} else {
					m.draftDelivery = catalog.DeliveryImmediate
				}
				m.okMsg = ""
			}
		})

		if m.stepKindIdx == 4 {
			// Crisp 2-column grid of built-in zero-config Window & System Tactics
			ui.Grid(c.Key("sys-tactics-grid")).Columns(2).Gap(7).Children(func() {
				for _, tac := range i18n.SystemTactics {
					tac := tac
					selected := (tac.IsCloseWindow && activeStep.Kind == catalog.StepCloseActiveWindow) ||
						(!tac.IsCloseWindow && activeStep.Kind == catalog.StepSystemAction && activeStep.Target == string(tac.Action))

					tileBg := ui.Hex("#0f141d")
					tileBdr := ui.RGBA(255, 255, 255, 0.10)
					iconCol := t.TextMuted
					nameCol := t.Text
					if selected {
						tileBg = ui.RGBA(255, 233, 0, 0.12)
						tileBdr = t.Accent
						iconCol = t.Accent
						nameCol = ui.Hex("#ffffff")
					}

					tileKey := "tactic-close-win"
					if !tac.IsCloseWindow {
						tileKey = "tactic-" + string(tac.Action)
					}
					tile := ui.ButtonBase(c.Key(tileKey)).
						MinWidth(0).
						Height(38).
						Padding(0, 12).
						Radius(2).
						Background(tileBg).
						Border(1.5, tileBdr).
						Cursor(ui.CursorPointer)
					if selected {
						tile.DrawOver(func(p *ui.Painter, r ui.Rect) {
							p.Fill(ui.Rect{X: r.X + 1.5, Y: r.Y + 2, W: 3, H: r.H - 4}, t.Accent, 0)
						})
					}
					tile.Children(func() {
						ui.Row(c).Grow(1).MinWidth(0).AlignItems(ui.Center).Gap(8).Children(func() {
							if svg := hud.UIIconSVG(tac.IconKey); svg != nil {
								ui.Icon(c, svg).Size(15, 15).TextColor(iconCol)
							} else {
								ui.Text(c, tac.Glyph).FontSize(13).Bold().TextColor(iconCol)
							}
							ui.Text(c, tac.Name(lang)).FontSize(12).Bold().TextColor(nameCol).SingleLine().Grow(1)
						})
					})
					if tile.Clicked() {
						if tac.IsCloseWindow {
							activeStep.Kind = catalog.StepCloseActiveWindow
							activeStep.Target = ""
							m.draftTarget = ""
						} else {
							activeStep.Kind = catalog.StepSystemAction
							activeStep.Target = string(tac.Action)
							m.draftTarget = string(tac.Action)
						}
						if len(m.draftSteps) == 1 && m.isDefaultDraftName() {
							m.draftName = tac.Name(lang)
						}
						m.errMsg = ""
						m.okMsg = ""
					}
				}
			})
		} else {
			placeholder := m.targetPlaceholder(activeStep.Kind, tr)
			if ui.TextInput(c.Key("inp-step-target"), &m.draftTarget).Placeholder(placeholder).Changed() {
				activeStep.Target = m.draftTarget
				m.errMsg = ""
				m.okMsg = ""
			}
		}

		// Quick action helper chips: {clipboard} placeholder + Add Combo Step
		showClipChip := activeStep.Kind == catalog.StepOpenTarget || activeStep.Kind == catalog.StepShellCommand
		showAddCombo := len(m.draftSteps) == 1
		if showClipChip || showAddCombo {
			ui.Row(c.Key("action-chips-row")).AlignItems(ui.Center).Gap(8).Wrap().Children(func() {
				if showClipChip {
					if ui.Button(c.Key("btn-insert-clipboard"), tr.InsertClipboardChip).Clicked() {
						if !strings.Contains(m.draftTarget, "{clipboard}") {
							m.draftTarget += "{clipboard}"
							activeStep.Target = m.draftTarget
							m.errMsg = ""
							m.okMsg = ""
						}
					}
				}
				ui.Spacer(c)
				if showAddCombo {
					if ui.Button(c.Key("btn-add-step"), tr.AddComboStep).Clicked() {
						m.appendComboStep()
					}
				}
			})
		}

		if activeStep.Kind == catalog.StepLaunchApp && len(m.apps) > 0 {
			ui.Column(c.Key("app-picker")).Gap(7).Children(func() {
				ui.TextInput(c.Key("inp-app-filter"), &m.appQuery).Placeholder(tr.FilterApps)

				filtered := m.filteredApps()
				ui.ScrollHorizontal(c.Key("app-chips")).Children(func() {
					ui.Row(c).Gap(6).Padding(2, 0).Children(func() {
						limit := min(len(filtered), 20)
						for i := range limit {
							app := filtered[i]
							targetVal := app.ID
							if targetVal == "" {
								targetVal = app.Path
							}
							if ui.Button(c.Key("pick-app-"+targetVal+"-"+app.Name), app.Name).Clicked() {
								m.draftTarget = targetVal
								activeStep.Target = targetVal
								if len(m.draftSteps) == 1 && m.isDefaultDraftName() {
									m.draftName = app.Name
								}
								m.errMsg = ""
								m.okMsg = ""
							}
						}
					})
				})
			})
		}
	})
}

func (m *Manager) renderPreviewStage(c *ui.Context, t *ui.Theme, tr i18n.Strings, width float32) {
	ui.Column(c.Key("preview-col")).
		Width(width).
		Shrink(0).
		MinHeight(0).
		Padding(12, 14).
		Gap(10).
		Background(ui.Hex("#0c1018")).
		Children(func() {
			// Header: Live Preview + Optional Desktop Overlay trigger
			ui.Row(c.Key("preview-hdr")).Shrink(0).AlignItems(ui.Center).Gap(8).Children(func() {
				renderSectionHeader(c, t, tr.PreviewTitle)
				ui.Spacer(c)
				if m.hooks.PreviewHUD != nil && m.navTab == tabStratagems {
					if ui.Button(c.Key("btn-stage-popout"), tr.PreviewDesktopBtn).Clicked() {
						m.hooks.PreviewHUD()
					}
				}
			})

			// Orbital Atmospheric Horizon + Live GPU HUD Preview
			stage := ui.Column(c.Key("preview-stage")).
				Grow(1).
				MinHeight(0).
				Radius(2).
				Clip().
				Padding(14).
				Border(1, ui.RGBA(255, 255, 255, 0.12)).
				LinearGradient(ui.LinearGradient{
					Angle: 165,
					Stops: []ui.GradientStop{
						{Color: ui.Hex("#0b121c"), At: 0},
						{Color: ui.Hex("#1b2b3d"), At: 0.48},
						{Color: ui.Hex("#263442"), At: 0.68},
						{Color: ui.Hex("#0a0f16"), At: 1},
					},
				}).
				Draw(func(p *ui.Painter, r ui.Rect) {
					// Subtle orbital terminator glow + telemetry corner brackets
					p.Fill(ui.Rect{X: r.X + r.W*0.12, Y: r.Y + r.H*0.52, W: r.W * 0.76, H: 46}, ui.RGBA(96, 178, 224, 0.18), 23)
					p.Fill(ui.Rect{X: r.X + 20, Y: r.Y + 26, W: 140, H: 60}, ui.RGBA(255, 233, 0, 0.06), 12)
					bCol := ui.RGBA(255, 233, 0, 0.40)
					const arm = 10
					p.Fill(ui.Rect{X: r.X + 6, Y: r.Y + 6, W: arm, H: 1.5}, bCol, 0)
					p.Fill(ui.Rect{X: r.X + 6, Y: r.Y + 6, W: 1.5, H: arm}, bCol, 0)
					p.Fill(ui.Rect{X: r.X + r.W - 6 - arm, Y: r.Y + 6, W: arm, H: 1.5}, bCol, 0)
					p.Fill(ui.Rect{X: r.X + r.W - 7.5, Y: r.Y + 6, W: 1.5, H: arm}, bCol, 0)
					p.Fill(ui.Rect{X: r.X + 6, Y: r.Y + r.H - 7.5, W: arm, H: 1.5}, bCol, 0)
					p.Fill(ui.Rect{X: r.X + 6, Y: r.Y + r.H - 6 - arm, W: 1.5, H: arm}, bCol, 0)
					p.Fill(ui.Rect{X: r.X + r.W - 6 - arm, Y: r.Y + r.H - 7.5, W: arm, H: 1.5}, bCol, 0)
					p.Fill(ui.Rect{X: r.X + r.W - 7.5, Y: r.Y + r.H - 6 - arm, W: 1.5, H: arm}, bCol, 0)
				})

			stage.Children(func() {
				ui.Scroll(c.Key("preview-scroll")).Grow(1).MinHeight(0).Children(func() {
					hud.Preview(c, m.buildPreviewSnapshot())
				})
			})

			// Simulation state bar
			ui.Row(c.Key("preview-sim-bar")).Shrink(0).AlignItems(ui.Center).Gap(8).Children(func() {
				ui.Segmented(
					c.Key("seg-sim"),
					&m.simIdx,
					tr.SimReady,
					tr.SimTyping,
					tr.SimDeploying,
				)
			})
		})
}

func (m *Manager) buildPreviewSnapshot() engine.Snapshot {
	loadout := m.candidateSnapshot()
	snap := engine.Snapshot{
		Phase:           engine.PhaseOpen,
		Trusted:         true,
		HelldiversReady: m.hdStatus.Found,
		Loadout:         loadout,
	}

	switch m.simIdx {
	case 1: // SimTyping: highlight partial prefix of the currently edited Stratagem
		if len(m.draftCode) > 1 {
			snap.Typed = slices.Clone(m.draftCode[:len(m.draftCode)-1])
		} else if len(m.draftCode) == 1 {
			snap.Typed = slices.Clone(m.draftCode)
		}
	case 2: // SimDeploying / Armed
		snap.MatchedID = m.draftID
		snap.Typed = slices.Clone(m.draftCode)
		if m.draftDelivery == catalog.DeliveryBeaconClick {
			snap.Phase = engine.PhaseArmed
		} else {
			snap.Phase = engine.PhaseDeploying
		}
	}
	return snap
}

func (m *Manager) isDefaultDraftName() bool {
	n := strings.TrimSpace(m.draftName)
	return n == "" ||
		n == i18n.For(catalog.LangZH).DefaultNewName ||
		n == i18n.For(catalog.LangEN).DefaultNewName
}

func (m *Manager) ensureValidActiveStep() {
	if len(m.draftSteps) == 0 {
		m.draftSteps = []catalog.Step{{Kind: catalog.StepLaunchApp}}
	}
	if m.activeStepIdx < 0 || m.activeStepIdx >= len(m.draftSteps) {
		m.activeStepIdx = 0
	}
}

func (m *Manager) selectActiveStep(idx int) {
	m.ensureValidActiveStep()
	if idx < 0 || idx >= len(m.draftSteps) {
		idx = 0
	}
	m.activeStepIdx = idx
	st := m.draftSteps[idx]
	m.stepKindIdx = stepKindToIndex(st.Kind)
	m.draftTarget = st.Target
}

func (m *Manager) appendComboStep() {
	m.ensureValidActiveStep()
	if len(m.draftSteps) >= maxComboSteps {
		return
	}
	m.draftSteps = append(m.draftSteps, catalog.Step{Kind: catalog.StepSystemAction, Target: string(catalog.SysWinLeft)})
	m.selectActiveStep(len(m.draftSteps) - 1)
	m.errMsg = ""
	m.okMsg = ""
}

func (m *Manager) removeActiveStep() {
	m.ensureValidActiveStep()
	if len(m.draftSteps) <= 1 {
		return
	}
	m.draftSteps = slices.Delete(m.draftSteps, m.activeStepIdx, m.activeStepIdx+1)
	if m.activeStepIdx >= len(m.draftSteps) {
		m.activeStepIdx = len(m.draftSteps) - 1
	}
	m.selectActiveStep(m.activeStepIdx)
	m.errMsg = ""
	m.okMsg = ""
}

func (m *Manager) stepShortLabel(s catalog.Step, tr i18n.Strings) string {
	switch s.Kind {
	case catalog.StepLaunchApp:
		return tr.StepApp
	case catalog.StepOpenTarget:
		return tr.StepOpen
	case catalog.StepShellCommand:
		return tr.StepShell
	case catalog.StepRunShortcut:
		return tr.StepShortcut
	default:
		return tr.StepSystem
	}
}

func (m *Manager) normalizedDraftSteps() []catalog.Step {
	if len(m.draftSteps) == 0 {
		return []catalog.Step{{Kind: catalog.StepLaunchApp, Target: strings.TrimSpace(m.draftTarget)}}
	}
	out := make([]catalog.Step, len(m.draftSteps))
	for i, s := range m.draftSteps {
		target := strings.TrimSpace(s.Target)
		if s.Kind == catalog.StepCloseActiveWindow {
			target = ""
		}
		out[i] = catalog.Step{
			Kind:   s.Kind,
			Target: target,
			Args:   slices.Clone(s.Args),
		}
	}
	return out
}

func stepsEqual(a, b []catalog.Step) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		ta := strings.TrimSpace(a[i].Target)
		if a[i].Kind == catalog.StepCloseActiveWindow {
			ta = ""
		}
		tb := strings.TrimSpace(b[i].Target)
		if b[i].Kind == catalog.StepCloseActiveWindow {
			tb = ""
		}
		if a[i].Kind != b[i].Kind || ta != tb || !slices.Equal(a[i].Args, b[i].Args) {
			return false
		}
	}
	return true
}

func (m *Manager) selectedMoveBounds() (canUp, canDown bool) {
	if m.isCreatingNew || m.selectedID == "" || catalog.IsSystemStratagem(m.selectedID) {
		return false, false
	}
	idx := -1
	sysIdx := len(m.snap.Stratagems)
	for i, st := range m.snap.Stratagems {
		if catalog.IsSystemStratagem(st.ID) && sysIdx == len(m.snap.Stratagems) {
			sysIdx = i
		}
		if st.ID == m.selectedID {
			idx = i
		}
	}
	if idx < 0 || idx >= sysIdx {
		return false, false
	}
	return idx > 0, idx+1 < sysIdx
}

func (m *Manager) moveSelectedStratagem(delta int) {
	canUp, canDown := m.selectedMoveBounds()
	if (delta < 0 && !canUp) || (delta > 0 && !canDown) {
		return
	}
	targetID := m.selectedID
	_ = m.cat.Update(func(s *catalog.Snapshot) error {
		for i := range s.Stratagems {
			if s.Stratagems[i].ID == targetID {
				j := i + delta
				if j >= 0 && j < len(s.Stratagems) && !catalog.IsSystemStratagem(s.Stratagems[j].ID) {
					s.Stratagems[i], s.Stratagems[j] = s.Stratagems[j], s.Stratagems[i]
				}
				break
			}
		}
		return nil
	})
}

func (m *Manager) applyOpenAtLogin(enable bool) {
	if m.hooks.SetOpenAtLogin != nil {
		if err := m.hooks.SetOpenAtLogin(enable); err != nil {
			m.errMsg = err.Error()
			if m.hooks.GetOpenAtLogin != nil {
				m.openAtLogin = m.hooks.GetOpenAtLogin()
			} else {
				m.openAtLogin = !enable
			}
		}
	}
}

func (m *Manager) exportBackupJSON() {
	if m.exporting {
		return
	}
	lang := m.currentLang()
	tr := i18n.For(lang)
	data, err := m.cat.ExportJSON()
	if err != nil {
		m.errMsg = err.Error()
		return
	}

	if m.hooks.ExportBackup != nil {
		m.exporting = true
		m.hooks.ExportBackup(m.win, data, func(savedPath string, saveErr error) {
			m.exporting = false
			if saveErr != nil {
				m.errMsg = saveErr.Error()
				m.okMsg = ""
			} else if strings.TrimSpace(savedPath) != "" {
				m.errMsg = ""
				m.okMsg = tr.ExportedToast
			}
		})
		return
	}

	m.exporting = true
	win := m.win
	title := tr.ExportDialogTitle
	go func() {
		savePath, dErr := mygo.Dialog.Save(mygo.SaveDialogOptions{
			Parent:      win,
			Title:       title,
			DefaultPath: "hellauncher-loadout.json",
			Filters: []mygo.FileFilter{
				{Name: "JSON", Extensions: []string{"json"}},
			},
		})
		var wErr error
		if dErr != nil {
			wErr = dErr
		} else if strings.TrimSpace(savePath) != "" {
			wErr = os.WriteFile(savePath, data, 0o644)
		}
		finish := func() {
			m.exporting = false
			if wErr != nil {
				m.errMsg = wErr.Error()
				m.okMsg = ""
			} else if strings.TrimSpace(savePath) != "" {
				m.errMsg = ""
				m.okMsg = tr.ExportedToast
			}
		}
		if win != nil {
			win.Update(finish)
		} else {
			finish()
		}
	}()
}

func (m *Manager) importBackupJSON() {
	if m.importing {
		return
	}
	lang := m.currentLang()
	tr := i18n.For(lang)

	applyImported := func(raw []byte, readErr error) {
		m.importing = false
		if readErr != nil {
			m.errMsg = readErr.Error()
			m.okMsg = ""
			return
		}
		if len(raw) == 0 {
			return
		}
		if err := m.cat.ImportJSON(raw); err != nil {
			m.errMsg = i18n.FormatError(err, m.currentLang())
			m.okMsg = ""
			return
		}
		m.reloadSnapshot(m.cat.Snapshot(), true)
		m.refreshHelldiversStatus()
		m.errMsg = ""
		m.okMsg = i18n.For(m.currentLang()).ImportedToast
	}

	if m.hooks.ImportBackup != nil {
		m.importing = true
		m.hooks.ImportBackup(m.win, applyImported)
		return
	}

	m.importing = true
	win := m.win
	title := tr.ImportDialogTitle
	go func() {
		files, dErr := mygo.Dialog.Open(mygo.OpenDialogOptions{
			Parent: win,
			Title:  title,
			Filters: []mygo.FileFilter{
				{Name: "JSON", Extensions: []string{"json"}},
			},
		})
		var (
			raw  []byte
			rErr error
		)
		if dErr != nil {
			rErr = dErr
		} else if len(files) > 0 && strings.TrimSpace(files[0]) != "" {
			raw, rErr = os.ReadFile(files[0])
		}
		if win != nil {
			win.Update(func() {
				applyImported(raw, rErr)
			})
		} else {
			applyImported(raw, rErr)
		}
	}()
}

func (m *Manager) candidateStratagem() catalog.Stratagem {
	if !m.isCreatingNew && catalog.IsSystemStratagem(m.selectedID) {
		for _, st := range m.snap.Stratagems {
			if st.ID == m.selectedID {
				return st
			}
		}
	}

	name := strings.TrimSpace(m.draftName)
	if name == "" {
		name = i18n.For(m.currentLang()).DefaultNewName
	}
	return catalog.Stratagem{
		ID:       m.draftID,
		Name:     name,
		Subtitle: strings.TrimSpace(m.draftSubtitle),
		Category: m.draftCategory,
		Icon:     m.draftIcon,
		Enabled:  m.draftEnabled,
		Code:     slices.Clone(m.draftCode),
		Delivery: m.draftDelivery,
		Steps:    m.normalizedDraftSteps(),
	}
}

func (m *Manager) candidateSnapshot() catalog.Snapshot {
	out := catalog.Snapshot{
		Settings:   m.currentSettings(),
		Stratagems: make([]catalog.Stratagem, 0, len(m.snap.Stratagems)+1),
	}
	if !m.isCreatingNew && catalog.IsSystemStratagem(m.selectedID) {
		out.Stratagems = append(out.Stratagems, m.snap.Stratagems...)
		return out
	}

	cand := m.candidateStratagem()
	replaced := false
	insertedBeforeSys := false
	for _, st := range m.snap.Stratagems {
		if m.isCreatingNew && catalog.IsSystemStratagem(st.ID) && !insertedBeforeSys {
			out.Stratagems = append(out.Stratagems, cand)
			insertedBeforeSys = true
		}
		if !m.isCreatingNew && st.ID == cand.ID {
			out.Stratagems = append(out.Stratagems, cand)
			replaced = true
		} else {
			out.Stratagems = append(out.Stratagems, st)
		}
	}
	if (m.isCreatingNew && !insertedBeforeSys) || (!m.isCreatingNew && !replaced) {
		out.Stratagems = append(out.Stratagems, cand)
	}
	return out
}

func (m *Manager) checkLiveConflict() error {
	if !m.draftEnabled || len(m.draftCode) == 0 {
		return nil
	}
	if !m.isCreatingNew && catalog.IsSystemStratagem(m.selectedID) {
		return nil
	}
	candSnap := m.candidateSnapshot()
	for i := range candSnap.Stratagems {
		if candSnap.Stratagems[i].ID == m.draftID {
			for j := range candSnap.Stratagems[i].Steps {
				step := &candSnap.Stratagems[i].Steps[j]
				if step.Kind == catalog.StepSystemAction {
					if !catalog.SystemAction(strings.TrimSpace(step.Target)).Valid() {
						step.Target = string(catalog.SysWinLeft)
					}
					continue
				}
				if step.Kind != catalog.StepCloseActiveWindow &&
					step.Kind != catalog.StepLaunchHelldivers &&
					strings.TrimSpace(step.Target) == "" {
					step.Target = "placeholder"
				}
			}
		}
	}
	err := catalog.Validate(candSnap)
	if err == nil {
		return nil
	}
	var ce *catalog.ConflictError
	if errors.As(err, &ce) {
		return err
	}
	return nil
}

func (m *Manager) isDirty() bool {
	if !m.isCreatingNew && catalog.IsSystemStratagem(m.selectedID) {
		return false
	}
	steps := m.normalizedDraftSteps()
	if m.isCreatingNew {
		if len(m.draftCode) > 0 || len(steps) > 1 {
			return true
		}
		return len(steps) == 1 && (steps[0].Kind != catalog.StepLaunchApp || steps[0].Target != "")
	}
	for _, st := range m.snap.Stratagems {
		if st.ID == m.selectedID {
			if strings.TrimSpace(m.draftName) != st.Name ||
				m.draftCategory != st.Category ||
				m.draftIcon != st.Icon ||
				m.draftEnabled != st.Enabled ||
				m.draftDelivery != st.Delivery ||
				!slices.Equal(m.draftCode, st.Code) ||
				!stepsEqual(steps, st.Steps) {
				return true
			}
			return false
		}
	}
	return false
}

func (m *Manager) targetPlaceholder(kind catalog.StepKind, tr i18n.Strings) string {
	isWin := runtime.GOOS == "windows"
	switch kind {
	case catalog.StepOpenTarget:
		return tr.TargetOpen
	case catalog.StepShellCommand:
		if isWin {
			return tr.TargetShellWin
		}
		return tr.TargetShellOther
	case catalog.StepRunShortcut:
		if isWin {
			return tr.TargetShortcutWin
		}
		return tr.TargetShortcutOther
	default:
		if isWin {
			return tr.TargetAppWin
		}
		return tr.TargetAppOther
	}
}

func (m *Manager) filteredApps() []AppOption {
	q := strings.ToLower(strings.TrimSpace(m.appQuery))
	if q == "" {
		return m.apps
	}
	out := make([]AppOption, 0, 16)
	for _, a := range m.apps {
		if strings.Contains(strings.ToLower(a.Name), q) || strings.Contains(strings.ToLower(a.ID), q) {
			out = append(out, a)
		}
	}
	return out
}

func (m *Manager) currentSettings() catalog.Settings {
	mode := catalog.ModeDoubleHoldCtrl
	switch m.triggerIdx {
	case 1:
		mode = catalog.ModeHoldLeftCtrl
	case 2:
		mode = catalog.ModeRightCtrlOrAlt
	}
	theme := catalog.ThemeClassic
	if m.themeIdx == 1 {
		theme = catalog.ThemeTactical
	}
	lang := catalog.LangZH
	if m.langIdx == 1 {
		lang = catalog.LangEN
	}
	return catalog.Settings{
		TriggerMode:       mode,
		Muted:             !m.soundOn,
		VisorCurved:       m.visorCurved,
		HUDTheme:          theme,
		Language:          lang,
		HelldiversPath:    m.snap.Settings.HelldiversPath,
		ShipName:          strings.TrimSpace(m.shipName),
		Onboarded:         m.snap.Settings.Onboarded,
		DisableGameBypass: !m.gameBypassOn,
	}
}

func (m *Manager) reloadSnapshot(snap catalog.Snapshot, resetDraft bool) {
	prevPath := m.snap.Settings.HelldiversPath
	wasDirty := !resetDraft && m.isDirty()
	m.snap = snap

	if prevPath != snap.Settings.HelldiversPath {
		m.hdChecked = false
	}

	switch snap.Settings.TriggerMode {
	case catalog.ModeHoldLeftCtrl:
		m.triggerIdx = 1
	case catalog.ModeRightCtrlOrAlt:
		m.triggerIdx = 2
	default:
		m.triggerIdx = 0
	}
	if snap.Settings.Language == catalog.LangEN {
		m.langIdx = 1
	} else {
		m.langIdx = 0
	}
	if snap.Settings.HUDTheme == catalog.ThemeTactical {
		m.themeIdx = 1
	} else {
		m.themeIdx = 0
	}
	m.soundOn = !snap.Settings.Muted
	m.visorCurved = snap.Settings.VisorCurved
	m.gameBypassOn = !snap.Settings.DisableGameBypass
	if resetDraft || strings.TrimSpace(m.shipName) != snap.Settings.ShipName {
		m.shipName = snap.Settings.ShipName
	}
	if m.hooks.GetOpenAtLogin != nil {
		m.openAtLogin = m.hooks.GetOpenAtLogin()
	}

	if !resetDraft && (m.isCreatingNew || m.selectedID != "") {
		if !m.isCreatingNew {
			for _, st := range snap.Stratagems {
				if st.ID == m.selectedID {
					if !wasDirty || catalog.IsSystemStratagem(st.ID) {
						m.draftName = st.Name
						m.draftIcon = st.Icon
						m.draftCategory = st.Category
						m.draftCode = slices.Clone(st.Code)
					}
					return
				}
			}
		} else {
			return
		}
	}

	if len(snap.Stratagems) > 0 {
		m.selectStratagem(snap.Stratagems[0].ID)
	} else {
		m.startNewDraft()
	}
}

func (m *Manager) selectStratagem(id string) {
	for _, st := range m.snap.Stratagems {
		if st.ID == id {
			m.isCreatingNew = false
			m.recording = false
			m.iconPickerOpen = false
			m.selectedID = st.ID
			m.draftID = st.ID
			m.draftName = st.Name
			m.draftSubtitle = st.Subtitle
			m.draftCategory = st.Category
			m.draftIcon = st.Icon
			m.draftEnabled = st.Enabled
			m.draftDelivery = st.Delivery
			if st.Delivery == catalog.DeliveryBeaconClick {
				m.deliveryIdx = 1
			} else {
				m.deliveryIdx = 0
			}
			m.draftCode = slices.Clone(st.Code)
			if len(st.Steps) > 0 {
				m.draftSteps = make([]catalog.Step, len(st.Steps))
				for i, step := range st.Steps {
					m.draftSteps[i] = catalog.Step{
						Kind:   step.Kind,
						Target: step.Target,
						Args:   slices.Clone(step.Args),
					}
				}
			} else {
				m.draftSteps = []catalog.Step{{Kind: catalog.StepLaunchApp}}
			}
			m.selectActiveStep(0)
			m.errMsg = ""
			m.okMsg = ""
			return
		}
	}
}

func (m *Manager) startNewDraft() {
	tr := i18n.For(m.currentLang())
	m.isCreatingNew = true
	m.recording = false
	m.iconPickerOpen = false
	m.iconSearch = ""
	m.iconCatFilter = 0
	m.selectedID = ""
	m.draftID = fmt.Sprintf("custom-%d", time.Now().UnixNano())
	m.draftName = tr.DefaultNewName
	m.draftSubtitle = ""
	m.draftCategory = catalog.CategoryRed
	m.draftIcon = "eagle-500kg.svg"
	m.draftEnabled = true
	m.draftDelivery = catalog.DeliveryImmediate
	m.deliveryIdx = 0
	m.draftCode = nil
	m.draftSteps = []catalog.Step{{Kind: catalog.StepLaunchApp}}
	m.selectActiveStep(0)
	m.errMsg = ""
	m.okMsg = ""
}

func (m *Manager) revertDraft() {
	m.recording = false
	m.errMsg = ""
	m.okMsg = ""
	if m.isCreatingNew {
		m.startNewDraft()
	} else {
		m.selectStratagem(m.selectedID)
	}
}

func (m *Manager) commitGlobalSettings() {
	next := m.currentSettings()
	if err := m.cat.Update(func(s *catalog.Snapshot) error {
		s.Settings = next
		return nil
	}); err != nil {
		m.errMsg = i18n.FormatError(err, next.Language)
	}
}

func (m *Manager) saveDraft() {
	if !m.isCreatingNew && catalog.IsSystemStratagem(m.selectedID) {
		return
	}
	m.recording = false
	lang := m.currentLang()
	tr := i18n.For(lang)

	candidate := catalog.Stratagem{
		ID:       m.draftID,
		Name:     strings.TrimSpace(m.draftName),
		Subtitle: strings.TrimSpace(m.draftSubtitle),
		Category: m.draftCategory,
		Icon:     m.draftIcon,
		Enabled:  m.draftEnabled,
		Code:     slices.Clone(m.draftCode),
		Delivery: m.draftDelivery,
		Steps:    m.normalizedDraftSteps(),
	}

	creating := m.isCreatingNew
	err := m.cat.Update(func(s *catalog.Snapshot) error {
		if creating {
			// Insert custom stratagems right above the pinned system stratagem at the bottom!
			sysIdx := -1
			for i := range s.Stratagems {
				if catalog.IsSystemStratagem(s.Stratagems[i].ID) {
					sysIdx = i
					break
				}
			}
			if sysIdx >= 0 {
				s.Stratagems = slices.Insert(s.Stratagems, sysIdx, candidate)
			} else {
				s.Stratagems = append(s.Stratagems, candidate)
			}
			return nil
		}
		for i := range s.Stratagems {
			if s.Stratagems[i].ID == candidate.ID {
				s.Stratagems[i] = candidate
				return nil
			}
		}
		s.Stratagems = append(s.Stratagems, candidate)
		return nil
	})
	if err != nil {
		m.errMsg = i18n.FormatError(err, lang)
		m.okMsg = ""
		return
	}

	m.isCreatingNew = false
	m.selectedID = candidate.ID
	m.errMsg = ""
	m.okMsg = tr.SavedToast
}

func (m *Manager) deleteSelectedStratagem() {
	targetID := m.selectedID
	if targetID == "" || catalog.IsSystemStratagem(targetID) {
		return
	}
	lang := m.currentLang()
	tr := i18n.For(lang)
	err := m.cat.Update(func(s *catalog.Snapshot) error {
		next := make([]catalog.Stratagem, 0, len(s.Stratagems))
		for _, st := range s.Stratagems {
			if st.ID != targetID {
				next = append(next, st)
			}
		}
		s.Stratagems = next
		return nil
	})
	if err != nil {
		m.errMsg = i18n.FormatError(err, lang)
		return
	}
	if len(m.snap.Stratagems) > 0 {
		m.selectStratagem(m.snap.Stratagems[0].ID)
	} else {
		m.startNewDraft()
	}
	m.okMsg = tr.DeletedToast
}

func (m *Manager) resetToDefaults() {
	lang := m.currentLang()
	tr := i18n.For(lang)
	err := m.cat.Update(func(s *catalog.Snapshot) error {
		s.Stratagems = catalog.DefaultStratagemsFor(s.Settings.Language, s.Settings.HelldiversPath)
		return nil
	})
	if err != nil {
		m.errMsg = i18n.FormatError(err, lang)
		return
	}
	m.reloadSnapshot(m.cat.Snapshot(), true)
	m.okMsg = tr.ResetToast
}

func (m *Manager) userStratagemCount() int {
	n := 0
	for _, st := range m.snap.Stratagems {
		if !catalog.IsSystemStratagem(st.ID) {
			n++
		}
	}
	return n
}
