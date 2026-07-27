// Package display isolates the HTTP server from the physical matrix driver.
package display

import (
	"context"

	"github.com/Djoulzy/GoLedMatrix2/internal/frame"
)

type Display interface {
	Geometry() (width, height int)
	Present(context.Context, frame.Frame) error
	Close() error
}
