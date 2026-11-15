package main

import (
	"context"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/egoist/mygo"

	"hellauncher/internal/arsenal"
	"hellauncher/internal/catalog"
	"hellauncher/internal/engine"
	"hellauncher/internal/hud"
	"hellauncher/internal/i18n"
	"hellauncher/internal/onboarding"
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

	mygo.Theme.SetSource(mygo.ThemeDark)
	mygo.App.SetActivationPolicy(mygo.ActivationPolicyAccessory)
	mygo.App.OnWindowAllClosed(func() {})

	var (
		cleanupOnce sync.Once
		stopInput   func()
		spk         sfx.Speaker
		overlay     *hud.Overlay
		arsenalMgr  *arsenal.Manager
		onboardMgr  *onboarding.Manager
		eng         *engine.Engine
	)

	shutdown := func() {
		cleanupOnce.Do(func() {
			if stopInput != nil {
				stopInput()
			}
			if onboardMgr != nil {
				onboardMgr.Close()
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

		var activeRig sys.Rig
		if isE2E {
			spk = sfx.NewRecordSpeaker()
			rig := sys.NewFakeRig()
			activeRig = rig
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
			eng.SetHelldiversReady(sys.DetectHelldivers(cat.Snapshot().Settings.HelldiversPath).Found)
			stopInput = rig.StartInput(eng.HandleInput, eng.SetTrusted)
			if os.Getenv("HELLAUNCHER_E2E_DEPLOY") == "1" {
				go func() {
					time.Sleep(1100 * time.Millisecond)
					t0 := time.Now()
					rig.Emit(sys.InputEvent{At: t0, Kind: sys.InputModifiers, Mods: sys.Mods{LeftCtrl: true}})
					rig.Emit(sys.InputEvent{At: t0.Add(60 * time.Millisecond), Kind: sys.InputModifiers, Mods: sys.Mods{}})
					rig.Emit(sys.InputEvent{At: t0.Add(120 * time.Millisecond), Kind: sys.InputModifiers, Mods: sys.Mods{LeftCtrl: true}})
					// Call In File Resupply canonical sequence: ↓↓↑→
					rig.Emit(sys.InputEvent{At: t0.Add(145 * time.Millisecond), Kind: sys.InputKeyDown, KeyCode: 1, Role: sys.KeyDirDown})
					rig.Emit(sys.InputEvent{At: t0.Add(155 * time.Millisecond), Kind: sys.InputKeyUp, KeyCode: 1})
					rig.Emit(sys.InputEvent{At: t0.Add(175 * time.Millisecond), Kind: sys.InputKeyDown, KeyCode: 1, Role: sys.KeyDirDown})
					rig.Emit(sys.InputEvent{At: t0.Add(185 * time.Millisecond), Kind: sys.InputKeyUp, KeyCode: 1})
					rig.Emit(sys.InputEvent{At: t0.Add(205 * time.Millisecond), Kind: sys.InputKeyDown, KeyCode: 13, Role: sys.KeyDirUp})
					rig.Emit(sys.InputEvent{At: t0.Add(215 * time.Millisecond), Kind: sys.InputKeyUp, KeyCode: 13})
					rig.Emit(sys.InputEvent{At: t0.Add(235 * time.Millisecond), Kind: sys.InputKeyDown, KeyCode: 2, Role: sys.KeyDirRight})
					rig.Emit(sys.InputEvent{At: t0.Add(245 * time.Millisecond), Kind: sys.InputKeyUp, KeyCode: 2})
					rig.Emit(sys.InputEvent{At: t0.Add(260 * time.Millisecond), Kind: sys.InputModifiers, Mods: sys.Mods{}})
				}()
			}
		} else {
			spk = sfx.NewAudioSpeaker()
			rig := sys.NewPlatformRig()
			activeRig = rig
			eng = engine.New(rig, spk, cat.Snapshot())
			eng.SetShouldBypass(sys.ShouldBypassFullscreenOrGame)
			stopInput = rig.StartInput(eng.HandleInput, eng.SetTrusted)
			initCustom := cat.Snapshot().Settings.HelldiversPath
			go func() {
				eng.SetHelldiversReady(sys.DetectHelldivers(initCustom).Found)
			}()
		}

		var (
			pauseMu           sync.Mutex
			userPaused        bool
			onboardingOpen    bool
			onboardingLiveHUD bool
			e2eOpenAtLogin    bool
		)
		syncEnginePaused := func() {
			pauseMu.Lock()
			paused := userPaused || (onboardingOpen && !onboardingLiveHUD)
			liveHUD := onboardingOpen && onboardingLiveHUD && !userPaused
			pauseMu.Unlock()
			eng.SetPaused(paused)
			eng.SetPinnedPreview(liveHUD)
			if liveHUD {
				eng.SetDrillIntercept(func(catalog.Stratagem) bool { return true })
			} else {
				eng.SetDrillIntercept(nil)
			}
		}
		toggleUserPaused := func() {
			pauseMu.Lock()
			userPaused = !userPaused
			pauseMu.Unlock()
			syncEnginePaused()
		}
		getOpenAtLogin := func() bool {
			if isE2E {
				return e2eOpenAtLogin
			}
			return mygo.App.OpenAtLogin()
		}
		setOpenAtLogin := func(open bool) error {
			if isE2E {
				e2eOpenAtLogin = open
				return nil
			}
			return mygo.App.SetOpenAtLogin(open)
		}

		overlay = hud.NewOverlay()
		arsenalMgr = arsenal.New(cat, listAppOptions)
		onboardMgr = onboarding.New(cat, spk, onboarding.Hooks{
			OnOpen: func() {
				pauseMu.Lock()
				onboardingOpen = true
				onboardingLiveHUD = false
				pauseMu.Unlock()
				overlay.HideImmediate()
				syncEnginePaused()
			},
			OnClose: func(completed bool) {
				pauseMu.Lock()
				onboardingOpen = false
				onboardingLiveHUD = false
				pauseMu.Unlock()
				syncEnginePaused()
				overlay.HideImmediate()
				if completed {
					time.AfterFunc(95*time.Millisecond, func() {
						eng.Preview(3200 * time.Millisecond)
					})
				}
			},
			SetLiveDesktopHUD: func(active bool) {
				pauseMu.Lock()
				onboardingLiveHUD = active
				pauseMu.Unlock()
				syncEnginePaused()
			},
			GuidePermission: func() {
				openPermissionGuide(eng)
			},
			IsTrusted: func() bool {
				return eng.Snapshot().Trusted
			},
			GetOpenAtLogin: getOpenAtLogin,
			SetOpenAtLogin: setOpenAtLogin,
			OnHelldiversStatus: func(ready bool) {
				eng.SetHelldiversReady(ready)
			},
		})

		arsenalMgr.SetHooks(arsenal.Hooks{
			PreviewHUD: func() {
				eng.Preview(3 * time.Second)
			},
			OpenOnboarding: func() {
				arsenalMgr.DismissWindow()
				onboardMgr.Open()
			},
			OpenLogs: func() {
				if err := mygo.Shell.OpenPath(logDir); err != nil {
					slog.Warn("open log dir failed", "err", err)
				}
			},
			GuidePermission: func() {
				openPermissionGuide(eng)
			},
			IsTrusted: func() bool {
				return eng.Snapshot().Trusted
			},
			GetOpenAtLogin: getOpenAtLogin,
			SetOpenAtLogin: setOpenAtLogin,
			LaunchHelldivers: func() {
				custom := cat.Snapshot().Settings.HelldiversPath
				go func() {
					ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
					defer cancel()
					if err := activeRig.Execute(ctx, catalog.Step{Kind: catalog.StepLaunchHelldivers, Target: custom}, nil); err != nil {
						slog.Warn("manual helldivers launch failed", "err", err)
						return
					}
					spk.Play(sfx.CueHellpodLaunch)
				}()
			},
			OpenEnlistURL: func() {
				if err := mygo.Shell.OpenExternal(sys.HelldiversSteamStoreURL); err != nil {
					slog.Warn("open steam store url failed", "err", err)
				}
			},
			OnHelldiversStatus: func(ready bool) {
				eng.SetHelldiversReady(ready)
			},
		})
		eng.SetOnMissingHelldivers(func() {
			mygo.RunOnMain(func() {
				arsenalMgr.OpenStratagem(catalog.SystemJoinHelldiversID)
			})
		})

		trayMenu := setupTray(isDev, cat, eng, arsenalMgr, onboardMgr, toggleUserPaused)

		cat.Subscribe(func(next catalog.Snapshot) {
			eng.SetLoadout(next)
			eng.SetHelldiversReady(sys.DetectHelldivers(next.Settings.HelldiversPath).Found)
			snap := eng.Snapshot()
			syncTraySettings(trayMenu, isDev, snap.Trusted, snap.Paused, next.Settings)
		})

		eng.Subscribe(func(snap engine.Snapshot) {
			pauseMu.Lock()
			inOnboarding := onboardingOpen
			pauseMu.Unlock()
			if !inOnboarding {
				overlay.Apply(snap)
			}
			if onboardMgr != nil && onboardMgr.IsOpen() {
				onboardMgr.ObserveEngineSnapshot(snap)
			}
			syncTrayStatus(trayMenu, isDev, snap.Trusted, snap.Paused, snap.Loadout.Settings)
		})

		if os.Getenv("HELLAUNCHER_OPEN_ARSENAL") == "1" {
			arsenalMgr.Open()
		}

		wantOnboarding := (!isE2E && !cat.Snapshot().Settings.Onboarded) || os.Getenv("HELLAUNCHER_OPEN_ONBOARDING") == "1"
		if wantOnboarding {
			onboardMgr.Open()
		} else {
			overlay.Apply(eng.Snapshot())
			previewDur := 2400 * time.Millisecond
			if isE2E {
				previewDur = 6 * time.Second
			}
			eng.Preview(previewDur)
		}

		if runtime.GOOS == "darwin" && !isE2E && !wantOnboarding && !sys.CheckTrusted() && os.Getenv("HELLAUNCHER_NO_AUTO_GUIDE") != "1" {
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
	cat *catalog.Catalog,
	eng *engine.Engine,
	arsenalMgr *arsenal.Manager,
	onboardMgr *onboarding.Manager,
	togglePause func(),
) *mygo.Menu {
	initSnap := cat.Snapshot()
	st := initSnap.Settings
	tr := i18n.For(st.Language)
	engSnap := eng.Snapshot()

	pauseLbl := tr.TrayPauseHotkeys
	if engSnap.Paused {
		pauseLbl = tr.TrayResumeHotkeys
	}

	items := []*mygo.MenuItem{
		{ID: "status", Label: formatStatusLabel(isDev, engSnap.Trusted, engSnap.Paused, st), Disabled: true},
		{
			ID:    "open-arsenal",
			Label: tr.TrayOpenArsenal,
			Click: func(*mygo.MenuItem, *mygo.Window) {
				arsenalMgr.Open()
			},
		},
		{
			ID:    "preview-hud",
			Label: tr.TrayPreviewHUD,
			Click: func(*mygo.MenuItem, *mygo.Window) {
				eng.Preview(3 * time.Second)
			},
		},
		{
			ID:    "toggle-pause",
			Label: pauseLbl,
			Click: func(*mygo.MenuItem, *mygo.Window) {
				if togglePause != nil {
					togglePause()
				}
			},
		},
		{
			ID:    "open-onboarding",
			Label: tr.TrayOnboarding,
			Click: func(*mygo.MenuItem, *mygo.Window) {
				arsenalMgr.DismissWindow()
				onboardMgr.Open()
			},
		},
		mygo.Separator(),
		{ID: "quit", Label: tr.TrayQuit, Role: mygo.RoleQuit},
	}

	menu := mygo.NewMenu(items)
	iconBytes, isTemplate := trayicon.Icon()
	tooltip := "HelLauncher"
	if isDev {
		tooltip = "[DEV] HelLauncher"
	}
	tray, err := mygo.NewTray(mygo.TrayOptions{
		ID:             "hellauncher-main",
		Icon:           iconBytes,
		IconIsTemplate: isTemplate,
		ToolTip:        tooltip,
		Menu:           menu,
	})
	if err != nil {
		slog.Error("create tray icon failed", "err", err)
	} else if runtime.GOOS == "windows" {
		tray.OnClick(func() {
			tray.PopUpMenu()
		})
	}
	return menu
}

func formatStatusLabel(isDev bool, trusted bool, paused bool, st catalog.Settings) string {
	tr := i18n.For(st.Language)
	prefix := "✪ HelLauncher"
	if ship := strings.TrimSpace(st.ShipName); ship != "" {
		prefix = "✪ " + ship
	}
	if isDev {
		prefix += " [DEV]"
	}
	if runtime.GOOS == "darwin" && !trusted {
		return prefix + " · " + tr.TrayNeedPermission
	}
	if paused {
		return prefix + " · " + tr.TrayPausedStatus
	}
	return prefix + " · " + tr.TrayReady
}

func syncTrayStatus(menu *mygo.Menu, isDev bool, trusted bool, paused bool, st catalog.Settings) {
	if menu == nil {
		return
	}
	tr := i18n.For(st.Language)
	pauseLbl := tr.TrayPauseHotkeys
	if paused {
		pauseLbl = tr.TrayResumeHotkeys
	}
	mygo.RunOnMain(func() {
		if item := menu.ItemByID("status"); item != nil {
			item.SetLabel(formatStatusLabel(isDev, trusted, paused, st))
		}
		if item := menu.ItemByID("toggle-pause"); item != nil {
			item.SetLabel(pauseLbl)
		}
	})
}

func syncTraySettings(menu *mygo.Menu, isDev bool, trusted bool, paused bool, st catalog.Settings) {
	if menu == nil {
		return
	}
	tr := i18n.For(st.Language)
	pauseLbl := tr.TrayPauseHotkeys
	if paused {
		pauseLbl = tr.TrayResumeHotkeys
	}
	mygo.RunOnMain(func() {
		setLabel := func(id, text string) {
			if it := menu.ItemByID(id); it != nil {
				it.SetLabel(text)
			}
		}
		setLabel("status", formatStatusLabel(isDev, trusted, paused, st))
		setLabel("open-arsenal", tr.TrayOpenArsenal)
		setLabel("preview-hud", tr.TrayPreviewHUD)
		setLabel("toggle-pause", pauseLbl)
		setLabel("open-onboarding", tr.TrayOnboarding)
		setLabel("quit", tr.TrayQuit)
	})
}
