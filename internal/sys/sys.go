package sys

import (
	"context"
	"net/url"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/egoist/mygo"

	"hellauncher/internal/catalog"
)

// Point represents global desktop screen coordinates in logical pixels (DIPs).
type Point struct {
	X int
	Y int
}

// Mods captures normalized modifier key state across macOS and Windows.
type Mods struct {
	LeftCtrl  bool
	RightCtrl bool
	Alt       bool
	Shift     bool
	Super     bool
}

// InputKind classifies normalized OS input events delivered by a Rig.
type InputKind uint8

const (
	InputModifiers InputKind = iota
	InputKeyDown
	InputKeyUp
	InputMouseLeftDown
	InputMouseRightDown
	InputReset
)

// KeyRole maps platform keycodes / virtual keys into semantic tactical roles.
type KeyRole uint8

const (
	KeyOther KeyRole = iota
	KeyDirUp
	KeyDirDown
	KeyDirLeft
	KeyDirRight
	KeyEscape
)

// Dir converts a directional KeyRole into a catalog.Dir.
func (r KeyRole) Dir() (catalog.Dir, bool) {
	switch r {
	case KeyDirUp:
		return catalog.DirUp, true
	case KeyDirDown:
		return catalog.DirDown, true
	case KeyDirLeft:
		return catalog.DirLeft, true
	case KeyDirRight:
		return catalog.DirRight, true
	default:
		return "", false
	}
}

// InputEvent is the normalized low-latency event emitted by StartInput.
type InputEvent struct {
	At      time.Time
	Kind    InputKind
	Mods    Mods
	KeyCode uint16
	Role    KeyRole
	Repeat  bool
	Point   Point
}

// Rig is the OS Input & Desktop Execution Seam.
//
// Invariants:
//   - StartInput's onEvent callback runs inline on the OS hook thread and MUST
//     return within microseconds without blocking.
//   - SetMouseCapture(false) keeps global mouse hooks disabled at the OS level
//     so regular desktop clicks incur zero IPC/hook overhead.
//   - Execute runs desktop/window operations synchronously for its caller and
//     must be invoked off the UI / hook thread.
type Rig interface {
	StartInput(
		onEvent func(InputEvent) (swallow bool),
		onTrust func(trusted bool),
	) (stop func())
	SetMouseCapture(enabled bool)
	Execute(ctx context.Context, step catalog.Step, beacon *Point) error
}

var (
	appsCacheMu  sync.Mutex
	appsCacheVal []mygo.InstalledApp
	appsCacheExp time.Time
)

// InstalledApps returns installed desktop applications with a short TTL cache.
// Call from a background goroutine to avoid disk I/O on the UI thread.
func InstalledApps() []mygo.InstalledApp {
	appsCacheMu.Lock()
	if time.Now().Before(appsCacheExp) && len(appsCacheVal) > 0 {
		cached := slices.Clone(appsCacheVal)
		appsCacheMu.Unlock()
		return cached
	}
	appsCacheMu.Unlock()

	var apps []mygo.InstalledApp
	func() {
		defer func() { _ = recover() }()
		found, err := mygo.Shell.InstalledApps()
		if err == nil {
			apps = found
		}
	}()

	if runtime.GOOS == "windows" {
		hasExplorer := false
		for _, a := range apps {
			if strings.EqualFold(a.ID, "explorer.exe") || strings.EqualFold(filepath.Base(a.Path), "explorer.exe") {
				hasExplorer = true
				break
			}
		}
		if !hasExplorer {
			apps = append([]mygo.InstalledApp{
				{ID: "explorer.exe", Name: "Windows Explorer", Path: "explorer.exe"},
			}, apps...)
		}
	}

	if len(apps) > 0 {
		appsCacheMu.Lock()
		appsCacheVal = slices.Clone(apps)
		appsCacheExp = time.Now().Add(30 * time.Second)
		appsCacheMu.Unlock()
	}
	return apps
}

func findInstalledApp(ids []string, names []string) (mygo.InstalledApp, bool) {
	apps := InstalledApps()
	if len(apps) == 0 {
		return mygo.InstalledApp{}, false
	}
	for _, wantID := range ids {
		if wantID == "" {
			continue
		}
		for _, a := range apps {
			if strings.EqualFold(a.ID, wantID) || strings.EqualFold(a.Path, wantID) {
				return a, true
			}
		}
	}
	for _, wantName := range names {
		if wantName == "" {
			continue
		}
		for _, a := range apps {
			if strings.EqualFold(a.Name, wantName) {
				return a, true
			}
		}
	}
	return mygo.InstalledApp{}, false
}

// ExpandDynamicTokens substitutes {clipboard} and {clipboard_raw} placeholders
// using the current OS text clipboard when present in raw.
func ExpandDynamicTokens(raw string, allowURLEncode bool) string {
	if !strings.Contains(raw, "{clipboard") {
		return raw
	}
	var clip string
	func() {
		defer func() { _ = recover() }()
		clip = mygo.Clipboard.ReadText()
	}()
	return ExpandDynamicTokensWith(raw, clip, allowURLEncode)
}

// ExpandDynamicTokensWith expands {clipboard} and {clipboard_raw} with an explicit
// clipboard value (exposed for deterministic unit tests).
func ExpandDynamicTokensWith(raw string, clipText string, allowURLEncode bool) string {
	if !strings.Contains(raw, "{clipboard") {
		return raw
	}
	clean := strings.TrimSpace(clipText)
	out := strings.ReplaceAll(raw, "{clipboard_raw}", clean)
	if !strings.Contains(out, "{clipboard}") {
		return out
	}
	templateWithoutToken := strings.ReplaceAll(out, "{clipboard}", "")
	if allowURLEncode && strings.Contains(templateWithoutToken, "://") {
		return strings.ReplaceAll(out, "{clipboard}", url.QueryEscape(clean))
	}
	return strings.ReplaceAll(out, "{clipboard}", clean)
}
