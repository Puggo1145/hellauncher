package catalog

import (
	"encoding/json"
	"errors"
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
)

const currentSchemaVersion = 1

const (
	// SystemJoinHelldiversID is the immutable, non-removable Super Earth system Stratagem.
	SystemJoinHelldiversID   = "sys-join-helldivers"
	SystemJoinHelldiversIcon = "reinforce.svg"

	defaultCloseWinZH       = "轨道精准攻击：关闭窗口"
	defaultCloseWinEN       = "Orbital Precision Strike: Close Window"
	legacyDefaultCloseWinZH = "轨道精准处决：关闭窗口"
	legacyDefaultCloseWinEN = "Orbital Strike: Execute Window"
	defaultResupplyZH       = "呼叫文件补给箱"
	defaultResupplyEN       = "Call In File Resupply"
)

// SystemJoinHelldiversCode is the canonical Helldivers 2 Reinforce sequence (↑↓→←↑).
var SystemJoinHelldiversCode = []Dir{DirUp, DirDown, DirRight, DirLeft, DirUp}

// IsSystemStratagem reports whether id is a locked system-level Stratagem.
func IsSystemStratagem(id string) bool {
	return id == SystemJoinHelldiversID
}

// DefaultFileManagerTarget returns the native OS file manager target identifier.
func DefaultFileManagerTarget() string {
	switch runtime.GOOS {
	case "windows":
		return "explorer.exe"
	case "darwin":
		return "Finder"
	default:
		return "files"
	}
}

// Dir represents one directional input in a Stratagem sequence.
type Dir string

const (
	DirUp    Dir = "U"
	DirDown  Dir = "D"
	DirLeft  Dir = "L"
	DirRight Dir = "R"
)

func (d Dir) Valid() bool {
	switch d {
	case DirUp, DirDown, DirLeft, DirRight:
		return true
	default:
		return false
	}
}

func (d Dir) Arrow() string {
	switch d {
	case DirUp:
		return "↑"
	case DirDown:
		return "↓"
	case DirLeft:
		return "←"
	case DirRight:
		return "→"
	default:
		return "?"
	}
}

// TriggerMode defines how the global Stratagem HUD is summoned.
type TriggerMode string

const (
	ModeDoubleHoldCtrl TriggerMode = "double_hold_ctrl"
	ModeHoldLeftCtrl   TriggerMode = "hold_left_ctrl"
	ModeRightCtrlOrAlt TriggerMode = "right_ctrl_or_alt"
)

func (m TriggerMode) Valid() bool {
	switch m {
	case ModeDoubleHoldCtrl, ModeHoldLeftCtrl, ModeRightCtrlOrAlt:
		return true
	default:
		return false
	}
}

// Delivery controls whether a matched Stratagem executes immediately or waits for a mouse click.
type Delivery string

const (
	DeliveryImmediate   Delivery = "immediate"
	DeliveryBeaconClick Delivery = "beacon_click"
)

func (d Delivery) Valid() bool {
	return d == DeliveryImmediate || d == DeliveryBeaconClick
}

// Category selects the Super Earth color badge of a Stratagem card.
type Category string

const (
	CategoryBlue   Category = "blue"
	CategoryRed    Category = "red"
	CategoryGreen  Category = "green"
	CategoryYellow Category = "yellow"
)

func (c Category) Valid() bool {
	switch c {
	case CategoryBlue, CategoryRed, CategoryGreen, CategoryYellow:
		return true
	default:
		return false
	}
}

// HUDTheme selects the visual presentation of the floating Stratagem overlay.
type HUDTheme string

const (
	ThemeClassic  HUDTheme = "classic"
	ThemeTactical HUDTheme = "tactical"
)

func (t HUDTheme) Valid() bool {
	return t == ThemeClassic || t == ThemeTactical
}

// Language selects the UI display language.
type Language string

const (
	LangZH Language = "zh"
	LangEN Language = "en"
)

func (l Language) Valid() bool {
	return l == LangZH || l == LangEN
}

// StepKind identifies one executable OS operation inside a Stratagem pipeline.
type StepKind string

const (
	StepLaunchApp         StepKind = "launch_app"
	StepCloseActiveWindow StepKind = "close_active_window"
	StepOpenTarget        StepKind = "open_target"
	StepRunShortcut       StepKind = "run_shortcut"
	StepShellCommand      StepKind = "shell_command"
	StepLaunchHelldivers  StepKind = "launch_helldivers"
	StepSystemAction      StepKind = "system_action"
)

func (k StepKind) Valid() bool {
	switch k {
	case StepLaunchApp, StepCloseActiveWindow, StepOpenTarget, StepRunShortcut, StepShellCommand, StepLaunchHelldivers, StepSystemAction:
		return true
	default:
		return false
	}
}

// SystemAction identifies a built-in zero-config desktop window or OS directive.
type SystemAction string

const (
	SysWinLeft        SystemAction = "win_left"
	SysWinRight       SystemAction = "win_right"
	SysWinMaximize    SystemAction = "win_maximize"
	SysWinCenter      SystemAction = "win_center"
	SysWinMinimize    SystemAction = "win_minimize"
	SysShowDesktop    SystemAction = "show_desktop"
	SysLockScreen     SystemAction = "lock_screen"
	SysScreenshot     SystemAction = "screenshot"
	SysMediaPlayPause SystemAction = "media_play_pause"
	SysVolumeMute     SystemAction = "volume_mute"
	SysTaskManager    SystemAction = "task_manager"
	SysEmptyTrash     SystemAction = "empty_trash"
)

func (a SystemAction) Valid() bool {
	switch a {
	case SysWinLeft, SysWinRight, SysWinMaximize, SysWinCenter, SysWinMinimize,
		SysShowDesktop, SysLockScreen, SysScreenshot, SysMediaPlayPause,
		SysVolumeMute, SysTaskManager, SysEmptyTrash:
		return true
	default:
		return false
	}
}

// Step describes a single desktop action executed as part of a Stratagem.
type Step struct {
	Kind   StepKind `json:"kind"`
	Target string   `json:"target,omitempty"`
	Args   []string `json:"args,omitempty"`
}

// Stratagem defines one tactical shortcut in the user's loadout.
type Stratagem struct {
	ID       string   `json:"id"`
	Name     string   `json:"name"`
	Subtitle string   `json:"subtitle"`
	Category Category `json:"category"`
	Icon     string   `json:"icon"`
	Enabled  bool     `json:"enabled"`
	Code     []Dir    `json:"code"`
	Delivery Delivery `json:"delivery"`
	Steps    []Step   `json:"steps"`
}

// Settings holds global user preferences.
type Settings struct {
	TriggerMode       TriggerMode `json:"triggerMode"`
	Muted             bool        `json:"muted"`
	VisorCurved       bool        `json:"visorCurved"`
	HUDTheme          HUDTheme    `json:"hudTheme,omitempty"`
	Language          Language    `json:"language,omitempty"`
	HelldiversPath    string      `json:"helldiversPath,omitempty"`
	ShipName          string      `json:"shipName,omitempty"`
	Onboarded         bool        `json:"onboarded,omitempty"`
	DisableGameBypass bool        `json:"disableGameBypass,omitempty"`
}

var (
	shipPrefixesZH = []string{
		"钢铁", "自由", "管理式民主", "家庭价值观", "个人功绩",
		"超级地球", "黎明", "星辰", "永恒", "无畏", "爱国者", "正义",
	}
	shipSuffixesZH = []string{
		"之拳号", "之翼号", "之锤号", "先驱号", "民选代表号",
		"审判官号", "捍卫者号", "破晓者号", "之矛号", "执政官号",
	}
	shipPrefixesEN = []string{
		"Fist", "Wing", "Hammer", "Harbinger", "Elected Representative",
		"Arbiter", "Spear", "Beacon", "Sword", "Champion", "Distributor", "Paragon",
	}
	shipSuffixesEN = []string{
		"of Family Values", "of Managed Democracy", "of Individual Merit",
		"of Dawn", "of Super Earth", "of Patriotism", "of Self-Determination",
		"of Iron", "of Liberty", "of Humankind",
	}
)

// DefaultShipName returns the canonical Super Destroyer christening name.
func DefaultShipName(lang Language) string {
	if lang == LangEN {
		return "SES Fist of Family Values"
	}
	return "SES 家庭价值观之拳号"
}

// RandomShipName rolls an authentic satirical Helldivers 2 Super Destroyer name.
func RandomShipName(lang Language) string {
	if lang == LangEN {
		p := shipPrefixesEN[rand.IntN(len(shipPrefixesEN))]
		s := shipSuffixesEN[rand.IntN(len(shipSuffixesEN))]
		return "SES " + p + " " + s
	}
	p := shipPrefixesZH[rand.IntN(len(shipPrefixesZH))]
	s := shipSuffixesZH[rand.IntN(len(shipSuffixesZH))]
	return "SES " + p + s
}

// Snapshot is an immutable value representing the entire validated Catalog state.
type Snapshot struct {
	Settings   Settings    `json:"settings"`
	Stratagems []Stratagem `json:"stratagems"`
}

// ConflictError reports that one enabled Stratagem code is a prefix of (or identical to) another.
type ConflictError struct {
	FirstID    string
	FirstName  string
	SecondID   string
	SecondName string
	Prefix     []Dir
}

func (e *ConflictError) PrefixArrows() string {
	var arrows strings.Builder
	for _, d := range e.Prefix {
		arrows.WriteString(d.Arrow())
	}
	return arrows.String()
}

func (e *ConflictError) Error() string {
	return fmt.Sprintf("指令序列冲突：「%s」与「%s」在前缀 [%s] 上重叠", e.FirstName, e.SecondName, e.PrefixArrows())
}

// ValidationCode identifies a semantic validation failure for i18n formatting.
type ValidationCode string

const (
	ValInvalidTriggerMode ValidationCode = "invalid_trigger_mode"
	ValInvalidHUDTheme    ValidationCode = "invalid_hud_theme"
	ValInvalidLanguage    ValidationCode = "invalid_language"
	ValEmptyID            ValidationCode = "empty_id"
	ValDuplicateID        ValidationCode = "duplicate_id"
	ValEmptyName          ValidationCode = "empty_name"
	ValInvalidCategory    ValidationCode = "invalid_category"
	ValInvalidDelivery    ValidationCode = "invalid_delivery"
	ValCodeLength         ValidationCode = "code_length"
	ValInvalidDir         ValidationCode = "invalid_dir"
	ValEmptySteps         ValidationCode = "empty_steps"
	ValInvalidStepKind    ValidationCode = "invalid_step_kind"
	ValEmptyStepTarget    ValidationCode = "empty_step_target"
	ValSystemProtected    ValidationCode = "system_protected"
)

// ValidationError reports an invalid Stratagem or Settings field.
type ValidationError struct {
	StratagemID string
	Code        ValidationCode
	Reason      string
}

func (e *ValidationError) Error() string {
	if e.StratagemID == "" {
		return e.Reason
	}
	return fmt.Sprintf("战备 [%s] 配置无效：%s", e.StratagemID, e.Reason)
}

type diskEnvelope struct {
	Version    int         `json:"version"`
	Settings   Settings    `json:"settings"`
	Stratagems []Stratagem `json:"stratagems"`
}

// SystemStratagem constructs the canonical immutable "加入绝地潜兵" Stratagem.
func SystemStratagem(lang Language, customPath string) Stratagem {
	name := "加入绝地潜兵"
	subtitle := "发射地狱空投舱启动《绝地潜兵 2》，传播管理式民主"
	if lang == LangEN {
		name = "Join the Helldivers"
		subtitle = "Launch HELLDIVERS™ 2 — Spread Managed Democracy"
	}
	return Stratagem{
		ID:       SystemJoinHelldiversID,
		Name:     name,
		Subtitle: subtitle,
		Category: CategoryYellow,
		Icon:     SystemJoinHelldiversIcon,
		Enabled:  true,
		Code:     slices.Clone(SystemJoinHelldiversCode),
		Delivery: DeliveryImmediate,
		Steps: []Step{
			{Kind: StepLaunchHelldivers, Target: strings.TrimSpace(customPath)},
		},
	}
}

// DefaultSettings returns the out-of-the-box user preferences.
func DefaultSettings() Settings {
	return Settings{
		TriggerMode: ModeDoubleHoldCtrl,
		Muted:       false,
		VisorCurved: true,
		HUDTheme:    ThemeClassic,
		Language:    LangZH,
	}
}

// DefaultStratagems returns the default prefix-free Stratagem loadout in Chinese.
func DefaultStratagems() []Stratagem {
	return DefaultStratagemsFor(LangZH, "")
}

// DefaultStratagemsFor returns the out-of-the-box Stratagem loadout localized for lang:
//  1. Orbital Precision Strike -> Close Active Window (→→↑)
//  2. Resupply -> Open File Manager (↓↓↑→)
//  3. Join the Helldivers -> System built-in game launcher (↑↓→←↑)
func DefaultStratagemsFor(lang Language, customHelldiversPath string) []Stratagem {
	closeName := defaultCloseWinZH
	closeSub := "判定当前聚焦窗口思想叛国，由超级驱逐舰降下民主制裁"
	resupplyName := defaultResupplyZH
	resupplySub := "向超级地球后勤部申请本地文件管理器空投支援"
	if lang == LangEN {
		closeName = defaultCloseWinEN
		closeSub = "Execute the active window for thought-treason via Orbital Precision Strike"
		resupplyName = defaultResupplyEN
		resupplySub = "Request Super Earth logistics to drop a local file manager pod"
	}

	return []Stratagem{
		{
			ID:       "orbital-close-win",
			Name:     closeName,
			Subtitle: closeSub,
			Category: CategoryRed,
			Icon:     "orbital-precision.svg",
			Enabled:  true,
			Code:     []Dir{DirRight, DirRight, DirUp}, // →→↑ (Orbital Precision Strike)
			Delivery: DeliveryImmediate,
			Steps:    []Step{{Kind: StepCloseActiveWindow}},
		},
		{
			ID:       "resupply-files",
			Name:     resupplyName,
			Subtitle: resupplySub,
			Category: CategoryYellow,
			Icon:     "resupply.svg",
			Enabled:  true,
			Code:     []Dir{DirDown, DirDown, DirUp, DirRight}, // ↓↓↑→ (Resupply)
			Delivery: DeliveryImmediate,
			Steps:    []Step{{Kind: StepLaunchApp, Target: DefaultFileManagerTarget()}},
		},
		SystemStratagem(lang, customHelldiversPath),
	}
}

// DefaultSnapshot returns a fresh copy of the default Settings and Stratagems.
func DefaultSnapshot() Snapshot {
	return Snapshot{
		Settings:   DefaultSettings(),
		Stratagems: DefaultStratagems(),
	}
}

func cloneSnapshot(s Snapshot) Snapshot {
	out := Snapshot{
		Settings:   s.Settings,
		Stratagems: make([]Stratagem, len(s.Stratagems)),
	}
	for i, st := range s.Stratagems {
		steps := make([]Step, len(st.Steps))
		for j, step := range st.Steps {
			steps[j] = Step{
				Kind:   step.Kind,
				Target: step.Target,
				Args:   slices.Clone(step.Args),
			}
		}
		out.Stratagems[i] = Stratagem{
			ID:       st.ID,
			Name:     st.Name,
			Subtitle: st.Subtitle,
			Category: st.Category,
			Icon:     st.Icon,
			Enabled:  st.Enabled,
			Code:     slices.Clone(st.Code),
			Delivery: st.Delivery,
			Steps:    steps,
		}
	}
	return out
}

func migrateLegacyIcon(icon string) string {
	switch icon {
	case "tactical-chrome":
		return "fast-recon-vehicle.svg"
	case "tactical-zed":
		return "orbital-laser.svg"
	case "tactical-terminal":
		return "upload-data.svg"
	case "":
		return "orbital-precision.svg"
	default:
		return icon
	}
}

// CodesOverlap reports whether two non-empty directional sequences share a prefix
// (one is identical to or a strict prefix of the other).
func CodesOverlap(a, b []Dir) bool {
	if len(a) == 0 || len(b) == 0 {
		return false
	}
	n := min(len(a), len(b))
	return slices.Equal(a[:n], b[:n])
}

func ensureSystemStratagems(s *Snapshot) bool {
	canonical := SystemStratagem(s.Settings.Language, s.Settings.HelldiversPath)
	migrated := false
	found := false
	for i := range s.Stratagems {
		if strings.TrimSpace(s.Stratagems[i].ID) == SystemJoinHelldiversID {
			if s.Stratagems[i].Icon != canonical.Icon || !slices.Equal(s.Stratagems[i].Code, canonical.Code) {
				migrated = true
			}
			s.Stratagems[i] = canonical
			found = true
			break
		}
	}
	if !found {
		s.Stratagems = append(s.Stratagems, canonical)
		migrated = true
	}

	// Defensive migration: if an older user stratagem overlaps the locked Reinforce
	// code (↑↓→←↑), disable it rather than discarding the user's catalog.json.
	for i := range s.Stratagems {
		st := &s.Stratagems[i]
		if st.ID == SystemJoinHelldiversID || !st.Enabled {
			continue
		}
		if CodesOverlap(st.Code, SystemJoinHelldiversCode) {
			st.Enabled = false
			migrated = true
		}
	}
	return migrated
}

func normalizeSnapshot(s *Snapshot) {
	if !s.Settings.TriggerMode.Valid() {
		s.Settings.TriggerMode = ModeDoubleHoldCtrl
	}
	if !s.Settings.HUDTheme.Valid() {
		s.Settings.HUDTheme = ThemeClassic
	}
	if !s.Settings.Language.Valid() {
		s.Settings.Language = LangZH
	}
	s.Settings.HelldiversPath = strings.TrimSpace(s.Settings.HelldiversPath)
	s.Settings.ShipName = strings.TrimSpace(s.Settings.ShipName)
	if runes := []rune(s.Settings.ShipName); len(runes) > 48 {
		s.Settings.ShipName = string(runes[:48])
	}

	sysRef := SystemStratagem(s.Settings.Language, s.Settings.HelldiversPath)
	for i := range s.Stratagems {
		st := &s.Stratagems[i]
		st.ID = strings.TrimSpace(st.ID)
		st.Name = strings.TrimSpace(st.Name)
		st.Subtitle = strings.TrimSpace(st.Subtitle)
		st.Icon = migrateLegacyIcon(strings.TrimSpace(st.Icon))
		if !st.Category.Valid() {
			st.Category = CategoryBlue
		}
		if !st.Delivery.Valid() {
			st.Delivery = DeliveryImmediate
		}
		for j := range st.Steps {
			st.Steps[j].Target = strings.TrimSpace(st.Steps[j].Target)
		}

		// Keep untouched default names localized when switching UI language.
		switch st.ID {
		case SystemJoinHelldiversID:
			st.Name = sysRef.Name
			st.Subtitle = sysRef.Subtitle
			if len(st.Steps) == 1 && st.Steps[0].Kind == StepLaunchHelldivers {
				st.Steps[0].Target = s.Settings.HelldiversPath
			}
		case "orbital-close-win":
			if st.Name == defaultCloseWinZH || st.Name == defaultCloseWinEN ||
				st.Name == legacyDefaultCloseWinZH || st.Name == legacyDefaultCloseWinEN {
				if s.Settings.Language == LangEN {
					st.Name = defaultCloseWinEN
				} else {
					st.Name = defaultCloseWinZH
				}
			}
		case "resupply-files":
			if st.Name == defaultResupplyZH || st.Name == defaultResupplyEN {
				if s.Settings.Language == LangEN {
					st.Name = defaultResupplyEN
				} else {
					st.Name = defaultResupplyZH
				}
			}
			if len(st.Steps) == 1 && st.Steps[0].Kind == StepLaunchApp && strings.EqualFold(strings.TrimSpace(st.Steps[0].Target), "files") {
				st.Steps[0].Target = DefaultFileManagerTarget()
			}
		}
	}
}

type trieNode struct {
	children [4]*trieNode
	termIdx  int // index into Stratagems, or -1
}

func dirIndex(d Dir) int {
	switch d {
	case DirUp:
		return 0
	case DirDown:
		return 1
	case DirLeft:
		return 2
	case DirRight:
		return 3
	default:
		return -1
	}
}

// Validate verifies structural validity, protects the built-in system Stratagem,
// and enforces the prefix-free invariant across all enabled Stratagems.
func Validate(snap Snapshot) error {
	if !snap.Settings.TriggerMode.Valid() {
		return &ValidationError{Code: ValInvalidTriggerMode, Reason: "无效的唤出热键模式"}
	}
	if snap.Settings.HUDTheme != "" && !snap.Settings.HUDTheme.Valid() {
		return &ValidationError{Code: ValInvalidHUDTheme, Reason: "无效的面板主题"}
	}
	if snap.Settings.Language != "" && !snap.Settings.Language.Valid() {
		return &ValidationError{Code: ValInvalidLanguage, Reason: "无效的界面语言"}
	}

	seenIDs := make(map[string]bool, len(snap.Stratagems))
	root := &trieNode{termIdx: -1}
	hasSystem := false

	for i, st := range snap.Stratagems {
		if strings.TrimSpace(st.ID) == "" {
			return &ValidationError{Code: ValEmptyID, Reason: "战备 ID 不能为空"}
		}
		if seenIDs[st.ID] {
			return &ValidationError{StratagemID: st.ID, Code: ValDuplicateID, Reason: "战备 ID 重复"}
		}
		seenIDs[st.ID] = true

		if st.ID == SystemJoinHelldiversID {
			hasSystem = true
			if !st.Enabled ||
				st.Category != CategoryYellow ||
				st.Icon != SystemJoinHelldiversIcon ||
				st.Delivery != DeliveryImmediate ||
				!slices.Equal(st.Code, SystemJoinHelldiversCode) ||
				len(st.Steps) != 1 ||
				st.Steps[0].Kind != StepLaunchHelldivers {
				return &ValidationError{
					StratagemID: st.ID,
					Code:        ValSystemProtected,
					Reason:      "系统内置战备「加入绝地潜兵」受真理部保护，不可停用或更改指令",
				}
			}
		}

		if strings.TrimSpace(st.Name) == "" {
			return &ValidationError{StratagemID: st.ID, Code: ValEmptyName, Reason: "战备名称不能为空"}
		}
		if !st.Category.Valid() {
			return &ValidationError{StratagemID: st.ID, Code: ValInvalidCategory, Reason: "无效的战备分类"}
		}
		if !st.Delivery.Valid() {
			return &ValidationError{StratagemID: st.ID, Code: ValInvalidDelivery, Reason: "无效的触发投送模式"}
		}
		if len(st.Code) == 0 || len(st.Code) > 12 {
			return &ValidationError{StratagemID: st.ID, Code: ValCodeLength, Reason: "指令序列长度需在 1 到 12 步之间"}
		}
		for _, d := range st.Code {
			if !d.Valid() {
				return &ValidationError{StratagemID: st.ID, Code: ValInvalidDir, Reason: fmt.Sprintf("包含非法方向键 %q", d)}
			}
		}
		if len(st.Steps) == 0 || len(st.Steps) > 8 {
			return &ValidationError{StratagemID: st.ID, Code: ValEmptySteps, Reason: "动作步骤需在 1 到 8 步之间"}
		}
		for _, step := range st.Steps {
			if !step.Kind.Valid() {
				return &ValidationError{StratagemID: st.ID, Code: ValInvalidStepKind, Reason: fmt.Sprintf("未知动作类型 %q", step.Kind)}
			}
			if step.Kind == StepLaunchHelldivers && st.ID != SystemJoinHelldiversID {
				return &ValidationError{StratagemID: st.ID, Code: ValSystemProtected, Reason: "仅系统战备可执行绝地潜兵空投指令"}
			}
			if step.Kind == StepSystemAction {
				if !SystemAction(strings.TrimSpace(step.Target)).Valid() {
					return &ValidationError{StratagemID: st.ID, Code: ValEmptyStepTarget, Reason: "无效的系统战术动作"}
				}
				continue
			}
			if step.Kind != StepCloseActiveWindow && step.Kind != StepLaunchHelldivers && strings.TrimSpace(step.Target) == "" {
				return &ValidationError{StratagemID: st.ID, Code: ValEmptyStepTarget, Reason: "动作目标不能为空"}
			}
		}

		if !st.Enabled {
			continue
		}

		// Prefix-free Trie check among enabled Stratagems:
		cur := root
		for depth, d := range st.Code {
			idx := dirIndex(d)
			if cur.children[idx] == nil {
				cur.children[idx] = &trieNode{termIdx: -1}
			}
			cur = cur.children[idx]
			if cur.termIdx >= 0 {
				prev := snap.Stratagems[cur.termIdx]
				return &ConflictError{
					FirstID:    prev.ID,
					FirstName:  prev.Name,
					SecondID:   st.ID,
					SecondName: st.Name,
					Prefix:     slices.Clone(st.Code[:depth+1]),
				}
			}
		}
		// Check whether `st.Code` is itself a strict prefix of an already-inserted longer Stratagem:
		if hasAnyChild(cur) {
			otherIdx := firstDescendantTerm(cur)
			other := snap.Stratagems[otherIdx]
			return &ConflictError{
				FirstID:    st.ID,
				FirstName:  st.Name,
				SecondID:   other.ID,
				SecondName: other.Name,
				Prefix:     slices.Clone(st.Code),
			}
		}
		cur.termIdx = i
	}

	if !hasSystem {
		return &ValidationError{
			StratagemID: SystemJoinHelldiversID,
			Code:        ValSystemProtected,
			Reason:      "系统内置战备「加入绝地潜兵」不可移除",
		}
	}
	return nil
}

func hasAnyChild(n *trieNode) bool {
	for _, ch := range n.children {
		if ch != nil {
			return true
		}
	}
	return false
}

func firstDescendantTerm(n *trieNode) int {
	if n.termIdx >= 0 {
		return n.termIdx
	}
	for _, ch := range n.children {
		if ch != nil {
			if idx := firstDescendantTerm(ch); idx >= 0 {
				return idx
			}
		}
	}
	return 0
}

// Catalog is the single source of truth for user Settings and Stratagems.
type Catalog struct {
	path   string
	writeM sync.Mutex
	cur    atomic.Pointer[Snapshot]

	subM   sync.Mutex
	nextID uint64
	subs   map[uint64]func(Snapshot)
}

// Open loads or initializes the Catalog inside dir, migrating legacy V1 settings if present.
func Open(dir string) (*Catalog, error) {
	if strings.TrimSpace(dir) == "" {
		return nil, errors.New("catalog: empty directory path")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("catalog: create dir: %w", err)
	}

	filePath := filepath.Join(dir, "catalog.json")
	c := &Catalog{
		path: filePath,
		subs: make(map[uint64]func(Snapshot)),
	}

	snap := DefaultSnapshot()
	if raw, err := os.ReadFile(filePath); err == nil {
		var env diskEnvelope
		if jErr := json.Unmarshal(raw, &env); jErr == nil {
			candidate := Snapshot{
				Settings:   env.Settings,
				Stratagems: env.Stratagems,
			}
			migrated := ensureSystemStratagems(&candidate)
			normalizeSnapshot(&candidate)
			if len(candidate.Stratagems) > 0 && Validate(candidate) == nil {
				snap = candidate
				if migrated {
					_ = writeAtomically(filePath, snap)
				}
			}
		}
	} else if os.IsNotExist(err) {
		// Seamlessly migrate legacy V1 settings.json if present.
		if legacyRaw, lErr := os.ReadFile(filepath.Join(dir, "settings.json")); lErr == nil {
			var legacy Settings
			if json.Unmarshal(legacyRaw, &legacy) == nil {
				if legacy.TriggerMode.Valid() {
					snap.Settings.TriggerMode = legacy.TriggerMode
				}
				snap.Settings.Muted = legacy.Muted
				snap.Settings.VisorCurved = legacy.VisorCurved
			}
		}
		if wErr := writeAtomically(filePath, snap); wErr != nil {
			return nil, wErr
		}
	}

	stored := cloneSnapshot(snap)
	c.cur.Store(&stored)
	return c, nil
}

// Snapshot returns an immutable copy of the current validated state in O(1).
func (c *Catalog) Snapshot() Snapshot {
	p := c.cur.Load()
	if p == nil {
		return DefaultSnapshot()
	}
	return cloneSnapshot(*p)
}

// Update applies edit to a working copy of the Snapshot, enforces all invariants
// (including prefix-free codes and system stratagem protection), writes atomically
// to disk, and notifies subscribers.
func (c *Catalog) Update(edit func(*Snapshot) error) error {
	if edit == nil {
		return nil
	}

	c.writeM.Lock()
	draft := cloneSnapshot(*c.cur.Load())
	if err := edit(&draft); err != nil {
		c.writeM.Unlock()
		return err
	}
	normalizeSnapshot(&draft)
	if err := Validate(draft); err != nil {
		c.writeM.Unlock()
		return err
	}
	if err := writeAtomically(c.path, draft); err != nil {
		c.writeM.Unlock()
		return err
	}

	committed := cloneSnapshot(draft)
	c.cur.Store(&committed)

	c.subM.Lock()
	listeners := make([]func(Snapshot), 0, len(c.subs))
	for _, fn := range c.subs {
		listeners = append(listeners, fn)
	}
	c.subM.Unlock()
	c.writeM.Unlock()

	for _, fn := range listeners {
		fn(cloneSnapshot(committed))
	}
	return nil
}

// Subscribe registers a listener invoked whenever Update commits a new Snapshot.
func (c *Catalog) Subscribe(fn func(Snapshot)) (cancel func()) {
	if fn == nil {
		return func() {}
	}
	c.subM.Lock()
	c.nextID++
	id := c.nextID
	c.subs[id] = fn
	c.subM.Unlock()

	var once sync.Once
	return func() {
		once.Do(func() {
			c.subM.Lock()
			delete(c.subs, id)
			c.subM.Unlock()
		})
	}
}

// ExportJSON serializes the current validated Catalog Snapshot into formatted JSON.
func (c *Catalog) ExportJSON() ([]byte, error) {
	snap := c.Snapshot()
	env := diskEnvelope{
		Version:    currentSchemaVersion,
		Settings:   snap.Settings,
		Stratagems: snap.Stratagems,
	}
	return json.MarshalIndent(env, "", "  ")
}

// ImportJSON parses, normalizes, validates, and atomically applies an exported catalog JSON payload.
func (c *Catalog) ImportJSON(raw []byte) error {
	var env diskEnvelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return fmt.Errorf("JSON 解析失败: %w", err)
	}
	return c.Update(func(s *Snapshot) error {
		wasOnboarded := s.Settings.Onboarded
		s.Settings = env.Settings
		if wasOnboarded {
			s.Settings.Onboarded = true
		}
		s.Stratagems = env.Stratagems
		ensureSystemStratagems(s)
		return nil
	})
}

func writeAtomically(targetPath string, snap Snapshot) error {
	env := diskEnvelope{
		Version:    currentSchemaVersion,
		Settings:   snap.Settings,
		Stratagems: snap.Stratagems,
	}
	data, err := json.MarshalIndent(env, "", "  ")
	if err != nil {
		return fmt.Errorf("catalog: marshal: %w", err)
	}

	dir := filepath.Dir(targetPath)
	tmp, err := os.CreateTemp(dir, ".catalog-*.tmp")
	if err != nil {
		return fmt.Errorf("catalog: create temp: %w", err)
	}
	tmpName := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpName)
		}
	}()

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("catalog: write temp: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("catalog: sync temp: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("catalog: close temp: %w", err)
	}
	if err := os.Rename(tmpName, targetPath); err != nil {
		return fmt.Errorf("catalog: atomic rename: %w", err)
	}
	cleanup = false
	return nil
}
