package client

import (
	"bytes"
	"image"
	"image/color"
	"image/gif"
	"testing"
)

func TestPrepareGIFCompositesAndResizes(t *testing.T) {
	palette := color.Palette{
		color.RGBA{A: 0},
		color.RGBA{R: 255, A: 255},
		color.RGBA{G: 255, A: 255},
	}
	first := image.NewPaletted(image.Rect(0, 0, 2, 2), palette)
	first.SetColorIndex(0, 0, 1)
	second := image.NewPaletted(image.Rect(1, 1, 2, 2), palette)
	second.SetColorIndex(1, 1, 2)
	source := gif.GIF{
		Image:     []*image.Paletted{first, second},
		Delay:     []int{2, 4},
		Disposal:  []byte{gif.DisposalNone, gif.DisposalNone},
		LoopCount: 2,
		Config: image.Config{
			ColorModel: palette, Width: 2, Height: 2,
		},
	}
	var encoded bytes.Buffer
	if err := gif.EncodeAll(&encoded, &source); err != nil {
		t.Fatal(err)
	}
	bundle, err := PrepareGIF(&encoded, 4, 4)
	if err != nil {
		t.Fatal(err)
	}
	if len(bundle.Frames) != 2 || bundle.Loops != 3 {
		t.Fatalf("bundle = %+v", bundle)
	}
	if bundle.Frames[0].Duration.Milliseconds() != 20 ||
		bundle.Frames[1].Duration.Milliseconds() != 40 {
		t.Fatalf("durations = %s, %s", bundle.Frames[0].Duration, bundle.Frames[1].Duration)
	}
}
