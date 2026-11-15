package sys

import (
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

const (
	HelldiversSteamAppID    = "553850"
	HelldiversSteamRunURI   = "steam://rungameid/553850"
	HelldiversSteamStoreURL = "https://store.steampowered.com/app/553850/HELLDIVERS_2/"
)

// ErrHelldiversMissing indicates HELLDIVERS™ 2 could not be located on disk.
var ErrHelldiversMissing = errors.New("sys: helldivers 2 installation not found")

// HelldiversInstall describes how HELLDIVERS™ 2 was resolved on the host machine.
type HelldiversInstall struct {
	Found         bool
	Source        string // "steam", "custom", or "none"
	Path          string // Resolved executable, shortcut, or bundle path
	LaunchURI     string // Optional URI such as steam://rungameid/553850
	CustomPath    string // User-specified override path, if any
	CustomInvalid bool   // True if CustomPath was set by user but does not exist
}

var (
	vdfPathRe    = regexp.MustCompile(`(?i)"path"\s+"([^"]+)"`)
	acfInstallRe = regexp.MustCompile(`(?i)"installdir"\s+"([^"]+)"`)
)

// ParseSteamLibraryFolders extracts Steam library root directories from libraryfolders.vdf text.
func ParseSteamLibraryFolders(vdf string) []string {
	matches := vdfPathRe.FindAllStringSubmatch(vdf, -1)
	out := make([]string, 0, len(matches))
	seen := make(map[string]bool, len(matches))
	for _, m := range matches {
		if len(m) < 2 {
			continue
		}
		raw := strings.ReplaceAll(strings.TrimSpace(m[1]), `\\`, `\`)
		if raw == "" {
			continue
		}
		cleaned := filepath.Clean(raw)
		key := strings.ToLower(cleaned)
		if !seen[key] {
			seen[key] = true
			out = append(out, cleaned)
		}
	}
	return out
}

// ParseSteamAppInstallDir extracts "installdir" from an appmanifest_*.acf file,
// defaulting to "Helldivers 2".
func ParseSteamAppInstallDir(acf string) string {
	if m := acfInstallRe.FindStringSubmatch(acf); len(m) >= 2 {
		dir := strings.ReplaceAll(strings.TrimSpace(m[1]), `\\`, `\`)
		if dir != "" {
			return dir
		}
	}
	return "Helldivers 2"
}

// ProbeSteamLibraryForHelldivers checks whether a Steam library root contains HELLDIVERS™ 2 (App 553850).
func ProbeSteamLibraryForHelldivers(libRoot string) (string, bool) {
	if strings.TrimSpace(libRoot) == "" {
		return "", false
	}
	steamApps := filepath.Join(libRoot, "steamapps")
	installFolder := "Helldivers 2"

	acfPath := filepath.Join(steamApps, "appmanifest_"+HelldiversSteamAppID+".acf")
	if raw, err := os.ReadFile(acfPath); err == nil {
		installFolder = ParseSteamAppInstallDir(string(raw))
	}

	gameRoot := filepath.Join(steamApps, "common", installFolder)
	candidates := []string{
		filepath.Join(gameRoot, "bin", "helldivers2.exe"),
		filepath.Join(gameRoot, "helldivers2.exe"),
	}
	for _, cand := range candidates {
		if info, err := os.Stat(cand); err == nil && !info.IsDir() {
			return cand, true
		}
	}
	return "", false
}

// ResolveCustomHelldiversPath validates a user-chosen file, shortcut, app bundle,
// directory, or steam:// URI for launching HELLDIVERS™ 2.
func ResolveCustomHelldiversPath(raw string) (resolved string, launchURI string, ok bool) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", "", false
	}
	expanded := expandPortablePath(trimmed)
	lower := strings.ToLower(expanded)
	if strings.HasPrefix(lower, "steam://") {
		return expanded, expanded, true
	}

	info, err := os.Stat(expanded)
	if err != nil {
		return "", "", false
	}
	if !info.IsDir() {
		return expanded, "", true
	}
	if strings.HasSuffix(lower, ".app") {
		return expanded, "", true
	}

	// User picked a directory (e.g. game root, bin folder, or Steam library root):
	subCandidates := []string{
		filepath.Join(expanded, "bin", "helldivers2.exe"),
		filepath.Join(expanded, "helldivers2.exe"),
	}
	for _, c := range subCandidates {
		if st, sErr := os.Stat(c); sErr == nil && !st.IsDir() {
			return c, "", true
		}
	}
	if exe, found := ProbeSteamLibraryForHelldivers(expanded); found {
		return exe, HelldiversSteamRunURI, true
	}
	return "", "", false
}

// DetectHelldivers resolves HELLDIVERS™ 2 using customPath when provided, or
// searches Steam libraries and installed applications automatically when empty.
func DetectHelldivers(customPath string) HelldiversInstall {
	trimmed := strings.TrimSpace(customPath)
	if trimmed != "" {
		if resolved, uri, ok := ResolveCustomHelldiversPath(trimmed); ok {
			return HelldiversInstall{
				Found:      true,
				Source:     "custom",
				Path:       resolved,
				LaunchURI:  uri,
				CustomPath: trimmed,
			}
		}
		return HelldiversInstall{
			Found:         false,
			Source:        "custom",
			CustomPath:    trimmed,
			CustomInvalid: true,
		}
	}
	return detectHelldiversPlatform()
}

func expandPortablePath(raw string) string {
	s := strings.TrimSpace(raw)
	if s == "~" || strings.HasPrefix(s, "~/") || strings.HasPrefix(s, `~\`) {
		if home, err := os.UserHomeDir(); err == nil && home != "" {
			if s == "~" {
				s = home
			} else {
				s = filepath.Join(home, s[2:])
			}
		}
	}
	if strings.Contains(s, "%") {
		var b strings.Builder
		b.Grow(len(s))
		for i := 0; i < len(s); {
			if s[i] == '%' {
				if end := strings.IndexByte(s[i+1:], '%'); end > 0 {
					if val := os.Getenv(s[i+1 : i+1+end]); val != "" {
						b.WriteString(val)
						i += end + 2
						continue
					}
				}
			}
			b.WriteByte(s[i])
			i++
		}
		s = b.String()
	}
	return os.ExpandEnv(s)
}

func scanSteamRootsForHelldivers(roots []string) (HelldiversInstall, bool) {
	libs := make([]string, 0, len(roots)*2)
	seen := make(map[string]bool, len(roots)*2)
	addLib := func(p string) {
		p = strings.TrimSpace(p)
		if p == "" {
			return
		}
		clean := filepath.Clean(p)
		key := strings.ToLower(clean)
		if !seen[key] {
			seen[key] = true
			libs = append(libs, clean)
		}
	}

	for _, r := range roots {
		addLib(r)
		vdfPath := filepath.Join(r, "steamapps", "libraryfolders.vdf")
		if raw, err := os.ReadFile(vdfPath); err == nil {
			for _, extra := range ParseSteamLibraryFolders(string(raw)) {
				addLib(extra)
			}
		}
	}

	for _, lib := range libs {
		if exe, ok := ProbeSteamLibraryForHelldivers(lib); ok {
			return HelldiversInstall{
				Found:     true,
				Source:    "steam",
				Path:      exe,
				LaunchURI: HelldiversSteamRunURI,
			}, true
		}
	}
	return HelldiversInstall{}, false
}
