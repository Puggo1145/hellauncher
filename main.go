package main

import (
	"context"
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"sync"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/plugins/permissionflow"

	"helldriver/internal/actions"
	tacticalaudio "helldriver/internal/audio"
	"helldriver/internal/input"
	"helldriver/internal/trayicon"
)

// Settings stores user preferences persisted in PathUserData/settings.json.
type Settings struct {
	TriggerMode input.TriggerMode `json:"triggerMode"`
	Muted       bool              `json:"muted"`
	VisorCurved bool              `json:"visorCurved"`
}

func defaultSettings() Settings {
	return Settings{
		TriggerMode: input.ModeDoubleHoldCtrl,
		Muted:       false,
		VisorCurved: true,
	}
}

// HudSnapshot represents the full tactical HUD state pushed to the overlay WebView.
type HudSnapshot struct {
	Phase       string              `json:"phase"`       // "IDLE" | "OPEN" | "DEPLOYING" | "DEPLOYED" | "PREVIEW"
	Typed       []string            `json:"typed"`       // e.g. ["U", "R", "D"]
	MatchedID   string              `json:"matchedId"`   // ID of matched Stratagem
	ErrorSeq    int                 `json:"errorSeq"`    // Increments on wrong key to trigger red shake
	StepSeq     int                 `json:"stepSeq"`     // Increments on valid directional input
	TriggerMode string              `json:"triggerMode"` // Current trigger mode ID
	VisorCurved bool                `json:"visorCurved"` // 3D helmet visor perspective
	Trusted     bool                `json:"trusted"`     // macOS Accessibility granted
	Platform    string              `json:"platform"`    // "darwin" | "windows" | "linux"
	Stratagems  []actions.Stratagem `json:"stratagems"`
}

// HudStateChanged streams HUD state transitions from Go to the transparent WebView panel.
var HudStateChanged = mygo.NewEvent[HudSnapshot]("hudState")

// HudController coordinates the low-level keyboard hook, audio mixer, OS actions, and overlay panel.
type HudController struct {
	mu         sync.Mutex
	settings   Settings
	stratagems []actions.Stratagem
	phase      string
	typed      []string
	matchedID  string
	errorSeq   int
	stepSeq    int
	trusted    bool
	timerToken uint64
	quitting   bool
	panelX     int
	panelY     int
	panelShown bool

	audio    *tacticalaudio.Engine
	executor *actions.Executor
	hook     *input.Hook
	panel    *mygo.Window
	trayMenu *mygo.Menu
}

// HudService exposes read/control methods to the TypeScript frontend via `mygo generate`.
type HudService struct {
	ctrl *HudController
}

// Snapshot returns the current HUD state when the WebView initializes.
func (s *HudService) Snapshot() HudSnapshot {
	return s.ctrl.snapshot()
}

// TriggerPreview opens the HUD for 3 seconds for visual inspection.
func (s *HudService) TriggerPreview() {
	s.ctrl.showPreview(3 * time.Second)
}

func newController() *HudController {
	return &HudController{
		settings:   defaultSettings(),
		stratagems: actions.DefaultStratagems(),
		phase:      "IDLE",
		trusted:    input.CheckTrusted(),
		audio:      tacticalaudio.New(),
		executor:   actions.NewExecutor(),
	}
}

func (c *HudController) loadSettings() {
	dir, err := mygo.App.Path(mygo.PathUserData)
	if err != nil {
		return
	}
	raw, err := os.ReadFile(filepath.Join(dir, "settings.json"))
	if err != nil {
		return
	}
	var s Settings
	if json.Unmarshal(raw, &s) == nil {
		if s.TriggerMode == "" {
			s.TriggerMode = input.ModeDoubleHoldCtrl
		}
		c.mu.Lock()
		c.settings = s
		c.mu.Unlock()
		c.audio.SetMuted(s.Muted)
	}
}

func (c *HudController) saveSettings() {
	c.mu.Lock()
	s := c.settings
	c.mu.Unlock()

	dir, err := mygo.App.Path(mygo.PathUserData)
	if err != nil {
		return
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err == nil {
		_ = os.WriteFile(filepath.Join(dir, "settings.json"), data, 0o644)
	}
}

func (c *HudController) snapshotLocked() HudSnapshot {
	return HudSnapshot{
		Phase:       c.phase,
		Typed:       slices.Clone(c.typed),
		MatchedID:   c.matchedID,
		ErrorSeq:    c.errorSeq,
		StepSeq:     c.stepSeq,
		TriggerMode: string(c.settings.TriggerMode),
		VisorCurved: c.settings.VisorCurved,
		Trusted:     c.trusted,
		Platform:    runtime.GOOS,
		Stratagems:  slices.Clone(c.stratagems),
	}
}

func (c *HudController) snapshot() HudSnapshot {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.snapshotLocked()
}

func (c *HudController) broadcast(snap HudSnapshot) {
	if c.panel != nil {
		_ = HudStateChanged.Emit(c.panel, snap)
	}
}

func (c *HudController) movePanelToActiveDisplay() {
	if c.panel == nil {
		return
	}
	pt := mygo.Screen.CursorScreenPoint()
	disp := mygo.Screen.DisplayNearestPoint(pt)
	x := disp.WorkArea.X + 16
	y := disp.WorkArea.Y + 12

	c.mu.Lock()
	needMove := !c.panelShown || c.panelX != x || c.panelY != y
	needShow := !c.panelShown
	c.panelX = x
	c.panelY = y
	c.panelShown = true
	c.mu.Unlock()

	if needMove {
		c.panel.SetPosition(x, y)
	}
	if needShow {
		c.panel.ShowInactive()
	}
}

func (c *HudController) showPreview(dur time.Duration) {
	c.movePanelToActiveDisplay()

	c.mu.Lock()
	if c.phase == "OPEN" || c.phase == "DEPLOYING" {
		c.mu.Unlock()
		return
	}
	c.timerToken++
	tok := c.timerToken
	c.phase = "PREVIEW"
	c.typed = nil
	c.matchedID = ""
	snap := c.snapshotLocked()
	c.mu.Unlock()

	c.broadcast(snap)

	time.AfterFunc(dur, func() {
		c.mu.Lock()
		if c.timerToken != tok || c.phase != "PREVIEW" {
			c.mu.Unlock()
			return
		}
		c.phase = "IDLE"
		s2 := c.snapshotLocked()
		c.mu.Unlock()
		c.broadcast(s2)
	})
}

func (c *HudController) handleInputEvent(ev input.Event) {
	switch ev.Kind {
	case input.EventOpen:
		// Wake native audio queue immediately so the first WASD keystroke has zero lag.
		c.audio.WarmUp()

		c.mu.Lock()
		c.timerToken++
		c.phase = "OPEN"
		c.typed = nil
		c.matchedID = ""
		snap := c.snapshotLocked()
		c.mu.Unlock()

		c.broadcast(snap)
		c.movePanelToActiveDisplay()

	case input.EventClose, input.EventCancel:
		c.mu.Lock()
		if c.phase != "OPEN" {
			c.mu.Unlock()
			return
		}
		c.timerToken++
		c.phase = "IDLE"
		c.typed = nil
		c.matchedID = ""
		snap := c.snapshotLocked()
		c.mu.Unlock()

		c.broadcast(snap)

	case input.EventInvalidKey:
		c.mu.Lock()
		if c.phase != "OPEN" {
			c.mu.Unlock()
			return
		}
		c.typed = nil
		c.errorSeq++
		snap := c.snapshotLocked()
		c.mu.Unlock()

		c.audio.PlayFail()
		c.broadcast(snap)

	case input.EventDirection:
		c.handleDirection(ev.Dir)
	}
}

func (c *HudController) handleDirection(dir string) {
	c.mu.Lock()
	if c.phase != "OPEN" {
		c.mu.Unlock()
		return
	}

	candidate := append(slices.Clone(c.typed), dir)
	var exact *actions.Stratagem
	anyPrefix := false

	for i := range c.stratagems {
		s := &c.stratagems[i]
		if hasPrefix(s.Code, candidate) {
			anyPrefix = true
			if len(s.Code) == len(candidate) {
				exact = s
				break
			}
		}
	}

	if !anyPrefix {
		// Wrong arrow pressed -> authentic HD2 red error flash & sequence reset while keeping HUD open!
		c.typed = nil
		c.errorSeq++
		snap := c.snapshotLocked()
		c.mu.Unlock()

		c.audio.PlayFail()
		c.broadcast(snap)
		return
	}

	if exact != nil {
		// Full sequence matched -> Highlight final arrow & enter fluid DEPLOYING loading state immediately!
		matched := *exact
		c.timerToken++
		tok := c.timerToken
		c.typed = candidate
		c.stepSeq++
		c.phase = "DEPLOYING"
		c.matchedID = matched.ID
		snap := c.snapshotLocked()
		c.mu.Unlock()

		if c.hook != nil {
			c.hook.CompleteCurrentSession()
		}
		c.audio.PlayReady()
		c.broadcast(snap)

		go c.runDeploySequence(tok, matched)
		return
	}

	// Valid prefix step!
	step := len(candidate)
	c.typed = candidate
	c.stepSeq++
	snap := c.snapshotLocked()
	c.mu.Unlock()

	c.audio.PlayBeep(step)
	c.broadcast(snap)
}

// runDeploySequence executes the OS action completely off the main thread while
// the HUD animates smoothly, then confirms deployment and slides out once the
// target window has appeared on screen.
func (c *HudController) runDeploySequence(tok uint64, matched actions.Stratagem) {
	start := time.Now()
	if err := c.executor.Execute(matched); err != nil {
		log.Printf("[actions] Execute(%s) error: %v", matched.ID, err)
	}

	const minDeployingDur = 320 * time.Millisecond
	if elapsed := time.Since(start); elapsed < minDeployingDur {
		time.Sleep(minDeployingDur - elapsed)
	}

	c.mu.Lock()
	if c.timerToken != tok || c.phase != "DEPLOYING" {
		c.mu.Unlock()
		return
	}
	c.phase = "DEPLOYED"
	deployedSnap := c.snapshotLocked()
	c.mu.Unlock()
	c.broadcast(deployedSnap)

	time.Sleep(180 * time.Millisecond)

	c.mu.Lock()
	if c.timerToken != tok || c.phase != "DEPLOYED" {
		c.mu.Unlock()
		return
	}
	c.phase = "IDLE"
	c.typed = nil
	c.matchedID = ""
	idleSnap := c.snapshotLocked()
	c.mu.Unlock()
	c.broadcast(idleSnap)
}

func hasPrefix(code, typed []string) bool {
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

func (c *HudController) setTriggerMode(m input.TriggerMode) {
	c.mu.Lock()
	c.settings.TriggerMode = m
	snap := c.snapshotLocked()
	c.mu.Unlock()

	if c.hook != nil {
		c.hook.SetMode(m)
	}
	c.saveSettings()
	c.broadcast(snap)
}

func (c *HudController) updateTrustStatus(trusted bool) {
	c.mu.Lock()
	c.trusted = trusted
	snap := c.snapshotLocked()
	c.mu.Unlock()

	c.broadcast(snap)
	if c.trayMenu != nil {
		if item := c.trayMenu.ItemByID("status"); item != nil {
			if trusted {
				item.SetLabel("✪ HellDriver 战术面板 — 已就绪")
			} else {
				item.SetLabel("⚠️ 等待授予辅助功能权限")
			}
		}
	}
}

func (c *HudController) openPermissionGuide() {
	if runtime.GOOS != "darwin" {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
		defer cancel()
		st, err := permissionflow.Guide(ctx, permissionflow.Accessibility, permissionflow.GuideOptions{})
		if err != nil {
			log.Printf("[permission] Guide error: %v", err)
			return
		}
		if st == permissionflow.Granted {
			c.updateTrustStatus(true)
		}
	}()
}

func (c *HudController) setupTray() {
	iconBytes, isTemplate := trayicon.Icon()
	statusLabel := "✪ HellDriver 战术面板 — 已就绪"
	if runtime.GOOS == "darwin" && !c.trusted {
		statusLabel = "⚠️ 等待授予辅助功能权限"
	}

	items := []*mygo.MenuItem{
		{ID: "status", Label: statusLabel, Disabled: true},
		{Label: "预览战术面板 (3秒)", Click: func(*mygo.MenuItem, *mygo.Window) {
			c.showPreview(3 * time.Second)
		}},
		mygo.Separator(),
		{Label: "唤出热键模式", Submenu: []*mygo.MenuItem{
			{
				ID:      "mode-double",
				Label:   "双击并按住 Ctrl（推荐，防日常快捷键冲突）",
				Type:    mygo.MenuItemRadio,
				Checked: c.settings.TriggerMode == input.ModeDoubleHoldCtrl,
				Click: func(*mygo.MenuItem, *mygo.Window) {
					c.setTriggerMode(input.ModeDoubleHoldCtrl)
				},
			},
			{
				ID:      "mode-left",
				Label:   "单按左 Ctrl（游戏原生手感）",
				Type:    mygo.MenuItemRadio,
				Checked: c.settings.TriggerMode == input.ModeHoldLeftCtrl,
				Click: func(*mygo.MenuItem, *mygo.Window) {
					c.setTriggerMode(input.ModeHoldLeftCtrl)
				},
			},
			{
				ID:      "mode-right",
				Label:   "右 Ctrl 或 Ctrl+Alt",
				Type:    mygo.MenuItemRadio,
				Checked: c.settings.TriggerMode == input.ModeRightCtrlOrAlt,
				Click: func(*mygo.MenuItem, *mygo.Window) {
					c.setTriggerMode(input.ModeRightCtrlOrAlt)
				},
			},
		}},
		{
			ID:      "toggle-sound",
			Label:   "键盘按键音效",
			Type:    mygo.MenuItemCheckbox,
			Checked: !c.settings.Muted,
			Click: func(item *mygo.MenuItem, _ *mygo.Window) {
				muted := !item.Checked
				c.mu.Lock()
				c.settings.Muted = muted
				c.mu.Unlock()
				c.audio.SetMuted(muted)
				c.saveSettings()
			},
		},
		{
			ID:      "toggle-visor",
			Label:   "3D 头盔弧面透视",
			Type:    mygo.MenuItemCheckbox,
			Checked: c.settings.VisorCurved,
			Click: func(item *mygo.MenuItem, _ *mygo.Window) {
				c.mu.Lock()
				c.settings.VisorCurved = item.Checked
				snap := c.snapshotLocked()
				c.mu.Unlock()
				c.saveSettings()
				c.broadcast(snap)
			},
		},
	}

	if runtime.GOOS == "darwin" {
		items = append(items,
			mygo.Separator(),
			&mygo.MenuItem{
				ID:    "grant-ax",
				Label: "授予辅助功能权限…",
				Click: func(*mygo.MenuItem, *mygo.Window) {
					c.openPermissionGuide()
				},
			},
		)
	}

	items = append(items,
		mygo.Separator(),
		&mygo.MenuItem{Label: "退出 HellDriver", Role: mygo.RoleQuit},
	)

	c.trayMenu = mygo.NewMenu(items)
	if _, err := mygo.NewTray(mygo.TrayOptions{
		ID:             "helldriver-main",
		Icon:           iconBytes,
		IconIsTemplate: isTemplate,
		ToolTip:        "HellDriver — 战略配备桌面快捷指令",
		Menu:           c.trayMenu,
	}); err != nil {
		log.Printf("[tray] failed creating tray icon: %v", err)
	}
}

func main() {
	if !mygo.App.RequestSingleInstanceLock() {
		return
	}

	ctrl := newController()
	mygo.BindAs("Hud", &HudService{ctrl: ctrl})

	mygo.App.SetActivationPolicy(mygo.ActivationPolicyAccessory)
	mygo.App.OnWindowAllClosed(func() {})
	mygo.App.OnBeforeQuit(func(*mygo.QuitEvent) {
		ctrl.mu.Lock()
		ctrl.quitting = true
		ctrl.mu.Unlock()
		if ctrl.hook != nil {
			ctrl.hook.Stop()
		}
		if ctrl.audio != nil {
			ctrl.audio.Close()
		}
	})
	mygo.App.OnSecondInstance(func([]string, string) {
		ctrl.showPreview(3 * time.Second)
	})

	mygo.App.WhenReady(func() {
		ctrl.loadSettings()
		ctrl.audio.StartKeepWarm()
		ctrl.executor.RefreshApps()

		disp := mygo.Screen.DisplayNearestPoint(mygo.Screen.CursorScreenPoint())
		initX := disp.WorkArea.X + 16
		initY := disp.WorkArea.Y + 12
		ctrl.panelX = initX
		ctrl.panelY = initY

		ctrl.panel = mygo.NewWindow(mygo.WindowOptions{
			Title:         "HellDriver Tactical HUD",
			URL:           "/",
			X:             initX,
			Y:             initY,
			Width:         430,
			Height:        500,
			Hidden:        true,
			Frameless:     true,
			Transparent:   true,
			AlwaysOnTop:   true,
			Panel:         true,
			DisableResize: true,
			DisableShadow: true,
			SkipTaskbar:   true,
		})
		ctrl.panel.SetIgnoreMouseEvents(true)

		ctrl.panel.OnClose(func(e *mygo.CloseEvent) {
			ctrl.mu.Lock()
			q := ctrl.quitting
			ctrl.mu.Unlock()
			if !q {
				e.PreventDefault()
			}
		})

		ctrl.panel.OnReadyToShow(func() {
			ctrl.mu.Lock()
			ctrl.panelShown = true
			ctrl.mu.Unlock()
			ctrl.panel.ShowInactive()
			// Show a brief 2.4s boot preview so the user immediately sees the floating HUD is online
			ctrl.showPreview(2400 * time.Millisecond)
		})

		ctrl.setupTray()

		ctrl.hook = input.Start(
			ctrl.settings.TriggerMode,
			ctrl.handleInputEvent,
			ctrl.updateTrustStatus,
		)

		// On macOS, if Accessibility permission is not yet granted, launch the drag-and-drop guide automatically
		if runtime.GOOS == "darwin" && !ctrl.trusted && os.Getenv("HELLDRIVER_NO_AUTO_GUIDE") != "1" {
			ctrl.openPermissionGuide()
		}
	})

	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}
