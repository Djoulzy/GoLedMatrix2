//go:build rpi

package display

import "fmt"

// RunSimulator is deliberately excluded from hardware builds so a headless
// Raspberry Pi does not need X11, EGL or OpenGL development libraries.
func RunSimulator(
	_, _, _ int,
	_ func(Display, <-chan struct{}) error,
) error {
	return fmt.Errorf("simulation is unavailable in a binary built with -tags rpi")
}
