package actions

import (
	"fmt"
	"log"
	"strings"
	"sync"

	"github.com/egoist/mygo"
)

// ActionKind defines the V1 desktop operation executed when a Stratagem code completes.
type ActionKind string

const (
	ActionOpenApp           ActionKind = "open_app"
	ActionCloseActiveWindow ActionKind = "close_active_window"
)

// Stratagem represents a tactical shortcut entry shown in the HUD.
type Stratagem struct {
	ID       string     `json:"id"`
	Name     string     `json:"name"`
	Subtitle string     `json:"subtitle"`
	Category string     `json:"category"` // "blue" | "red" | "yellow" | "green"
	Icon     string     `json:"icon"`
	Code     []string   `json:"code"` // Sequence of "U", "D", "L", "R"
	Kind     ActionKind `json:"kind"`
	Target   string     `json:"target"`
}

// DefaultStratagems returns the V1 prefix-free Stratagem loadout.
func DefaultStratagems() []Stratagem {
	return []Stratagem{
		{
			ID:       "open-chrome",
			Name:     "Google Chrome",
			Subtitle: "启动并切换至浏览器",
			Category: "blue",
			Icon:     "tactical-chrome",
			Code:     []string{"U", "D", "U"}, // ⬆️⬇️⬆️ (WSW)
			Kind:     ActionOpenApp,
			Target:   "chrome",
		},
		{
			ID:       "open-zed",
			Name:     "Zed Editor",
			Subtitle: "部署极速代码编辑器",
			Category: "blue",
			Icon:     "tactical-zed",
			Code:     []string{"U", "R", "D", "D", "U"}, // ⬆️➡️⬇️⬇️⬆️ (WDSSW)
			Kind:     ActionOpenApp,
			Target:   "zed",
		},
		{
			ID:       "close-active-win",
			Name:     "“飞鹰”清场：关闭窗口",
			Subtitle: "关闭当前最前端聚焦窗口",
			Category: "red",
			Icon:     "eagle-500kg.svg",
			Code:     []string{"U", "R", "D", "D", "D"}, // ⬆️➡️⬇️⬇️⬇️ (WDSSS)
			Kind:     ActionCloseActiveWindow,
			Target:   "active",
		},
		{
			ID:       "open-terminal",
			Name:     "战术终端 Terminal",
			Subtitle: "呼叫命令行终端",
			Category: "green",
			Icon:     "tactical-terminal",
			Code:     []string{"D", "D", "U", "R"}, // ⬇️⬇️⬆️➡️ (SSWD)
			Kind:     ActionOpenApp,
			Target:   "terminal",
		},
		{
			ID:       "open-files",
			Name:     "文件补给箱 Finder",
			Subtitle: "打开访达 / 资源管理器",
			Category: "yellow",
			Icon:     "resupply.svg",
			Code:     []string{"L", "D", "R", "U"}, // ⬅️⬇️➡️⬆️ (ASDW)
			Kind:     ActionOpenApp,
			Target:   "files",
		},
	}
}

// Executor resolves installed OS applications and runs Stratagem actions
// completely off the main UI thread so HUD animations never stutter.
type Executor struct {
	mu         sync.RWMutex
	apps       []mygo.InstalledApp
	OverrideFn func(Stratagem) error
}

func NewExecutor() *Executor {
	return &Executor{}
}

// RefreshApps scans installed system applications in the background.
func (e *Executor) RefreshApps() {
	go func() {
		apps, err := mygo.Shell.InstalledApps()
		if err != nil {
			log.Printf("[actions] InstalledApps scan failed: %v", err)
			return
		}
		e.mu.Lock()
		e.apps = apps
		e.mu.Unlock()
		log.Printf("[actions] indexed %d installed system applications", len(apps))
	}()
}

func (e *Executor) findInstalledApp(idCandidates []string, nameCandidates []string) (mygo.InstalledApp, bool) {
	e.mu.RLock()
	apps := e.apps
	e.mu.RUnlock()

	for _, wantID := range idCandidates {
		wantLower := strings.ToLower(wantID)
		for _, a := range apps {
			if strings.ToLower(a.ID) == wantLower {
				return a, true
			}
		}
	}
	for _, wantName := range nameCandidates {
		wantLower := strings.ToLower(wantName)
		for _, a := range apps {
			if strings.ToLower(a.Name) == wantLower {
				return a, true
			}
		}
	}
	for _, wantName := range nameCandidates {
		wantLower := strings.ToLower(wantName)
		for _, a := range apps {
			if strings.Contains(strings.ToLower(a.Name), wantLower) {
				return a, true
			}
		}
	}
	return mygo.InstalledApp{}, false
}

// Execute runs a matched Stratagem asynchronously on a background goroutine
// and returns once the action completes (e.g. target window appears on screen).
func (e *Executor) Execute(s Stratagem) error {
	if e.OverrideFn != nil {
		return e.OverrideFn(s)
	}
	switch s.Kind {
	case ActionOpenApp:
		return e.openTargetPlatform(s.Target)
	case ActionCloseActiveWindow:
		return closeActiveWindowPlatform()
	default:
		return fmt.Errorf("unsupported action kind: %s", s.Kind)
	}
}
