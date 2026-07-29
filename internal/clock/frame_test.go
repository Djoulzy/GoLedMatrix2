package clock

import (
	"testing"
	"time"
)

func TestRenderClockIsPixelPerfect(t *testing.T) {
	for _, mode := range []Mode{Simple, Fancy, Round} {
		t.Run(string(mode), func(t *testing.T) {
			next, err := Render(time.Date(2026, time.July, 29, 15, 4, 30, 0, time.Local), 128, 128, mode)
			if err != nil {
				t.Fatal(err)
			}
			if next.Width != 128 || next.Height != 128 {
				t.Fatalf("frame geometry = %dx%d", next.Width, next.Height)
			}
			assertPixelPerfect(t, next.Pixels)
		})
	}
}

func TestRenderClockChangesEverySecond(t *testing.T) {
	at := time.Date(2026, time.July, 29, 15, 4, 0, 0, time.Local)
	first, err := Render(at, 64, 32, Simple)
	if err != nil {
		t.Fatal(err)
	}
	second, err := Render(at.Add(time.Second), 64, 32, Simple)
	if err != nil {
		t.Fatal(err)
	}
	if string(first.Pixels) == string(second.Pixels) {
		t.Fatal("clock frame did not change after one second")
	}
}

func TestParseMode(t *testing.T) {
	for _, value := range []string{"simple", "FANCY", " round "} {
		if _, err := ParseMode(value); err != nil {
			t.Fatalf("ParseMode(%q): %v", value, err)
		}
	}
	if _, err := ParseMode("unknown"); err == nil {
		t.Fatal("expected invalid mode error")
	}
}

func assertPixelPerfect(t *testing.T, pixels []byte) {
	t.Helper()
	allowed := map[[3]byte]bool{
		{0, 0, 0}:       true,
		{255, 131, 55}:  true,
		{123, 224, 222}: true,
		{220, 220, 220}: true,
		{20, 48, 48}:    true,
	}
	lit := 0
	for offset := 0; offset < len(pixels); offset += 3 {
		rgb := [3]byte{pixels[offset], pixels[offset+1], pixels[offset+2]}
		if !allowed[rgb] {
			t.Fatalf("pixel %d has non-bitmap color %v", offset/3, rgb)
		}
		if rgb != [3]byte{} {
			lit++
		}
	}
	if lit == 0 {
		t.Fatal("clock frame is blank")
	}
}
