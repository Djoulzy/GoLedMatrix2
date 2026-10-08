package client

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

func TestResizeCoverCenterCrop(t *testing.T) {
	for _, tc := range []struct {
		name           string
		bounds, center image.Rectangle
	}{
		{"landscape", image.Rect(0, 0, 6, 2), image.Rect(2, 0, 4, 2)},
		{"portrait", image.Rect(0, 0, 2, 6), image.Rect(0, 2, 2, 4)},
		{"nonzero origin", image.Rect(10, 20, 16, 22), image.Rect(12, 20, 14, 22)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source := image.NewRGBA(tc.bounds)
			draw.Draw(source, source.Bounds(), image.NewUniform(color.RGBA{R: 255, A: 255}), image.Point{}, draw.Src)
			draw.Draw(source, tc.center, image.NewUniform(color.RGBA{G: 255, A: 255}), image.Point{}, draw.Src)
			next, err := resizeCover(source, 2, 2)
			if err != nil {
				t.Fatal(err)
			}
			if next.Width != 2 || next.Height != 2 || !bytes.Equal(next.Pixels, bytes.Repeat([]byte{0, 255, 0}, 4)) {
				t.Fatalf("center crop = %+v", next)
			}
		})
	}
}

func TestPrepareImageResizesPNGAndJPEG(t *testing.T) {
	for _, format := range []string{"png", "jpeg"} {
		for _, bounds := range []image.Rectangle{image.Rect(0, 0, 6, 3), image.Rect(0, 0, 1, 4)} {
			source := image.NewRGBA(bounds)
			draw.Draw(source, bounds, image.NewUniform(color.RGBA{R: 120, G: 150, B: 180, A: 255}), image.Point{}, draw.Src)
			var encoded bytes.Buffer
			var err error
			if format == "png" {
				err = png.Encode(&encoded, source)
			} else {
				err = jpeg.Encode(&encoded, source, &jpeg.Options{Quality: 100})
			}
			if err != nil {
				t.Fatal(err)
			}
			next, err := PrepareImage(context.Background(), bytes.NewReader(encoded.Bytes()), 2, 2)
			if err != nil || next.Width != 2 || next.Height != 2 || len(next.Pixels) != 12 {
				t.Fatalf("%s %v resize = %+v, %v", format, bounds, next, err)
			}
			for offset := 0; offset < len(next.Pixels); offset += 3 {
				for channel, expected := range []byte{120, 150, 180} {
					delta := int(next.Pixels[offset+channel]) - int(expected)
					if delta < -2 || delta > 2 {
						t.Fatalf("%s distorted or padded solid color: %v", format, next.Pixels)
					}
				}
			}
		}
	}
}

func TestPrepareImageInvalidAndCancelled(t *testing.T) {
	if _, err := PrepareImage(context.Background(), bytes.NewReader([]byte("not an image")), 2, 2); err == nil {
		t.Fatal("invalid image accepted")
	}
	if _, err := PrepareImage(context.Background(), bytes.NewReader(nil), 0, 2); err == nil {
		t.Fatal("invalid target geometry accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := PrepareImage(ctx, bytes.NewReader(nil), 2, 2); err != context.Canceled {
		t.Fatalf("cancelled preparation = %v", err)
	}
	if !isHEIF([]byte("\x00\x00\x00\x18ftypheic\x00\x00\x00\x00mif1heic")) || isHEIF([]byte("an image named .heic")) {
		t.Fatal("HEIC content detection is incorrect")
	}
}

func TestPrepareActualHEIC(t *testing.T) {
	program, native, err := heicConverter()
	if err != nil {
		t.Skip("HEIC integration test requires sips or libheif")
	}
	directory := t.TempDir()
	inputPNG, inputHEIC := filepath.Join(directory, "sample.png"), filepath.Join(directory, "sample.HEIC")
	source := image.NewRGBA(image.Rect(0, 0, 64, 32))
	draw.Draw(source, source.Bounds(), image.NewUniform(color.RGBA{R: 255, A: 255}), image.Point{}, draw.Src)
	draw.Draw(source, image.Rect(16, 0, 48, 32), image.NewUniform(color.RGBA{G: 255, A: 255}), image.Point{}, draw.Src)
	file, err := os.Create(inputPNG)
	if err != nil {
		t.Fatal(err)
	}
	encodeErr := png.Encode(file, source)
	closeErr := file.Close()
	if encodeErr != nil || closeErr != nil {
		t.Fatalf("write PNG: %v, %v", encodeErr, closeErr)
	}
	var encoder *exec.Cmd
	if runtime.GOOS == "darwin" {
		encoder = exec.Command("/usr/bin/sips", "-s", "format", "heic", inputPNG, "--out", inputHEIC)
	} else {
		path, err := exec.LookPath("heif-enc")
		if err != nil {
			t.Skip("HEIC integration test requires a HEIC encoder")
		}
		encoder = exec.Command(path, inputPNG, "-o", inputHEIC)
	}
	if output, err := encoder.CombinedOutput(); err != nil {
		t.Fatalf("encode HEIC: %v: %s", err, output)
	}
	for _, useLibheif := range []bool{false, true} {
		if useLibheif {
			program, err = exec.LookPath("heif-convert")
			if err != nil {
				continue
			}
			native = false
		}
		file, err := os.Open(inputHEIC)
		if err != nil {
			t.Fatal(err)
		}
		var nextPixels []byte
		if !useLibheif {
			next, err := PrepareImage(context.Background(), file, 8, 8)
			if err != nil || next.Width != 8 || next.Height != 8 {
				t.Fatalf("prepare HEIC = %+v, %v", next, err)
			}
			nextPixels = next.Pixels
		} else {
			decoded, err := convertHEIC(context.Background(), file, program, native)
			if err != nil {
				t.Fatal(err)
			}
			next, err := resizeCover(decoded, 8, 8)
			if err != nil {
				t.Fatal(err)
			}
			nextPixels = next.Pixels
		}
		file.Close()
		// HEIC is lossy. Check the center is green, without the red side bars.
		offset := (4*8 + 4) * 3
		if nextPixels[offset] > 20 || nextPixels[offset+1] < 230 || nextPixels[offset+2] > 20 {
			t.Fatalf("HEIC center crop is not green: %v", nextPixels[offset:offset+3])
		}
	}
}
