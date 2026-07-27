package display

import (
	"image"
	"testing"
)

func TestInitialSimulationWindowIsBounded(t *testing.T) {
	got := initialWindowSize(256, 64, 12)
	if got.X > 1600 || got.Y > 1000 {
		t.Fatalf("window size = %v, want at most 1600x1000", got)
	}
	if got.X <= 0 || got.Y <= 0 {
		t.Fatalf("window size = %v, want positive dimensions", got)
	}
}

func TestSimulationLayoutFitsView(t *testing.T) {
	pitch, gutter, margin := simulationLayout(image.Pt(800, 400), 64, 32)
	if pitch < 1 || gutter < 1 || margin < 0 {
		t.Fatalf("invalid layout: pitch=%d gutter=%d margin=%d", pitch, gutter, margin)
	}
	if margin+64*(pitch+gutter)-gutter > 800 {
		t.Fatal("matrix layout exceeds view width")
	}
	if margin+32*(pitch+gutter)-gutter > 400 {
		t.Fatal("matrix layout exceeds view height")
	}
}
