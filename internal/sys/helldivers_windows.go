//go:build windows

package sys

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"
)

const (
	hkeyCurrentUser  uintptr = 0x80000001
	hkeyLocalMachine uintptr = 0x80000002
	keyQueryValue    uintptr = 0x0001
	keyWow6464Key    uintptr = 0x0100
	keyWow6432Key    uintptr = 0x0200
	regSZ            uint32  = 1
	regExpandSZ      uint32  = 2
)

var (
	advapi32            = syscall.NewLazyDLL(winSysDir + `\advapi32.dll`)
	procRegOpenKeyExW   = advapi32.NewProc("RegOpenKeyExW")
	procRegQueryValueEx = advapi32.NewProc("RegQueryValueExW")
	procRegCloseKey     = advapi32.NewProc("RegCloseKey")
)

func readWinRegString(root uintptr, subKey, valueName string, extraAccess uintptr) string {
	subPtr, err := syscall.UTF16PtrFromString(subKey)
	if err != nil {
		return ""
	}
	valPtr, err := syscall.UTF16PtrFromString(valueName)
	if err != nil {
		return ""
	}
	var hKey uintptr
	rc, _, _ := procRegOpenKeyExW.Call(
		root,
		uintptr(unsafe.Pointer(subPtr)),
		0,
		keyQueryValue|extraAccess,
		uintptr(unsafe.Pointer(&hKey)),
	)
	if rc != 0 || hKey == 0 {
		return ""
	}
	defer procRegCloseKey.Call(hKey)

	var valType uint32
	var buf [1024]uint16
	byteLen := uint32(len(buf) * 2)
	rc, _, _ = procRegQueryValueEx.Call(
		hKey,
		uintptr(unsafe.Pointer(valPtr)),
		0,
		uintptr(unsafe.Pointer(&valType)),
		uintptr(unsafe.Pointer(&buf[0])),
		uintptr(unsafe.Pointer(&byteLen)),
	)
	if rc != 0 || (valType != regSZ && valType != regExpandSZ) || byteLen < 2 {
		return ""
	}
	return strings.TrimSpace(syscall.UTF16ToString(buf[:byteLen/2]))
}

func detectHelldiversPlatform() HelldiversInstall {
	// 1. Direct Steam App 553850 Uninstall registry entry
	for _, view := range []uintptr{keyWow6464Key, keyWow6432Key, 0} {
		loc := readWinRegString(
			hkeyLocalMachine,
			`SOFTWARE\Microsoft\Windows\CurrentVersion\Uninstall\Steam App `+HelldiversSteamAppID,
			"InstallLocation",
			view,
		)
		if loc != "" {
			if exe, _, ok := ResolveCustomHelldiversPath(loc); ok {
				return HelldiversInstall{
					Found:     true,
					Source:    "steam",
					Path:      exe,
					LaunchURI: HelldiversSteamRunURI,
				}
			}
		}
	}

	// 2. Discover Steam roots via Registry + standard drive locations
	var roots []string
	if p := readWinRegString(hkeyCurrentUser, `Software\Valve\Steam`, "SteamPath", 0); p != "" {
		roots = append(roots, filepath.FromSlash(p))
	}
	if p := readWinRegString(hkeyLocalMachine, `SOFTWARE\WOW6432Node\Valve\Steam`, "InstallPath", 0); p != "" {
		roots = append(roots, p)
	}
	if p := readWinRegString(hkeyLocalMachine, `SOFTWARE\Valve\Steam`, "InstallPath", keyWow6432Key); p != "" {
		roots = append(roots, p)
	}

	if pf86 := os.Getenv("PROGRAMFILES(X86)"); pf86 != "" {
		roots = append(roots, filepath.Join(pf86, "Steam"))
	}
	if pf := os.Getenv("PROGRAMFILES"); pf != "" {
		roots = append(roots, filepath.Join(pf, "Steam"))
	}

	for drive := 'C'; drive <= 'Z'; drive++ {
		driveRoot := string(drive) + `:\`
		if st, err := os.Stat(driveRoot); err != nil || !st.IsDir() {
			continue
		}
		roots = append(
			roots,
			filepath.Join(driveRoot, "SteamLibrary"),
			filepath.Join(driveRoot, "Steam"),
			filepath.Join(driveRoot, "Games", "Steam"),
			filepath.Join(driveRoot, "Games", "SteamLibrary"),
			filepath.Join(driveRoot, "Program Files (x86)", "Steam"),
		)
	}

	if inst, ok := scanSteamRootsForHelldivers(roots); ok {
		return inst
	}

	// 3. Fallback: check MyGo Shell InstalledApps cache
	if app, ok := findInstalledApp(
		[]string{HelldiversSteamAppID, "Steam App " + HelldiversSteamAppID},
		[]string{"HELLDIVERS 2", "HELLDIVERS™ 2", "Helldivers 2", "绝地潜兵2", "绝地潜兵 2"},
	); ok && app.Path != "" {
		return HelldiversInstall{
			Found:     true,
			Source:    "steam",
			Path:      app.Path,
			LaunchURI: HelldiversSteamRunURI,
		}
	}

	return HelldiversInstall{
		Found:  false,
		Source: "none",
	}
}
