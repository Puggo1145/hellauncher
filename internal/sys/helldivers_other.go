//go:build !windows

package sys

import (
	"os"
	"path/filepath"
	"runtime"
)

func detectHelldiversPlatform() HelldiversInstall {
	home, _ := os.UserHomeDir()

	if runtime.GOOS == "darwin" {
		appBundles := []string{
			"/Applications/HELLDIVERS 2.app",
		}
		if home != "" {
			appBundles = append(appBundles, filepath.Join(home, "Applications", "HELLDIVERS 2.app"))
		}
		for _, bundle := range appBundles {
			if st, err := os.Stat(bundle); err == nil && st.IsDir() {
				return HelldiversInstall{
					Found:  true,
					Source: "steam",
					Path:   bundle,
				}
			}
		}
	}

	var roots []string
	if home != "" {
		roots = append(
			roots,
			filepath.Join(home, "Library", "Application Support", "Steam"),
			filepath.Join(home, ".steam", "steam"),
			filepath.Join(home, ".local", "share", "Steam"),
		)
		if bottles, err := filepath.Glob(filepath.Join(home, "Library", "Application Support", "CrossOver", "Bottles", "*", "drive_c", "Program Files (x86)", "Steam")); err == nil {
			roots = append(roots, bottles...)
		}
	}

	if inst, ok := scanSteamRootsForHelldivers(roots); ok {
		return inst
	}

	return HelldiversInstall{
		Found:  false,
		Source: "none",
	}
}
