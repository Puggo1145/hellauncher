package catalog

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
)

const currentSchemaVersion = 1

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

// StepKind identifies one executable OS operation inside a Stratagem pipeline.
type StepKind string

const (
	StepLaunchApp         StepKind = "launch_app"
	StepCloseActiveWindow StepKind = "close_active_window"
	StepOpenTarget        StepKind = "open_target"
	StepRunShortcut       StepKind = "run_shortcut"
	StepShellCommand      StepKind = "shell_command"
)

func (k StepKind) Valid() bool {
	switch k {
	case StepLaunchApp, StepCloseActiveWindow, StepOpenTarget, StepRunShortcut, StepShellCommand:
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
	TriggerMode TriggerMode `json:"triggerMode"`
	Muted       bool        `json:"muted"`
	VisorCurved bool        `json:"visorCurved"`
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

func (e *ConflictError) Error() string {
	var arrows strings.Builder
	for _, d := range e.Prefix {
		arrows.WriteString(d.Arrow())
	}
	return fmt.Sprintf("指令序列冲突：「%s」与「%s」在前缀 [%s] 上重叠", e.FirstName, e.SecondName, arrows.String())
}

// ValidationError reports an invalid Stratagem or Settings field.
type ValidationError struct {
	StratagemID string
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

// DefaultSettings returns the out-of-the-box user preferences.
func DefaultSettings() Settings {
	return Settings{
		TriggerMode: ModeDoubleHoldCtrl,
		Muted:       false,
		VisorCurved: true,
	}
}

// DefaultStratagems returns the default prefix-free Stratagem loadout.
func DefaultStratagems() []Stratagem {
	return []Stratagem{
		{
			ID:       "open-chrome",
			Name:     "Google Chrome",
			Subtitle: "启动并切换至浏览器",
			Category: CategoryBlue,
			Icon:     "tactical-chrome",
			Enabled:  true,
			Code:     []Dir{DirUp, DirDown, DirUp}, // ↑↓↑ (WSW)
			Delivery: DeliveryImmediate,
			Steps:    []Step{{Kind: StepLaunchApp, Target: "chrome"}},
		},
		{
			ID:       "open-zed",
			Name:     "Zed Editor",
			Subtitle: "部署极速代码编辑器",
			Category: CategoryBlue,
			Icon:     "tactical-zed",
			Enabled:  true,
			Code:     []Dir{DirUp, DirRight, DirDown, DirDown, DirUp}, // ↑→↓↓↑ (WDSSW)
			Delivery: DeliveryImmediate,
			Steps:    []Step{{Kind: StepLaunchApp, Target: "zed"}},
		},
		{
			ID:       "close-active-win",
			Name:     "“飞鹰”清场：关闭窗口",
			Subtitle: "关闭当前最前端聚焦窗口",
			Category: CategoryRed,
			Icon:     "eagle-500kg.svg",
			Enabled:  true,
			Code:     []Dir{DirUp, DirRight, DirDown, DirDown, DirDown}, // ↑→↓↓↓ (WDSSS)
			Delivery: DeliveryImmediate,
			Steps:    []Step{{Kind: StepCloseActiveWindow}},
		},
		{
			ID:       "open-terminal",
			Name:     "战术终端 Terminal",
			Subtitle: "呼叫命令行终端",
			Category: CategoryGreen,
			Icon:     "tactical-terminal",
			Enabled:  true,
			Code:     []Dir{DirDown, DirDown, DirUp, DirRight}, // ↓↓↑→ (SSWD)
			Delivery: DeliveryImmediate,
			Steps:    []Step{{Kind: StepLaunchApp, Target: "terminal"}},
		},
		{
			ID:       "open-files",
			Name:     "文件补给箱 Finder",
			Subtitle: "打开访达 / 资源管理器",
			Category: CategoryYellow,
			Icon:     "resupply.svg",
			Enabled:  true,
			Code:     []Dir{DirLeft, DirDown, DirRight, DirUp}, // ←↓→↑ (ASDW)
			Delivery: DeliveryImmediate,
			Steps:    []Step{{Kind: StepLaunchApp, Target: "files"}},
		},
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

func normalizeSnapshot(s *Snapshot) {
	if !s.Settings.TriggerMode.Valid() {
		s.Settings.TriggerMode = ModeDoubleHoldCtrl
	}
	for i := range s.Stratagems {
		st := &s.Stratagems[i]
		st.ID = strings.TrimSpace(st.ID)
		st.Name = strings.TrimSpace(st.Name)
		st.Subtitle = strings.TrimSpace(st.Subtitle)
		if !st.Category.Valid() {
			st.Category = CategoryBlue
		}
		if !st.Delivery.Valid() {
			st.Delivery = DeliveryImmediate
		}
		if st.Icon == "" {
			st.Icon = "orbital-precision.svg"
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

// Validate verifies structural validity and enforces the prefix-free invariant
// across all enabled Stratagems.
func Validate(snap Snapshot) error {
	if !snap.Settings.TriggerMode.Valid() {
		return &ValidationError{Reason: "无效的唤出热键模式"}
	}

	seenIDs := make(map[string]bool, len(snap.Stratagems))
	root := &trieNode{termIdx: -1}

	for i, st := range snap.Stratagems {
		if strings.TrimSpace(st.ID) == "" {
			return &ValidationError{Reason: "战备 ID 不能为空"}
		}
		if seenIDs[st.ID] {
			return &ValidationError{StratagemID: st.ID, Reason: "战备 ID 重复"}
		}
		seenIDs[st.ID] = true

		if strings.TrimSpace(st.Name) == "" {
			return &ValidationError{StratagemID: st.ID, Reason: "战备名称不能为空"}
		}
		if !st.Category.Valid() {
			return &ValidationError{StratagemID: st.ID, Reason: "无效的战备分类"}
		}
		if !st.Delivery.Valid() {
			return &ValidationError{StratagemID: st.ID, Reason: "无效的触发投送模式"}
		}
		if len(st.Code) == 0 || len(st.Code) > 12 {
			return &ValidationError{StratagemID: st.ID, Reason: "指令序列长度需在 1 到 12 步之间"}
		}
		for _, d := range st.Code {
			if !d.Valid() {
				return &ValidationError{StratagemID: st.ID, Reason: fmt.Sprintf("包含非法方向键 %q", d)}
			}
		}
		if len(st.Steps) == 0 {
			return &ValidationError{StratagemID: st.ID, Reason: "至少需要配置一个执行动作"}
		}
		for _, step := range st.Steps {
			if !step.Kind.Valid() {
				return &ValidationError{StratagemID: st.ID, Reason: fmt.Sprintf("未知动作类型 %q", step.Kind)}
			}
			if step.Kind != StepCloseActiveWindow && strings.TrimSpace(step.Target) == "" {
				return &ValidationError{StratagemID: st.ID, Reason: "动作目标不能为空"}
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
			normalizeSnapshot(&candidate)
			if len(candidate.Stratagems) > 0 && Validate(candidate) == nil {
				snap = candidate
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
// (including prefix-free codes), writes atomically to disk, and notifies subscribers.
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
