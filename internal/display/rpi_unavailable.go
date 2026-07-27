//go:build !linux || !cgo || !rpi

package display

import "fmt"

func NewRPI(RPIConfig) (Display, error) {
	return nil, fmt.Errorf("Raspberry Pi backend is unavailable: build on Linux with CGO_ENABLED=1 and -tags rpi")
}
