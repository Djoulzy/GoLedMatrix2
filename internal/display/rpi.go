//go:build linux && cgo && rpi

package display

import (
	"context"
	"fmt"
	"image/color"

	"github.com/Djoulzy/GoLedMatrix2/internal/frame"
	rgbmatrix "github.com/zaggash/go-rpi-rgb-led-matrix"
)

type RPI struct {
	matrix rgbmatrix.Matrix
	width  int
	height int
}

func NewRPI(config RPIConfig) (Display, error) {
	hardware := &rgbmatrix.HardwareConfig{
		GPIOMapping:            config.HardwareMapping,
		Rows:                   config.Rows,
		Cols:                   config.Cols,
		ChainLength:            config.ChainLength,
		Parallel:               config.Parallel,
		Multiplexing:           config.Multiplexing,
		PixelMapperConfig:      config.PixelMapperConfig,
		Brightness:             config.Brightness,
		PWMBits:                config.PWMBits,
		ShowRefreshRate:        config.ShowRefreshRate,
		LimitRefresh:           config.LimitRefreshRateHz,
		ScanMode:               rgbmatrix.ScanMode(config.ScanMode),
		PWMLSBNanoseconds:      config.PWMLSBNanoseconds,
		PWMDitherBits:          config.PWMDitherBits,
		DisableHardwarePulsing: config.DisableHardwarePulsing,
		InverseColors:          config.InverseColors,
		RGBSequence:            "RGB",
	}
	runtime := &rgbmatrix.RuntimeConfig{GPIOSlowdown: config.GPIOSlowdown}
	matrix, err := rgbmatrix.NewRGBLedMatrix(hardware, runtime)
	if err != nil {
		return nil, fmt.Errorf("initialize RGB matrix: %w", err)
	}
	width, height := matrix.Geometry()
	return &RPI{matrix: matrix, width: width, height: height}, nil
}

func (d *RPI) Geometry() (int, int) { return d.width, d.height }

func (d *RPI) Present(_ context.Context, next frame.Frame) error {
	if next.Width != d.width || next.Height != d.height {
		return fmt.Errorf("frame geometry %dx%d does not match display %dx%d", next.Width, next.Height, d.width, d.height)
	}
	for position, offset := 0, 0; offset < len(next.Pixels); position, offset = position+1, offset+3 {
		d.matrix.Set(position, color.RGBA{
			R: next.Pixels[offset],
			G: next.Pixels[offset+1],
			B: next.Pixels[offset+2],
			A: 0xff,
		})
	}
	return d.matrix.Render()
}

func (d *RPI) Close() error { return d.matrix.Close() }
