//go:build !windows

package hud

import (
	"runtime"

	"github.com/egoist/mygo"
)

func configureOverlayWindow(*mygo.Window) {}

func setOverlayBackdrop(win *mygo.Window, classic bool) {
	if win == nil || runtime.GOOS != "darwin" {
		return
	}
	if classic {
		win.SetVibrancy(mygo.VibrancyHUD)
	} else {
		win.SetVibrancy(mygo.VibrancyNone)
	}
}

func captureWindowBackdrop(*mygo.Window, int) *backdropCapture {
	return nil
}
