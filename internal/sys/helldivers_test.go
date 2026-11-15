package sys

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSteamLibraryVdfAndManifestProbe(t *testing.T) {
	tmp := t.TempDir()
	libB := filepath.Join(tmp, "SteamLibB")

	vdf := `"libraryfolders"
{
	"0"
	{
		"path"		"C:\\Program Files (x86)\\Steam"
	}
	"1"
	{
		"path"		"` + filepath.ToSlash(libB) + `"
	}
}`
	libs := ParseSteamLibraryFolders(vdf)
	if len(libs) != 2 {
		t.Fatalf("expected 2 parsed library folders, got %v", libs)
	}

	// Create mock Steam library with appmanifest_553850.acf and bin/helldivers2.exe
	steamApps := filepath.Join(libB, "steamapps")
	binDir := filepath.Join(steamApps, "common", "Helldivers 2", "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	acf := `"AppState"
{
	"appid"		"553850"
	"name"		"HELLDIVERS™ 2"
	"installdir"		"Helldivers 2"
}`
	if err := os.WriteFile(filepath.Join(steamApps, "appmanifest_553850.acf"), []byte(acf), 0o644); err != nil {
		t.Fatal(err)
	}
	exePath := filepath.Join(binDir, "helldivers2.exe")
	if err := os.WriteFile(exePath, []byte("MZ"), 0o644); err != nil {
		t.Fatal(err)
	}

	gotExe, ok := ProbeSteamLibraryForHelldivers(libB)
	if !ok || gotExe != exePath {
		t.Fatalf("expected ProbeSteamLibraryForHelldivers to find %q, got ok=%v path=%q", exePath, ok, gotExe)
	}

	// Also verify DetectHelldivers resolves both game root dir and direct executable
	byDir := DetectHelldivers(filepath.Join(steamApps, "common", "Helldivers 2"))
	if !byDir.Found || byDir.Source != "custom" || byDir.Path != exePath {
		t.Fatalf("expected directory custom path to resolve exe, got %+v", byDir)
	}

	byExe := DetectHelldivers(exePath)
	if !byExe.Found || byExe.Source != "custom" || byExe.Path != exePath {
		t.Fatalf("expected direct exe custom path to resolve, got %+v", byExe)
	}

	// Invalid custom path reports CustomInvalid=true and Found=false
	missing := DetectHelldivers(filepath.Join(tmp, "nonexistent", "helldivers2.exe"))
	if missing.Found || !missing.CustomInvalid {
		t.Fatalf("expected CustomInvalid=true for missing custom path, got %+v", missing)
	}
}
