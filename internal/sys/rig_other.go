//go:build !darwin && !windows

package sys

import (
	"context"

	"hellauncher/internal/catalog"
)

func CheckTrusted() bool { return true }

func GuidePermission(_ context.Context) (bool, error) { return true, nil }

func HasConflictingBundleRunning(_ string) bool { return false }

type stubRig struct{}

func NewPlatformRig() Rig { return &stubRig{} }

func (s *stubRig) StartInput(_ func(InputEvent) bool, onTrust func(bool)) func() {
	if onTrust != nil {
		go onTrust(true)
	}
	return func() {}
}

func (s *stubRig) SetMouseCapture(_ bool) {}

func (s *stubRig) Execute(_ context.Context, _ catalog.Step, _ *Point) error {
	return nil
}
