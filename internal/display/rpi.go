//go:build (linux && cgo && rpi) || (cgo && rpistub)

package display

/*
#cgo CFLAGS: -I${SRCDIR}/../../third_party/_rpi-rgb-led-matrix/include
#cgo !rpistub LDFLAGS: -L${SRCDIR}/../../third_party/_rpi-rgb-led-matrix/lib -lrgbmatrix -lstdc++ -lm -lpthread
#include <stdlib.h>
#include <led-matrix-c.h>

static struct LedCanvas *present_rgb24(struct RGBLedMatrix *matrix,
                                      struct LedCanvas *back,
                                      const uint8_t *pixels, size_t size,
                                      int width, int height) {
  set_image(back, 0, 0, pixels, size, width, height, 0);
  // The returned canvas is now inactive; use it for the NEXT frame.
  return led_matrix_swap_on_vsync(matrix, back);
}
*/
import "C"

import (
	"context"
	"fmt"
	"sync"
	"unsafe"

	"github.com/Djoulzy/GoLedMatrix2/internal/frame"
)

type RPI struct {
	mu        sync.Mutex
	matrix    *C.struct_RGBLedMatrix
	back      *C.struct_LedCanvas
	width     int
	height    int
	frameSize int
	strings   [3]*C.char
}

func NewRPI(config RPIConfig) (Display, error) {
	mapping := C.CString(config.HardwareMapping)
	pixelMapper := C.CString(config.PixelMapperConfig)
	rgbSequence := C.CString(config.RGBSequence)
	strings := [3]*C.char{mapping, pixelMapper, rgbSequence}
	initialized := false
	defer func() {
		if !initialized {
			for _, value := range strings {
				C.free(unsafe.Pointer(value))
			}
		}
	}()
	hardware := C.struct_RGBLedMatrixOptions{
		hardware_mapping:         mapping,
		rows:                     C.int(config.Rows),
		cols:                     C.int(config.Cols),
		chain_length:             C.int(config.ChainLength),
		parallel:                 C.int(config.Parallel),
		multiplexing:             C.int(config.Multiplexing),
		pixel_mapper_config:      pixelMapper,
		brightness:               C.int(config.Brightness),
		pwm_bits:                 C.int(config.PWMBits),
		show_refresh_rate:        C.bool(config.ShowRefreshRate),
		limit_refresh_rate_hz:    C.int(config.LimitRefreshRateHz),
		scan_mode:                C.int(config.ScanMode),
		row_address_type:         C.int(config.RowAddressType),
		pwm_lsb_nanoseconds:      C.int(config.PWMLSBNanoseconds),
		pwm_dither_bits:          C.int(config.PWMDitherBits),
		disable_hardware_pulsing: C.bool(config.DisableHardwarePulsing),
		inverse_colors:           C.bool(config.InverseColors),
		led_rgb_sequence:         rgbSequence,
	}
	runtime := C.struct_RGBLedRuntimeOptions{gpio_slowdown: C.int(config.GPIOSlowdown)}
	matrix := C.led_matrix_create_from_options_and_rt_options(&hardware, &runtime)
	if matrix == nil {
		return nil, fmt.Errorf("initialize RGB matrix: native driver returned no matrix")
	}
	back := C.led_matrix_create_offscreen_canvas(matrix)
	if back == nil {
		C.led_matrix_delete(matrix)
		return nil, fmt.Errorf("initialize RGB matrix: native driver returned no offscreen canvas")
	}
	var width, height C.int
	C.led_canvas_get_size(back, &width, &height)
	frameSize, err := frame.ByteLen(int(width), int(height))
	if err != nil {
		C.led_matrix_delete(matrix)
		return nil, fmt.Errorf("initialize RGB matrix: %w", err)
	}
	// The native options may retain their string pointers for the matrix's
	// lifetime. Release them only after deleting the matrix.
	initialized = true
	return &RPI{matrix: matrix, back: back, width: int(width), height: int(height), frameSize: frameSize, strings: strings}, nil
}

func (d *RPI) Geometry() (int, int) { return d.width, d.height }

func (d *RPI) Present(ctx context.Context, next frame.Frame) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if d.matrix == nil {
		return fmt.Errorf("RGB matrix is closed")
	}
	if next.Width != d.width || next.Height != d.height {
		return fmt.Errorf("frame geometry %dx%d does not match display %dx%d", next.Width, next.Height, d.width, d.height)
	}
	if len(next.Pixels) != d.frameSize {
		return fmt.Errorf("invalid RGB24 payload: got %d bytes, want %d", len(next.Pixels), d.frameSize)
	}
	// C copies these bytes before returning; it retains no Go pointer. Always
	// retain the canvas returned by VSync, otherwise we overwrite the front.
	d.back = C.present_rgb24(d.matrix, d.back,
		(*C.uint8_t)(unsafe.Pointer(&next.Pixels[0])), C.size_t(len(next.Pixels)),
		C.int(d.width), C.int(d.height))
	return nil
}

func (d *RPI) Close() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.matrix != nil {
		C.led_matrix_delete(d.matrix)
		d.matrix, d.back = nil, nil
		for i, value := range d.strings {
			C.free(unsafe.Pointer(value))
			d.strings[i] = nil
		}
	}
	return nil
}
