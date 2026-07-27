package frame

import (
	"image"
	"image/color"
	"testing"
)

func TestFromImageUsesRGB24AndNonZeroBounds(t *testing.T) {
	src := image.NewRGBA(image.Rect(10, 20, 12, 21))
	src.Set(10, 20, color.RGBA{R: 1, G: 2, B: 3, A: 255})
	src.Set(11, 20, color.RGBA{R: 4, G: 5, B: 6, A: 255})

	got, err := FromImage(src)
	if err != nil {
		t.Fatal(err)
	}
	if got.Width != 2 || got.Height != 1 {
		t.Fatalf("geometry = %dx%d, want 2x1", got.Width, got.Height)
	}
	want := []byte{1, 2, 3, 4, 5, 6}
	for i := range want {
		if got.Pixels[i] != want[i] {
			t.Fatalf("pixel byte %d = %d, want %d", i, got.Pixels[i], want[i])
		}
	}
}

func TestNewRejectsInvalidPayload(t *testing.T) {
	if _, err := New(2, 2, make([]byte, 11)); err == nil {
		t.Fatal("expected an error")
	}
}
