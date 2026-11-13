//go:build !darwin && !windows

package input

func CheckTrusted() bool {
	return false
}

func (h *Hook) startPlatform() {
	h.setTrusted(false)
}
