//go:build !darwin && !windows

package actions

import "errors"

func (e *Executor) openTargetPlatform(target string) error {
	return errors.ErrUnsupported
}

func closeActiveWindowPlatform() error {
	return errors.ErrUnsupported
}
