// Package display isolates the HTTP server from the physical matrix driver.
package display

import (
	"context"
	"fmt"

	"github.com/Djoulzy/GoLedMatrix2/internal/frame"
)

type Display interface {
	Geometry() (width, height int)
	Present(context.Context, frame.Frame) error
	Brightness() int
	SetBrightness(context.Context, int) error
	Close() error
}

func ValidateBrightness(value int) error {
	if value < 1 || value > 100 {
		return fmt.Errorf("brightness must be between 1 and 100")
	}
	return nil
}

func dimPixel(value byte, brightness int) byte {
	return byte((int(value)*brightness + 50) / 100)
}
