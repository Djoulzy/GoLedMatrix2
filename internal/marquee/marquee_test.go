package marquee

import (
	"bytes"
	"context"
	"image/color"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/Djoulzy/GoLedMatrix2/internal/display"
	"github.com/Djoulzy/GoLedMatrix2/internal/frame"
	"github.com/Djoulzy/GoLedMatrix2/internal/render"
)

func TestScrollDirectionAndWrap(t *testing.T) {
	m, err := New(64, 32, Options{Text: "Été", Size: 16, Speed: 1, Color: "#ff0000"})
	if err != nil {
		t.Fatal(err)
	}
	blank := make([]byte, 64*32*3)
	if !bytes.Equal(m.Render(0).Pixels, blank) {
		t.Fatal("text should start outside the right edge")
	}
	before, after := m.Render(40*time.Second), m.Render(41*time.Second)
	if bytes.Equal(before.Pixels, blank) {
		t.Fatal("text never entered the display")
	}
	for y := 0; y < 32; y++ {
		for x := 0; x < 63; x++ {
			a, b := (y*64+x)*3, (y*64+x+1)*3
			if !bytes.Equal(after.Pixels[a:a+3], before.Pixels[b:b+3]) {
				t.Fatalf("text did not shift one pixel left at %d,%d", x, y)
			}
		}
	}
	period := time.Duration(64+m.mask.Rect.Dx()) * time.Second
	if !bytes.Equal(m.Render(period).Pixels, blank) || !bytes.Equal(m.Render(period+40*time.Second).Pixels, before.Pixels) {
		t.Fatal("text did not wrap after leaving the left edge")
	}
	for offset := 0; offset < len(before.Pixels); offset += 3 {
		if before.Pixels[offset+1] != 0 || before.Pixels[offset+2] != 0 {
			t.Fatal("fixed red text contains another color")
		}
	}
}

func TestPaletteInterpolation(t *testing.T) {
	m, err := New(64, 32, Options{Text: "Bonjour", ColorCycle: []string{"#ff0000", "#0000ff"}, CycleSeconds: 4})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		seconds float64
		want    color.RGBA
	}{
		{0, color.RGBA{R: 255, A: 255}},
		{1, color.RGBA{R: 128, B: 128, A: 255}},
		{2, color.RGBA{B: 255, A: 255}},
		{3, color.RGBA{R: 128, B: 128, A: 255}},
		{4, color.RGBA{R: 255, A: 255}},
	} {
		if got := m.colorAt(tc.seconds); got != tc.want {
			t.Fatalf("color at %gs = %v, want %v", tc.seconds, got, tc.want)
		}
	}
}

func TestFontsAndValidation(t *testing.T) {
	for _, options := range []Options{
		{}, {Text: "   "}, {Text: "a\nb"}, {Text: string([]byte{0xff})}, {Text: strings.Repeat("x", 4097)},
		{Text: "test", Font: "missing"}, {Text: "test", Size: -1}, {Text: "test", Size: 257},
		{Text: "test", Color: "#GG0000"}, {Text: "test", Speed: -1}, {Text: "test", Speed: math.NaN()},
		{Text: "test", Speed: math.Inf(1)}, {Text: "test", CycleSeconds: -1},
		{Text: "test", ColorCycle: []string{"#ff0000"}},
		{Text: "test", ColorCycle: []string{"#ff0000", "bad"}},
		{Text: strings.Repeat("W", 4096), Size: 256},
	} {
		if _, err := New(64, 32, options); err == nil {
			t.Fatalf("invalid options accepted: %+v", options)
		}
	}
	if colors, err := ParseCycle("rainbow"); err != nil || len(colors) != 6 {
		t.Fatalf("rainbow = %v, %v", colors, err)
	}
	if colors, err := ParseCycle(" #ff0000, #00ff00 "); err != nil || len(colors) != 2 || colors[1] != "#00FF00" {
		t.Fatalf("custom palette = %v, %v", colors, err)
	}
	if _, err := ParseCycle("#ff0000"); err == nil {
		t.Fatal("single-color cycle accepted")
	}
}

func TestEveryBundledFontRenders(t *testing.T) {
	fonts, err := Fonts()
	if err != nil {
		t.Fatal(err)
	}
	for _, font := range fonts {
		t.Run(font.Name, func(t *testing.T) {
			m, err := New(64, 32, Options{Text: "Bonjour 0123456789", Font: font.Name, Speed: 1})
			if err != nil {
				t.Fatal(err)
			}
			if got := m.Render(64 * time.Second).Pixels; bytes.Equal(got, make([]byte, len(got))) {
				t.Fatal("font renders no visible pixels")
			}
		})
	}
}

func TestAssetFontAliases(t *testing.T) {
	for _, name := range []string{"Flashback", "flashback.ttf", "marquee/Flashback.ttf", "assets/ttf/marquee/Flashback.ttf"} {
		m, err := New(64, 32, Options{Text: "Hello", Font: name})
		if err != nil {
			t.Fatalf("alias %q: %v", name, err)
		}
		if m.Options().Font != "marquee/Flashback.ttf" {
			t.Fatalf("alias %q resolved to %q", name, m.Options().Font)
		}
	}
	if _, err := New(64, 32, Options{Text: "Hello", Font: "../modern/Perform.ttf"}); err == nil {
		t.Fatal("non-catalogue path accepted")
	}
}

func TestPlayerStopPreventsLateFrames(t *testing.T) {
	target, _ := display.NewMemory(32, 16)
	renderer := render.New(target)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go renderer.Run(ctx)
	player := NewPlayer(ctx, renderer)
	text, err := New(32, 16, Options{Text: "Test", Size: 12, Speed: 500})
	if err != nil {
		t.Fatal(err)
	}
	player.Play(text)
	deadline := time.Now().Add(time.Second)
	for len(target.Latest().Pixels) == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	player.Stop()
	pixels := bytes.Repeat([]byte{17, 23, 42}, 32*16)
	renderer.Submit(frame.Frame{Width: 32, Height: 16, Pixels: pixels})
	deadline = time.Now().Add(time.Second)
	for !bytes.Equal(target.Latest().Pixels, pixels) && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	// Observe several playback ticks to catch a stale submission after Stop.
	for i := 0; i < 10; i++ {
		if !bytes.Equal(target.Latest().Pixels, pixels) {
			t.Fatal("stopped marquee replaced a newer client frame")
		}
		time.Sleep(10 * time.Millisecond)
	}
}
