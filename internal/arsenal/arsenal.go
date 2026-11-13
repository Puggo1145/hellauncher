package arsenal

import (
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"helldriver/internal/catalog"
)

// AppOption describes an installed desktop application selectable in the Arsenal editor.
type AppOption struct {
	ID   string
	Name string
	Path string
}

var iconChoices = []string{
	"orbital-precision.svg",
	"eagle-500kg.svg",
	"eagle-airstrike.svg",
	"eagle-strafing-run.svg",
	"orbital-laser.svg",
	"orbital-railcannon.svg",
	"gatling-sentry.svg",
	"hellbomb.svg",
	"jump-pack.svg",
	"quasar-cannon.svg",
	"reinforce.svg",
	"resupply.svg",
	"shield-generator-pack.svg",
	"sos.svg",
	"super-earth-flag.svg",
	"upload-data.svg",
	"tactical-chrome",
	"tactical-zed",
	"tactical-terminal",
}

// Manager owns the on-demand native GPU Configurator window ("Arsenal").
//
// It depends strictly on *catalog.Catalog (plus an optional installed-app listing
// function) with zero coupling to OS hooks or audio playback.
type Manager struct {
	cat      *catalog.Catalog
	listApps func() []AppOption

	win       *mygo.Window
	unsub     func()
	closeOnce sync.Once

	// Bound global settings state (UI thread)
	snap        catalog.Snapshot
	triggerIdx  int
	soundOn     bool
	visorCurved bool

	// Selected / Draft Stratagem state (UI thread)
	selectedID    string
	isCreatingNew bool
	recording     bool
	draftID       string
	draftName     string
	draftSubtitle string
	draftCategory catalog.Category
	draftIcon     string
	draftEnabled  bool
	draftDelivery catalog.Delivery
	draftCode     []catalog.Dir
	draftStepKind catalog.StepKind
	draftTarget   string

	// Installed app quick-picker state
	apps     []AppOption
	appQuery string

	// Feedback banners
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

// Open creates and focuses the Arsenal window on demand, or brings the existing
// window to the front.
func (m *Manager) Open() {
	mygo.App.SetActivationPolicy(mygo.ActivationPolicyRegular)
	if m.win != nil {
		m.win.Show()
		m.win.Focus()
		return
	}

	m.errMsg = ""
	m.okMsg = ""
	m.recording = false
	m.reloadSnapshot(m.cat.Snapshot(), true)

	win := mygo.NewWindow(mygo.WindowOptions{
		Title:     "HellDriver 军械库 // SUPER EARTH ARSENAL",
		Width:     960,
		Height:    700,
		MinWidth:  840,
		MinHeight: 580,
		Content:   ui.View(m.View),
	})
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

func arsenalTheme() *ui.Theme {
	t := ui.DarkTheme()
	accent := ui.Hex("#ffe900")
	t.Background = ui.Hex("#0d1117")
	t.Surface = ui.Hex("#161c26")
	t.SurfaceHover = ui.Hex("#1f2735")
	t.SurfacePressed = ui.Hex("#263042")
	t.Border = ui.RGBA(255, 233, 0, 0.24)
	t.Text = ui.Hex("#f1f4f9")
	t.TextMuted = ui.Hex("#8f99aa")
	t.Accent = accent
	t.AccentHover = ui.Hex("#fff04d")
	t.AccentPressed = ui.Hex("#e6d200")
	t.AccentText = ui.Hex("#0b0d12")
	t.InactiveAccent = accent
	t.Danger = ui.Hex("#ff453a")
	t.Success = ui.Hex("#42d674")
	t.Radius = 5
	t.FontSize = 13
	return t
}

// View renders the complete Arsenal configurator UI.
func (m *Manager) View(c *ui.Context) {
	// Populate apps synchronously in headless tests when no window exists yet.
	if m.win == nil && len(m.apps) == 0 && m.listApps != nil {
		m.apps = m.listApps()
	}

	t := arsenalTheme()
	c.SetTheme(t)
	c.Root().Background(t.Background)

	m.captureRecordingKeys(c)

	ui.Column(c.Key("arsenal-root")).Fill().Children(func() {
		m.renderTopSettingsBar(c, t)
		ui.Divider(c)
		ui.Row(c.Key("arsenal-body")).Grow(1).MinHeight(0).AlignItems(ui.Stretch).Children(func() {
			m.renderRosterSidebar(c, t)
			ui.Divider(c)
			m.renderEditorPane(c, t)
		})
	})
}

func (m *Manager) captureRecordingKeys(c *ui.Context) {
	if !m.recording {
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
		}
	case c.Shortcut(0, ui.KeyEscape) || c.Shortcut(0, ui.KeyEnter):
		m.recording = false
	}
}

func (m *Manager) renderTopSettingsBar(c *ui.Context, t *ui.Theme) {
	ui.Column(c.Key("top-bar")).Shrink(0).Padding(14, 18).Gap(10).Background(ui.Hex("#11161f")).Children(func() {
		ui.Row(c).AlignItems(ui.Center).Gap(12).Children(func() {
			ui.Column(c).Grow(1).Gap(2).Children(func() {
				ui.Text(c, "✪ SUPER EARTH ARSENAL // 战略配备军械库").FontSize(17).Bold().TextColor(t.Accent)
				ui.Text(c, "所有战备启用后自动检验前缀无冲突（Prefix-Free Trie），保存即刻热更新至桌面终端").
					FontSize(11.5).
					TextColor(t.TextMuted)
			})
			if ui.Button(c.Key("btn-reset-defaults"), "恢复默认战备库").Clicked() {
				m.resetToDefaults()
			}
		})

		ui.Row(c.Key("global-prefs")).AlignItems(ui.Center).Gap(18).Children(func() {
			ui.Row(c).AlignItems(ui.Center).Gap(8).Children(func() {
				ui.Text(c, "唤出方式").FontSize(12).TextColor(t.TextMuted)
				if ui.Segmented(
					c.Key("seg-trigger"),
					&m.triggerIdx,
					"双击按住 Ctrl",
					"单按左 Ctrl",
					"右 Ctrl / Ctrl+Alt",
				).Changed() {
					m.commitGlobalSettings()
				}
			})

			ui.Spacer(c)

			ui.Row(c).AlignItems(ui.Center).Gap(6).Children(func() {
				if ui.Checkbox(c.Key("chk-sound"), &m.soundOn, "按键音效").Changed() {
					m.commitGlobalSettings()
				}
			})

			ui.Row(c).AlignItems(ui.Center).Gap(6).Children(func() {
				if ui.Checkbox(c.Key("chk-visor"), &m.visorCurved, "3D 头盔弧面").Changed() {
					m.commitGlobalSettings()
				}
			})
		})
	})
}

func (m *Manager) renderRosterSidebar(c *ui.Context, t *ui.Theme) {
	ui.Column(c.Key("roster-col")).Width(300).Shrink(0).MinHeight(0).Padding(14).Gap(10).Background(ui.Hex("#10151e")).Children(func() {
		ui.Row(c).Shrink(0).AlignItems(ui.Center).Gap(8).Children(func() {
			ui.Text(c, fmt.Sprintf("战备清单 (%d)", len(m.snap.Stratagems))).FontSize(13).Bold().Grow(1)
			if ui.PrimaryButton(c.Key("btn-new-strat"), "+ 新建战备").Clicked() {
				m.startNewDraft()
			}
		})

		ui.Scroll(c.Key("roster-scroll")).Grow(1).MinHeight(0).Children(func() {
			ui.Column(c).Gap(6).Children(func() {
				for _, st := range m.snap.Stratagems {
					isSelected := !m.isCreatingNew && m.selectedID == st.ID
					rowBg := t.Surface
					rowBorder := ui.RGBA(255, 255, 255, 0.07)
					if isSelected {
						rowBg = ui.RGBA(255, 233, 0, 0.14)
						rowBorder = t.Accent
					}

					card := ui.ButtonBase(c.Key("roster-item-"+st.ID)).
						Padding(9, 11).
						Radius(4).
						Background(rowBg).
						Border(1, rowBorder)

					card.Children(func() {
						ui.Column(c).Grow(1).Gap(4).Children(func() {
							ui.Row(c).AlignItems(ui.Center).Gap(6).Children(func() {
								ui.Box(c).Size(8, 8).Radius(4).Background(categoryDotColor(st.Category))
								ui.Text(c, st.Name).Bold().FontSize(13).SingleLine().Grow(1)
								statusTxt := "启用"
								statusCol := ui.Hex("#42d674")
								if !st.Enabled {
									statusTxt = "停用"
									statusCol = t.TextMuted
								}
								ui.Text(c, statusTxt).FontSize(10.5).TextColor(statusCol)
							})

							ui.Row(c).AlignItems(ui.Center).Gap(6).Children(func() {
								ui.Text(c, formatArrowString(st.Code)).
									FontSize(12).
									Bold().
									TextColor(t.Accent).
									Grow(1)
								if st.Delivery == catalog.DeliveryBeaconClick {
									ui.Text(c, "⊕信标落点").FontSize(10.5).TextColor(ui.Hex("#8cd2ff"))
								}
							})
						})
					})

					if card.Clicked() {
						m.selectStratagem(st.ID)
					}
				}
			})
		})
	})
}

func (m *Manager) renderEditorPane(c *ui.Context, t *ui.Theme) {
	ui.Column(c.Key("editor-col")).Grow(1).MinWidth(0).MinHeight(0).Children(func() {
		ui.Scroll(c.Key("editor-scroll")).Grow(1).MinWidth(0).MinHeight(0).Children(func() {
			ui.Column(c.Key("editor-form")).Padding(12, 20).Gap(9).Children(func() {
				// Header & Delete button
				ui.Row(c).AlignItems(ui.Center).Gap(10).Children(func() {
					heading := "编辑战略配备"
					if m.isCreatingNew {
						heading = "新建战略配备"
					}
					ui.Text(c, heading).FontSize(16).Bold().Grow(1)
					ui.Checkbox(c.Key("chk-draft-enabled"), &m.draftEnabled, "启用此战备")
					if !m.isCreatingNew && len(m.snap.Stratagems) > 1 {
						if ui.Button(c.Key("btn-delete-strat"), "删除战备").Clicked() {
							m.deleteSelectedStratagem()
						}
					}
				})

				// Validation / Conflict Error or Success Banner
				if m.errMsg != "" {
					ui.Row(c.Key("banner-err")).
						Padding(10, 12).
						Radius(4).
						Background(ui.RGBA(255, 69, 58, 0.16)).
						Border(1, ui.Hex("#ff453a")).
						Children(func() {
							ui.Text(c, "⚠ "+m.errMsg).FontSize(12.5).Bold().TextColor(ui.Hex("#ffb4ae"))
						})
				} else if m.okMsg != "" {
					ui.Row(c.Key("banner-ok")).
						Padding(8, 12).
						Radius(4).
						Background(ui.RGBA(66, 214, 116, 0.14)).
						Border(1, ui.Hex("#42d674")).
						Children(func() {
							ui.Text(c, m.okMsg).FontSize(12).Bold().TextColor(ui.Hex("#42d674"))
						})
				}

				// Name & Subtitle
				ui.Row(c.Key("row-names")).Gap(12).Children(func() {
					ui.Column(c).Grow(1).Gap(5).Children(func() {
						ui.Text(c, "战备名称").FontSize(12).TextColor(t.TextMuted)
						ui.TextInput(c.Key("inp-name"), &m.draftName).Placeholder("例如：打开 Ghostty 终端")
					})
					ui.Column(c).Grow(1).Gap(5).Children(func() {
						ui.Text(c, "副标题说明").FontSize(12).TextColor(t.TextMuted)
						ui.TextInput(c.Key("inp-subtitle"), &m.draftSubtitle).Placeholder("显示在 HUD 列表右侧")
					})
				})

				// Category & Icon
				ui.Row(c.Key("row-cat-icon")).Gap(16).AlignItems(ui.Center).Children(func() {
					ui.Column(c).Grow(1).Gap(5).Children(func() {
						ui.Text(c, "战备分类色标").FontSize(12).TextColor(t.TextMuted)
						ui.Row(c).Gap(10).Children(func() {
							ui.Radio(c.Key("cat-blue"), &m.draftCategory, catalog.CategoryBlue, "蓝·支援")
							ui.Radio(c.Key("cat-red"), &m.draftCategory, catalog.CategoryRed, "红·清场")
							ui.Radio(c.Key("cat-green"), &m.draftCategory, catalog.CategoryGreen, "绿·终端")
							ui.Radio(c.Key("cat-yellow"), &m.draftCategory, catalog.CategoryYellow, "黄·补给")
						})
					})
					ui.Column(c).Width(240).Gap(5).Children(func() {
						ui.Text(c, "HUD 图标").FontSize(12).TextColor(t.TextMuted)
						ui.Select(c.Key("sel-icon"), &m.draftIcon, iconChoices)
					})
				})

				// Delivery Mode (Immediate vs BeaconClick)
				ui.Column(c.Key("col-delivery")).Gap(5).Children(func() {
					ui.Text(c, "投送触发模式").FontSize(12).TextColor(t.TextMuted)
					ui.Row(c).Gap(16).Children(func() {
						ui.Radio(c.Key("deliv-imm"), &m.draftDelivery, catalog.DeliveryImmediate, "⚡ 输完序列立即触发")
						ui.Radio(c.Key("deliv-beacon"), &m.draftDelivery, catalog.DeliveryBeaconClick, "⊕ 握持信标 · 左键点击目标位置空投")
					})
				})

				// Key-Chord Recorder
				m.renderChordRecorder(c, t)

				// Step Action Editor + Installed App Quick Picker
				m.renderStepEditor(c, t)
			})
		})

		ui.Divider(c)

		// Sticky Save / Reset Footer
		ui.Row(c.Key("editor-actions")).Shrink(0).Padding(12, 22).Background(ui.Hex("#11161f")).AlignItems(ui.Center).Gap(10).Children(func() {
			if ui.Button(c.Key("btn-revert"), "重置未保存修改").Clicked() {
				m.recording = false
				m.errMsg = ""
				m.okMsg = ""
				if m.isCreatingNew {
					m.startNewDraft()
				} else {
					m.selectStratagem(m.selectedID)
				}
			}
			ui.Spacer(c)
			if ui.PrimaryButton(c.Key("btn-save"), "保存战备配置").Clicked() {
				m.saveDraft()
			}
		})
	})
}

func (m *Manager) renderChordRecorder(c *ui.Context, t *ui.Theme) {
	boxBorder := ui.RGBA(255, 233, 0, 0.28)
	if m.recording {
		boxBorder = t.Accent
	}

	ui.Column(c.Key("chord-box")).
		Padding(10, 12).
		Gap(7).
		Radius(5).
		Background(ui.Hex("#131923")).
		Border(1.5, boxBorder).
		Children(func() {
			ui.Row(c).AlignItems(ui.Center).Gap(10).Children(func() {
				ui.Column(c).Grow(1).Gap(2).Children(func() {
					ui.Text(c, "指令序列（WASD / 方向键，1–12 步）").FontSize(12.5).Bold()
					hint := "点击右侧按钮录制，或使用下方方向按钮逐步编排"
					if m.recording {
						hint = "● 正在录制按键：请按 W / A / S / D 或 ↑ ↓ ← → 输入，Backspace 退格，Enter 结束"
					}
					ui.Text(c, hint).FontSize(11).TextColor(t.TextMuted)
				})
				recLabel := "录制按键序列"
				if m.recording {
					recLabel = "完成录制"
				}
				if ui.Button(c.Key("btn-toggle-record"), recLabel).Clicked() {
					m.recording = !m.recording
				}
			})

			// Current Arrows Display
			ui.Row(c.Key("chord-preview")).
				Padding(10, 12).
				Radius(4).
				Background(ui.Hex("#0b0f16")).
				AlignItems(ui.Center).
				Gap(8).
				Children(func() {
					if len(m.draftCode) == 0 {
						ui.Text(c, "尚未录入方向序列…").FontSize(13).TextColor(t.TextMuted)
					} else {
						for idx, d := range m.draftCode {
							ui.Row(c.Key(idx)).
								Padding(4, 9).
								Radius(3).
								Background(ui.RGBA(255, 233, 0, 0.16)).
								Border(1, t.Accent).
								Children(func() {
									ui.Text(c, d.Arrow()).FontSize(16).Bold().TextColor(t.Accent)
								})
						}
					}
				})

			// Manual step buttons
			ui.Row(c.Key("chord-pad")).Gap(8).AlignItems(ui.Center).Children(func() {
				addStep := func(d catalog.Dir) {
					if len(m.draftCode) < 12 {
						m.draftCode = append(m.draftCode, d)
						m.errMsg = ""
						m.okMsg = ""
					}
				}
				if ui.Button(c.Key("pad-up"), "+ ↑ (W)").Clicked() {
					addStep(catalog.DirUp)
				}
				if ui.Button(c.Key("pad-down"), "+ ↓ (S)").Clicked() {
					addStep(catalog.DirDown)
				}
				if ui.Button(c.Key("pad-left"), "+ ← (A)").Clicked() {
					addStep(catalog.DirLeft)
				}
				if ui.Button(c.Key("pad-right"), "+ → (D)").Clicked() {
					addStep(catalog.DirRight)
				}
				ui.Spacer(c)
				if ui.Button(c.Key("pad-back"), "退格").Disabled(len(m.draftCode) == 0).Clicked() {
					m.draftCode = m.draftCode[:len(m.draftCode)-1]
				}
				if ui.Button(c.Key("pad-clear"), "清空序列").Disabled(len(m.draftCode) == 0).Clicked() {
					m.draftCode = nil
				}
			})
		})
}

func (m *Manager) renderStepEditor(c *ui.Context, t *ui.Theme) {
	ui.Column(c.Key("step-box")).
		Padding(10, 12).
		Gap(7).
		Radius(5).
		Background(ui.Hex("#131923")).
		Border(1, ui.RGBA(255, 255, 255, 0.08)).
		Children(func() {
			ui.Text(c, "部署执行动作").FontSize(12.5).Bold()

			ui.Row(c.Key("step-kinds")).Gap(10).Children(func() {
				ui.Radio(c.Key("sk-app"), &m.draftStepKind, catalog.StepLaunchApp, "启动应用")
				ui.Radio(c.Key("sk-close"), &m.draftStepKind, catalog.StepCloseActiveWindow, "关闭当前窗口")
				ui.Radio(c.Key("sk-open"), &m.draftStepKind, catalog.StepOpenTarget, "打开路径/URL")
				ui.Radio(c.Key("sk-short"), &m.draftStepKind, catalog.StepRunShortcut, "系统快捷指令")
				ui.Radio(c.Key("sk-shell"), &m.draftStepKind, catalog.StepShellCommand, "Shell 命令")
			})

			if m.draftStepKind != catalog.StepCloseActiveWindow {
				placeholder := "输入应用 Bundle ID、路径或预设名（如 chrome / zed / terminal / files）"
				switch m.draftStepKind {
				case catalog.StepOpenTarget:
					placeholder = "例如：https://github.com 或 ~/Downloads"
				case catalog.StepRunShortcut:
					placeholder = "macOS 快捷指令名称 / Windows PowerShell 脚本"
				case catalog.StepShellCommand:
					placeholder = "例如：open -a Simulator"
				}
				ui.Column(c.Key("step-target-col")).Gap(5).Children(func() {
					ui.Text(c, "动作目标参数").FontSize(12).TextColor(t.TextMuted)
					ui.TextInput(c.Key("inp-step-target"), &m.draftTarget).Placeholder(placeholder)
				})
			}

			if m.draftStepKind == catalog.StepLaunchApp && len(m.apps) > 0 {
				ui.Column(c.Key("app-picker")).Gap(6).Children(func() {
					ui.Row(c).AlignItems(ui.Center).Gap(8).Children(func() {
						ui.Text(c, "已安装桌面应用速选").FontSize(11.5).TextColor(t.TextMuted)
						ui.TextInput(c.Key("inp-app-filter"), &m.appQuery).
							Grow(1).
							Placeholder("搜索本机已安装应用自动填入…")
					})

					filtered := m.filteredApps()
					ui.ScrollHorizontal(c.Key("app-chips")).Children(func() {
						ui.Row(c).Gap(6).Padding(2, 0).Children(func() {
							limit := min(len(filtered), 18)
							for i := range limit {
								app := filtered[i]
								targetVal := app.ID
								if targetVal == "" {
									targetVal = app.Path
								}
								if ui.Button(c.Key("pick-app-"+targetVal+"-"+app.Name), app.Name).Clicked() {
									m.draftTarget = targetVal
									if m.draftName == "" || m.draftName == "新建战略配备" {
										m.draftName = app.Name
									}
								}
							}
						})
					})
				})
			}
		})
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

func (m *Manager) reloadSnapshot(snap catalog.Snapshot, resetDraft bool) {
	m.snap = snap
	switch snap.Settings.TriggerMode {
	case catalog.ModeHoldLeftCtrl:
		m.triggerIdx = 1
	case catalog.ModeRightCtrlOrAlt:
		m.triggerIdx = 2
	default:
		m.triggerIdx = 0
	}
	m.soundOn = !snap.Settings.Muted
	m.visorCurved = snap.Settings.VisorCurved

	if !resetDraft && (m.isCreatingNew || m.selectedID != "") {
		if !m.isCreatingNew {
			for _, st := range snap.Stratagems {
				if st.ID == m.selectedID {
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
			m.selectedID = st.ID
			m.draftID = st.ID
			m.draftName = st.Name
			m.draftSubtitle = st.Subtitle
			m.draftCategory = st.Category
			m.draftIcon = st.Icon
			m.draftEnabled = st.Enabled
			m.draftDelivery = st.Delivery
			m.draftCode = slices.Clone(st.Code)
			if len(st.Steps) > 0 {
				m.draftStepKind = st.Steps[0].Kind
				m.draftTarget = st.Steps[0].Target
			} else {
				m.draftStepKind = catalog.StepLaunchApp
				m.draftTarget = ""
			}
			m.errMsg = ""
			m.okMsg = ""
			return
		}
	}
}

func (m *Manager) startNewDraft() {
	m.isCreatingNew = true
	m.recording = false
	m.selectedID = ""
	m.draftID = fmt.Sprintf("custom-%d", time.Now().UnixNano())
	m.draftName = "新建战略配备"
	m.draftSubtitle = "自定义桌面战术动作"
	m.draftCategory = catalog.CategoryBlue
	m.draftIcon = "orbital-precision.svg"
	m.draftEnabled = true
	m.draftDelivery = catalog.DeliveryImmediate
	m.draftCode = nil
	m.draftStepKind = catalog.StepLaunchApp
	m.draftTarget = ""
	m.errMsg = ""
	m.okMsg = ""
}

func (m *Manager) commitGlobalSettings() {
	mode := catalog.ModeDoubleHoldCtrl
	switch m.triggerIdx {
	case 1:
		mode = catalog.ModeHoldLeftCtrl
	case 2:
		mode = catalog.ModeRightCtrlOrAlt
	}
	muted := !m.soundOn
	visor := m.visorCurved

	if err := m.cat.Update(func(s *catalog.Snapshot) error {
		s.Settings.TriggerMode = mode
		s.Settings.Muted = muted
		s.Settings.VisorCurved = visor
		return nil
	}); err != nil {
		m.errMsg = err.Error()
	}
}

func (m *Manager) saveDraft() {
	m.recording = false
	target := strings.TrimSpace(m.draftTarget)
	if m.draftStepKind == catalog.StepCloseActiveWindow {
		target = ""
	}
	candidate := catalog.Stratagem{
		ID:       m.draftID,
		Name:     strings.TrimSpace(m.draftName),
		Subtitle: strings.TrimSpace(m.draftSubtitle),
		Category: m.draftCategory,
		Icon:     m.draftIcon,
		Enabled:  m.draftEnabled,
		Code:     slices.Clone(m.draftCode),
		Delivery: m.draftDelivery,
		Steps: []catalog.Step{
			{Kind: m.draftStepKind, Target: target},
		},
	}

	creating := m.isCreatingNew
	err := m.cat.Update(func(s *catalog.Snapshot) error {
		if creating {
			s.Stratagems = append(s.Stratagems, candidate)
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
		m.errMsg = err.Error()
		m.okMsg = ""
		return
	}

	m.isCreatingNew = false
	m.selectedID = candidate.ID
	m.errMsg = ""
	m.okMsg = "✓ 战备已保存并同步至战术终端"
}

func (m *Manager) deleteSelectedStratagem() {
	targetID := m.selectedID
	if targetID == "" {
		return
	}
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
		m.errMsg = err.Error()
		return
	}
	m.selectedID = ""
	m.reloadSnapshot(m.cat.Snapshot(), true)
	m.okMsg = "✓ 已移除战备"
}

func (m *Manager) resetToDefaults() {
	err := m.cat.Update(func(s *catalog.Snapshot) error {
		s.Stratagems = catalog.DefaultStratagems()
		return nil
	})
	if err != nil {
		m.errMsg = err.Error()
		return
	}
	m.reloadSnapshot(m.cat.Snapshot(), true)
	m.okMsg = "✓ 已恢复超级地球出厂战备清单"
}

func formatArrowString(code []catalog.Dir) string {
	var b strings.Builder
	for _, d := range code {
		b.WriteString(d.Arrow())
	}
	return b.String()
}

func categoryDotColor(cat catalog.Category) ui.Color {
	switch cat {
	case catalog.CategoryRed:
		return ui.Hex("#e05347")
	case catalog.CategoryGreen:
		return ui.Hex("#67b85c")
	case catalog.CategoryYellow:
		return ui.Hex("#e5c538")
	default:
		return ui.Hex("#4fb3d0")
	}
}
