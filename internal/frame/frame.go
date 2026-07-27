// Package frame defines the uncompressed pixel format exchanged over the wire.
package frame

import (
	"fmt"
	"image"
)

const (
	// PixelFormat is three bytes per pixel, row-major, from top-left: R, G, B.
	PixelFormat = "rgb24"
	MediaType   = "application/vnd.goledmatrix.rgb24"
)

// Frame is an immutable-by-convention RGB24 image.
type Frame struct {
	Width  int
	Height int
	Pixels []byte
}

func ByteLen(width, height int) (int, error) {
	if width <= 0 || height <= 0 {
		return 0, fmt.Errorf("invalid frame dimensions %dx%d", width, height)
	}
	const maxInt = int(^uint(0) >> 1)
	if width > maxInt/height/3 {
		return 0, fmt.Errorf("frame dimensions are too large")
	}
	return width * height * 3, nil
}

func New(width, height int, pixels []byte) (Frame, error) {
	want, err := ByteLen(width, height)
	if err != nil {
		return Frame{}, err
	}
	if len(pixels) != want {
		return Frame{}, fmt.Errorf("invalid RGB24 payload: got %d bytes, want %d", len(pixels), want)
	}
	return Frame{Width: width, Height: height, Pixels: pixels}, nil
}

// FromImage converts an image without resizing it.
func FromImage(src image.Image) (Frame, error) {
	bounds := src.Bounds()
	width, height := bounds.Dx(), bounds.Dy()
	size, err := ByteLen(width, height)
	if err != nil {
		return Frame{}, err
	}

	pixels := make([]byte, size)
	offset := 0
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			r, g, b, _ := src.At(x, y).RGBA()
			pixels[offset] = byte(r >> 8)
			pixels[offset+1] = byte(g >> 8)
			pixels[offset+2] = byte(b >> 8)
			offset += 3
		}
	}
	return Frame{Width: width, Height: height, Pixels: pixels}, nil
}
