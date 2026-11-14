package main

import (
	"context"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"sync"
	"syscall"
	"time"

	"github.com/egoist/mygo"
	_ "github.com/egoist/mygo/plugins/automation"

	"hellauncher/internal/arsenal"
	"hellauncher/internal/catalog"
	"hellauncher/internal/engine"
	"hellauncher/internal/hud"
	"hellauncher/internal/sfx"
	"hellauncher/internal/sys"
	"hellauncher/internal/trayicon"
)

const (
	prodBundleID = "com.superearth.hellauncher"
	maxLogBytes  = 2 << 20 // 2 MiB rotating log cap
)

func main() {
	isE2E := os.Getenv("HELLAUNCHER_E2E") == "1"
	isDev := os.Getenv("MYGO_ENV") == "development"

	if !isE2E && !mygo.App.RequestSingleInstanceLock() {
		return
	}

	mygo.App.SetActivationPolicy(mygo.ActivationPolicyAccessory)
	mygo.App.OnWindowAllClosed(func() {})

	var (
		cleanupOnce sync.Once
		stopInput   func()
		spk         sfx.Speaker
		overlay     *hud.Overlay
		arsenalMgr  *arsenal.Manager
		eng         *engine.Engine
	)

	shutdown := func() {
		cleanupOnce.Do(func() {
			if stopInput != nil {
				stopInput()
			}
			if spk != nil {
				spk.Close()
			}
			if arsenalMgr != nil {
				arsenalMgr.Close()
			}
			if overlay != nil {
				overlay.Close()
			}
		})
	}

	mygo.App.OnBeforeQuit(func(*mygo.QuitEvent) {
		shutdown()
	})

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigCh
		shutdown()
		mygo.App.Quit()
	}()
	mygo.App.OnSecondInstance(func([]string, string) {
		if eng != nil {
			eng.Preview(3 * time.Second)
		}
	})

	mygo.App.WhenReady(func() {
		logDir := setupLogger(isDev)
		dataDir := resolveDataDir(isDev)
		slog.Info("hellauncher starting", "dev", isDev, "os", runtime.GOOS, "dataDir", dataDir, "logDir", logDir)

		if isDev && sys.HasConflictingBundleRunning(prodBundleID) {
			slog.Warn("detected production HelLauncher bundle running alongside dev session; quit prod to avoid duplicate Ctrl hooks")
		}

		cat, err := catalog.Open(dataDir)
		if err != nil {
			slog.Error("catalog open failed", "err", err)
			mygo.App.Quit()
			return
		}

		if isE2E {
			spk = sfx.NewRecordSpeaker()
			rig := sys.NewFakeRig()
			if os.Getenv("HELLAUNCHER_E2E_DEPLOY") == "1" {
				rig.ExecFn = func(ctx context.Context, _ catalog.Step, _ *sys.Point) error {
					select {
					case <-ctx.Done():
						return ctx.Err()
					case <-time.After(1400 * time.Millisecond):
						return nil
					}
				}
			}
			eng = engine.New(rig, spk, cat.Snapshot())
			stopInput = rig.StartInput(eng.HandleInput, eng.SetTrusted)
			if os.Getenv("HELLAUNCHER_E2E_DEPLOY") == "1" {
				go func() {
					time.Sleep(750 * time.Millisecond)
					t0 := time.Now()
					rig.Emit(sys.InputEvent{At: t0, Kind: sys.InputModifiers, Mods: sys.Mods{LeftCtrl: true}})
					rig.Emit(sys.InputEvent{At: t0.Add(60 * time.Millisecond), Kind: sys.InputModifiers, Mods: sys.Mods{}})
					rig.Emit(sys.InputEvent{At: t0.Add(120 * time.Millisecond), Kind: sys.InputModifiers, Mods: sys.Mods{LeftCtrl: true}})
					rig.Emit(sys.InputEvent{At: t0.Add(150 * time.Millisecond), Kind: sys.InputKeyDown, KeyCode: 13, Role: sys.KeyDirUp})
					rig.Emit(sys.InputEvent{At: t0.Add(160 * time.Millisecond), Kind: sys.InputKeyUp, KeyCode: 13})
					rig.Emit(sys.InputEvent{At: t0.Add(180 * time.Millisecond), Kind: sys.InputKeyDown, KeyCode: 1, Role: sys.KeyDirDown})
					rig.Emit(sys.InputEvent{At: t0.Add(190 * time.Millisecond), Kind: sys.InputKeyUp, KeyCode: 1})
					rig.Emit(sys.InputEvent{At: t0.Add(210 * time.Millisecond), Kind: sys.InputKeyDown, KeyCode: 13, Role: sys.KeyDirUp})
					rig.Emit(sys.InputEvent{At: t0.Add(220 * time.Millisecond), Kind: sys.InputKeyUp, KeyCode: 13})
					rig.Emit(sys.InputEvent{At: t0.Add(240 * time.Millisecond), Kind: sys.InputModifiers, Mods: sys.Mods{}})
				}()
			}
		} else {
			spk = sfx.NewAudioSpeaker()
			rig := sys.NewPlatformRig()
			eng = engine.New(rig, spk, cat.Snapshot())
			stopInput = rig.StartInput(eng.HandleInput, eng.SetTrusted)
		}

		overlay = hud.NewOverlay()
		arsenalMgr = arsenal.New(cat, listAppOptions)

		trayMenu := setupTray(isDev, logDir, cat, eng, arsenalMgr)

		cat.Subscribe(func(next catalog.Snapshot) {
			eng.SetLoadout(next)
			syncTraySettings(trayMenu, next.Settings)
		})

		eng.Subscribe(func(snap engine.Snapshot) {
			overlay.Apply(snap)
			syncTrayStatus(trayMenu, isDev, snap.Trusted)
		})

		overlay.Apply(eng.Snapshot())
		eng.Preview(2400 * time.Millisecond)

		if os.Getenv("HELLAUNCHER_OPEN_ARSENAL") == "1" {
			arsenalMgr.Open()
		}

		if runtime.GOOS == "darwin" && !isE2E && !sys.CheckTrusted() && os.Getenv("HELLAUNCHER_NO_AUTO_GUIDE") != "1" {
			openPermissionGuide(eng)
		}
	})

	if err := mygo.App.Run(); err != nil {
		slog.Error("mygo app loop exited with error", "err", err)
		os.Exit(1)
	}
}

func resolveDataDir(isDev bool) string {
	if custom := os.Getenv("HELLAUNCHER_DATA_DIR"); custom != "" {
		return custom
	}
	base, err := mygo.App.Path(mygo.PathUserData)
	if err != nil || base == "" {
		base = filepath.Join(os.TempDir(), "hellauncher-data")
	}
	if isDev {
		return filepath.Join(base, "dev")
	}
	return base
}

func setupLogger(isDev bool) string {
	logDir, err := mygo.App.Path(mygo.PathLogs)
	if err != nil || logDir == "" {
		logDir = filepath.Join(os.TempDir(), "hellauncher-logs")
	}
	_ = os.MkdirAll(logDir, 0o755)
	logPath := filepath.Join(logDir, "hellauncher.log")

	if info, err := os.Stat(logPath); err == nil && info.Size() > maxLogBytes {
		_ = os.Rename(logPath, logPath+".1")
	}

	var writers []io.Writer
	if f, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644); err == nil {
		writers = append(writers, f)
	}
	if isDev || len(writers) == 0 {
		writers = append(writers, os.Stderr)
	}

	handler := slog.NewTextHandler(io.MultiWriter(writers...), &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})
	slog.SetDefault(slog.New(handler))
	return logDir
}

func listAppOptions() []arsenal.AppOption {
	found := sys.InstalledApps()
	out := make([]arsenal.AppOption, len(found))
	for i, a := range found {
		out[i] = arsenal.AppOption{
			ID:   a.ID,
			Name: a.Name,
			Path: a.Path,
		}
	}
	return out
}

func openPermissionGuide(eng *engine.Engine) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
		defer cancel()
		granted, err := sys.GuidePermission(ctx)
		if err != nil {
			slog.Warn("accessibility permission guide error", "err", err)
			return
		}
		if granted {
			eng.SetTrusted(true)
		}
	}()
}

func setupTray(
	isDev bool,
	logDir string,
	cat *catalog.Catalog,
	eng *engine.Engine,
	arsenalMgr *arsenal.Manager,
) *mygo.Menu {
	initSnap := cat.Snapshot()
	trusted := eng.Snapshot().Trusted

	setMode := func(m catalog.TriggerMode) {
		if err := cat.Update(func(s *catalog.Snapshot) error {
			s.Settings.TriggerMode = m
			return nil
		}); err != nil {
			slog.Warn("update trigger mode failed", "err", err)
		}
	}

	items := []*mygo.MenuItem{
		{ID: "status", Label: formatStatusLabel(isDev, trusted), Disabled: true},
		{
			ID:    "open-arsenal",
			Label: "管理战略配备 (军械库)…",
			Click: func(*mygo.MenuItem, *mygo.Window) {
				arsenalMgr.Open()
			},
		},
		{
			ID:    "preview-hud",
			Label: "预览战术面板 (3秒)",
			Click: func(*mygo.MenuItem, *mygo.Window) {
				eng.Preview(3 * time.Second)
			},
		},
		mygo.Separator(),
		{
			Label: "唤出热键模式",
			Submenu: []*mygo.MenuItem{
				{
					ID:      "mode-double",
					Label:   "双击并按住 Ctrl（推荐，防日常快捷键冲突）",
					Type:    mygo.MenuItemRadio,
					Checked: initSnap.Settings.TriggerMode == catalog.ModeDoubleHoldCtrl,
					Click:   func(*mygo.MenuItem, *mygo.Window) { setMode(catalog.ModeDoubleHoldCtrl) },
				},
				{
					ID:      "mode-left",
					Label:   "单按左 Ctrl（游戏原生手感）",
					Type:    mygo.MenuItemRadio,
					Checked: initSnap.Settings.TriggerMode == catalog.ModeHoldLeftCtrl,
					Click:   func(*mygo.MenuItem, *mygo.Window) { setMode(catalog.ModeHoldLeftCtrl) },
				},
				{
					ID:      "mode-right",
					Label:   "右 Ctrl 或 Ctrl+Alt",
					Type:    mygo.MenuItemRadio,
					Checked: initSnap.Settings.TriggerMode == catalog.ModeRightCtrlOrAlt,
					Click:   func(*mygo.MenuItem, *mygo.Window) { setMode(catalog.ModeRightCtrlOrAlt) },
				},
			},
		},
		{
			ID:      "toggle-sound",
			Label:   "键盘按键音效",
			Type:    mygo.MenuItemCheckbox,
			Checked: !initSnap.Settings.Muted,
			Click: func(item *mygo.MenuItem, _ *mygo.Window) {
				muted := !item.Checked
				_ = cat.Update(func(s *catalog.Snapshot) error {
					s.Settings.Muted = muted
					return nil
				})
			},
		},
		{
			ID:      "toggle-visor",
			Label:   "3D 头盔弧面透视",
			Type:    mygo.MenuItemCheckbox,
			Checked: initSnap.Settings.VisorCurved,
			Click: func(item *mygo.MenuItem, _ *mygo.Window) {
				curved := item.Checked
				_ = cat.Update(func(s *catalog.Snapshot) error {
					s.Settings.VisorCurved = curved
					return nil
				})
			},
		},
		mygo.Separator(),
		{
			ID:    "open-logs",
			Label: "打开运行日志目录",
			Click: func(*mygo.MenuItem, *mygo.Window) {
				if err := mygo.Shell.OpenPath(logDir); err != nil {
					slog.Warn("open log dir failed", "err", err)
				}
			},
		},
	}

	if runtime.GOOS == "darwin" {
		items = append(items, &mygo.MenuItem{
			ID:    "grant-ax",
			Label: "授予辅助功能权限…",
			Click: func(*mygo.MenuItem, *mygo.Window) {
				openPermissionGuide(eng)
			},
		})
	}

	items = append(items,
		mygo.Separator(),
		&mygo.MenuItem{Label: "退出 HelLauncher", Role: mygo.RoleQuit},
	)

	menu := mygo.NewMenu(items)
	iconBytes, isTemplate := trayicon.Icon()
	tooltip := "HelLauncher — 战略配备桌面快捷指令"
	if isDev {
		tooltip = "[DEV] " + tooltip
	}
	if _, err := mygo.NewTray(mygo.TrayOptions{
		ID:             "hellauncher-main",
		Icon:           iconBytes,
		IconIsTemplate: isTemplate,
		ToolTip:        tooltip,
		Menu:           menu,
	}); err != nil {
		slog.Error("create tray icon failed", "err", err)
	}
	return menu
}

func formatStatusLabel(isDev bool, trusted bool) string {
	prefix := "✪ HelLauncher"
	if isDev {
		prefix = "✪ HelLauncher [DEV]"
	}
	if runtime.GOOS == "darwin" && !trusted {
		return prefix + " — ⚠️ 等待授予辅助功能权限"
	}
	return prefix + " — 战术终端已就绪"
}

func syncTrayStatus(menu *mygo.Menu, isDev bool, trusted bool) {
	if menu == nil {
		return
	}
	mygo.RunOnMain(func() {
		if item := menu.ItemByID("status"); item != nil {
			item.SetLabel(formatStatusLabel(isDev, trusted))
		}
	})
}

func syncTraySettings(menu *mygo.Menu, st catalog.Settings) {
	if menu == nil {
		return
	}
	mygo.RunOnMain(func() {
		if m := menu.ItemByID("mode-double"); m != nil && st.TriggerMode == catalog.ModeDoubleHoldCtrl {
			m.SetChecked(true)
		}
		if m := menu.ItemByID("mode-left"); m != nil && st.TriggerMode == catalog.ModeHoldLeftCtrl {
			m.SetChecked(true)
		}
		if m := menu.ItemByID("mode-right"); m != nil && st.TriggerMode == catalog.ModeRightCtrlOrAlt {
			m.SetChecked(true)
		}
		if snd := menu.ItemByID("toggle-sound"); snd != nil {
			snd.SetChecked(!st.Muted)
		}
		if vis := menu.ItemByID("toggle-visor"); vis != nil {
			vis.SetChecked(st.VisorCurved)
		}
	})
}
