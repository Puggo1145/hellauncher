package trayicon

import (
	_ "embed"
	"runtime"
)

//go:embed tray_template.png
var templatePNG []byte

//go:embed tray_color.png
var colorPNG []byte

// Icon returns the PNG bytes and whether it should be rendered as a macOS template icon.
func Icon() ([]byte, bool) {
	if runtime.GOOS == "darwin" {
		return templatePNG, true
	}
	return colorPNG, false
}
