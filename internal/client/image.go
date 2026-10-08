package client

import (
	"context"
	"fmt"
	"image"
	"image/draw"
	_ "image/jpeg"
	_ "image/png"
	"io"

	"github.com/Djoulzy/GoLedMatrix2/internal/frame"
	xdraw "golang.org/x/image/draw"
	"golang.org/x/image/math/f64"
)

const maxImagePixels = 64_000_000

// PrepareImage decodes a PNG, JPEG or HEIC and fills the matrix without changing
// its aspect ratio. Any overflow is cropped equally around the image center.
func PrepareImage(ctx context.Context, reader io.ReadSeeker, width, height int) (frame.Frame, error) {
	if _, err := frame.ByteLen(width, height); err != nil {
		return frame.Frame{}, err
	}
	if err := ctx.Err(); err != nil {
		return frame.Frame{}, err
	}
	var header [64]byte
	n, _ := io.ReadFull(reader, header[:])
	if _, err := reader.Seek(0, io.SeekStart); err != nil {
		return frame.Frame{}, fmt.Errorf("rewind image: %w", err)
	}
	var source image.Image
	var err error
	if isHEIF(header[:n]) {
		source, err = decodeHEIC(ctx, reader)
	} else {
		source, err = decodeStillImage(reader)
	}
	if err != nil {
		return frame.Frame{}, err
	}
	if err := ctx.Err(); err != nil {
		return frame.Frame{}, err
	}
	return resizeCover(source, width, height)
}

func decodeStillImage(reader io.ReadSeeker) (image.Image, error) {
	config, format, err := image.DecodeConfig(reader)
	if err != nil {
		return nil, fmt.Errorf("decode image (PNG, JPEG or HEIC expected): %w", err)
	}
	if format != "png" && format != "jpeg" {
		return nil, fmt.Errorf("unsupported still image format %q (want PNG, JPEG or HEIC)", format)
	}
	if config.Width <= 0 || config.Height <= 0 || config.Width > maxImagePixels/config.Height {
		return nil, fmt.Errorf("image dimensions exceed the %d megapixel limit", maxImagePixels/1_000_000)
	}
	if _, err := reader.Seek(0, io.SeekStart); err != nil {
		return nil, fmt.Errorf("rewind image: %w", err)
	}
	source, _, err := image.Decode(reader)
	if err != nil {
		return nil, fmt.Errorf("decode image: %w", err)
	}
	return source, nil
}

func resizeCover(source image.Image, width, height int) (frame.Frame, error) {
	if _, err := frame.ByteLen(width, height); err != nil {
		return frame.Frame{}, err
	}
	bounds := source.Bounds()
	if bounds.Empty() {
		return frame.Frame{}, fmt.Errorf("source image is empty")
	}
	scale := max(float64(width)/float64(bounds.Dx()), float64(height)/float64(bounds.Dy()))
	left := (float64(width)-float64(bounds.Dx())*scale)/2 - float64(bounds.Min.X)*scale
	top := (float64(height)-float64(bounds.Dy())*scale)/2 - float64(bounds.Min.Y)*scale
	canvas := image.NewRGBA(image.Rect(0, 0, width, height))
	// Transform writes directly into the final canvas; even an extreme aspect
	// ratio does not require allocating a huge intermediate resized image.
	xdraw.CatmullRom.Transform(canvas, f64.Aff3{scale, 0, left, 0, scale, top}, source, bounds, draw.Over, nil)
	return frame.FromImage(canvas)
}
