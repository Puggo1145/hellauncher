//go:build !windows

package onboarding

import (
	"image"

	"github.com/egoist/mygo"
)

func configureTranslucentWindow(*mygo.Window, bool) {}

func (m *Manager) WindowScreenRectForTest() (winRectOut, monRectOut image.Rectangle, ok bool) {
	return image.Rectangle{}, image.Rectangle{}, false
}
