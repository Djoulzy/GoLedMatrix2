package marquee

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
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

func TestSubpixelScrollAndClipping(t *testing.T) {
	mask := image.NewAlpha(image.Rect(0, 0, 1, 1))
	mask.Pix[0] = 255
	m := &Marquee{
		options: Options{Speed: 1}, width: 5, height: 1, frameSize: 15,
		mask: mask, color: color.RGBA{R: 255, G: 255, B: 255, A: 255},
	}
	for _, tc := range []struct {
		elapsed time.Duration
		want    []byte
	}{
		{-time.Second, []byte{0, 0, 0, 0, 0}},
		{0, []byte{0, 0, 0, 0, 0}},
		{250 * time.Millisecond, []byte{0, 0, 0, 0, 64}},
		{500 * time.Millisecond, []byte{0, 0, 0, 0, 128}},
		{time.Second, []byte{0, 0, 0, 0, 255}},
		{1250 * time.Millisecond, []byte{0, 0, 0, 64, 191}},
		{1500 * time.Millisecond, []byte{0, 0, 0, 128, 128}},
		{2250 * time.Millisecond, []byte{0, 0, 64, 191, 0}},
		{5500 * time.Millisecond, []byte{128, 0, 0, 0, 0}},
		{5750 * time.Millisecond, []byte{64, 0, 0, 0, 0}},
		{6 * time.Second, []byte{0, 0, 0, 0, 0}},
		{6250 * time.Millisecond, []byte{0, 0, 0, 0, 64}},
	} {
		got := m.Render(tc.elapsed)
		for col, want := range tc.want {
			for channel := 0; channel < 3; channel++ {
				if got.Pixels[col*3+channel] != want {
					t.Fatalf("at %v col %d channel %d: got %d, want %d", tc.elapsed, col, channel, got.Pixels[col*3+channel], want)
				}
			}
		}
	}
	// Tiny clock differences around an integer or wrap boundary no longer
	// turn a one-pixel movement into a hold followed by a two-pixel jump.
	for _, boundary := range []time.Duration{time.Second, 6 * time.Second} {
		before := m.Render(boundary - time.Nanosecond)
		after := m.Render(boundary + time.Nanosecond)
		if !bytes.Equal(before.Pixels, after.Pixels) {
			t.Fatalf("discontinuous subpixel position near %v", boundary)
		}
	}
}

func TestSubpixelScrollDoesNotMixTextColors(t *testing.T) {
	m, err := New(64, 32, Options{Text: "Bonjour", Speed: 45, Color: "#FF0000"})
	if err != nil {
		t.Fatal(err)
	}
	// At 60 fps this speed advances 0.75 pixel per frame. Intermediate
	// frames must be distinct without introducing other RGB channels.
	before, after := m.Render(time.Second), m.Render(time.Second+playbackInterval)
	if bytes.Equal(before.Pixels, after.Pixels) {
		t.Fatal("fractional motion was rounded away")
	}
	for offset := 0; offset < len(after.Pixels); offset += 3 {
		if after.Pixels[offset+1] != 0 || after.Pixels[offset+2] != 0 {
			t.Fatal("subpixel blending introduced a different color")
		}
	}
}

func TestBounceFrames(t *testing.T) {
	for _, tc := range []struct {
		name  string
		width int
		mask  []byte
		want  [][]byte // Half-second steps through one full round trip.
	}{
		{
			name: "short text", width: 5, mask: []byte{255, 128, 64},
			want: [][]byte{
				{0, 0, 255, 128, 64}, {0, 128, 192, 96, 32},
				{0, 255, 128, 64, 0}, {128, 192, 96, 32, 0},
				{255, 128, 64, 0, 0}, {128, 192, 96, 32, 0},
				{0, 255, 128, 64, 0}, {0, 128, 192, 96, 32},
				{0, 0, 255, 128, 64},
			},
		},
		{
			name: "long text", width: 3, mask: []byte{255, 128, 64, 32, 16},
			want: [][]byte{
				{255, 128, 64}, {192, 96, 48}, {128, 64, 32}, {96, 48, 24},
				{64, 32, 16}, {96, 48, 24}, {128, 64, 32}, {192, 96, 48},
				{255, 128, 64},
			},
		},
		{
			name: "exact fit", width: 3, mask: []byte{255, 128, 64},
			want: [][]byte{{255, 128, 64}, {255, 128, 64}, {255, 128, 64}},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mask := image.NewAlpha(image.Rect(0, 0, len(tc.mask), 1))
			copy(mask.Pix, tc.mask)
			m := &Marquee{
				options: Options{Speed: 1, Bounce: true}, width: tc.width, height: 1, frameSize: tc.width * 3,
				mask: mask, color: color.RGBA{R: 255, G: 255, B: 255, A: 255},
			}
			for cycle := 0; cycle < 3; cycle++ {
				for i, want := range tc.want {
					elapsed := time.Duration(cycle)*4*time.Second + time.Duration(i)*500*time.Millisecond
					got := m.Render(elapsed)
					for col, value := range want {
						for channel := 0; channel < 3; channel++ {
							if got.Pixels[col*3+channel] != value {
								t.Fatalf("at %v col %d channel %d: got %d, want %d", elapsed, col, channel, got.Pixels[col*3+channel], value)
							}
						}
					}
				}
			}
			if !bytes.Equal(m.Render(-time.Second).Pixels, m.Render(0).Pixels) {
				t.Fatal("negative elapsed time must use the initial position")
			}
			// The direction reverses without a discontinuity at either edge.
			for _, boundary := range []time.Duration{2 * time.Second, 4 * time.Second} {
				if !bytes.Equal(m.Render(boundary-time.Nanosecond).Pixels, m.Render(boundary+time.Nanosecond).Pixels) {
					t.Fatalf("discontinuous bounce near %v", boundary)
				}
			}
		})
	}
}

func TestBounceSpeedAndPalette(t *testing.T) {
	m, err := New(128, 32, Options{Text: "Hi", Bounce: true, Speed: 3.5,
		ColorCycle: []string{"#FF0000", "#0000FF"}, CycleSeconds: 4})
	if err != nil {
		t.Fatal(err)
	}
	if !m.Options().Bounce {
		t.Fatal("normalized options lost bounce")
	}
	right := float64(128 - m.mask.Rect.Dx())
	turn := right / 3.5
	for _, tc := range []struct{ seconds, want float64 }{
		{0, right}, {1, right - 3.5}, {turn, 0}, {turn + 1, 3.5}, {2 * turn, right},
	} {
		if got := m.horizontalPosition(tc.seconds); math.Abs(got-tc.want) > 1e-9 {
			t.Fatalf("position at %gs = %g, want %g", tc.seconds, got, tc.want)
		}
	}
	for _, tc := range []struct {
		elapsed time.Duration
		channel int
	}{{0, 0}, {2 * time.Second, 2}} {
		pixels := m.Render(tc.elapsed).Pixels
		visible := false
		for offset := 0; offset < len(pixels); offset += 3 {
			visible = visible || pixels[offset+tc.channel] > 0
			for channel := 0; channel < 3; channel++ {
				if channel != tc.channel && pixels[offset+channel] != 0 {
					t.Fatalf("unexpected color at %v", tc.elapsed)
				}
			}
		}
		if !visible {
			t.Fatalf("bouncing text is invisible at %v", tc.elapsed)
		}
	}
}

func TestVerticalBounceTrajectory(t *testing.T) {
	m := &Marquee{height: 11, mask: image.NewAlpha(image.Rect(0, 0, 1, 3)),
		options: Options{VerticalBounce: true, VerticalBounceSeconds: 4}}
	for _, tc := range []struct{ seconds, want float64 }{
		{0, 4}, {1, 8}, {2, 4}, {3, 0}, {4, 4}, {5, 8},
		{1.0 / 3, 6}, {11.0 / 3, 2},
	} {
		if got := m.verticalPosition(tc.seconds); math.Abs(got-tc.want) > 1e-9 {
			t.Fatalf("vertical position at %gs = %g, want %g", tc.seconds, got, tc.want)
		}
	}
	for tick := 0; tick < 600; tick++ {
		if got := m.verticalPosition(float64(tick) / 60); got < 0 || got > 8 {
			t.Fatalf("vertical position outside display: %g", got)
		}
	}
	for _, boundary := range []float64{1, 3, 4} {
		if math.Abs(m.verticalPosition(boundary-1e-9)-m.verticalPosition(boundary+1e-9)) > 1e-7 {
			t.Fatalf("discontinuous vertical motion at %gs", boundary)
		}
	}
	m.options.VerticalBounceSeconds = 8
	if got := m.verticalPosition(2); got != 8 {
		t.Fatalf("custom period: got %g, want 8", got)
	}
	// An odd amount of spare height uses a subpixel center when oscillating,
	// but keeps the original integer-centered rendering when disabled.
	m.height = 6
	if got := m.verticalPosition(0); got != 1.5 {
		t.Fatalf("subpixel vertical center = %g", got)
	}
	m.options.VerticalBounce = false
	if got := m.verticalPosition(2); got != 1 {
		t.Fatalf("legacy vertical center = %g", got)
	}
}

func TestVerticalBounceFrames(t *testing.T) {
	mask := image.NewAlpha(image.Rect(0, 0, 1, 1))
	mask.Pix[0] = 255
	m := &Marquee{
		options: Options{Speed: 1, Bounce: true, VerticalBounce: true, VerticalBounceSeconds: 4},
		width:   1, height: 5, frameSize: 15, mask: mask,
		color: color.RGBA{R: 255, G: 255, B: 255, A: 255},
	}
	halfPixelTime := time.Duration(math.Asin(0.25) / (2 * math.Pi) * 4 * float64(time.Second))
	for _, tc := range []struct {
		elapsed time.Duration
		want    []byte
	}{
		{-time.Second, []byte{0, 0, 255, 0, 0}},
		{0, []byte{0, 0, 255, 0, 0}},
		{halfPixelTime, []byte{0, 0, 128, 128, 0}},
		{time.Second, []byte{0, 0, 0, 0, 255}},
		{2 * time.Second, []byte{0, 0, 255, 0, 0}},
		{3 * time.Second, []byte{255, 0, 0, 0, 0}},
		{4 * time.Second, []byte{0, 0, 255, 0, 0}},
	} {
		pixels := m.Render(tc.elapsed).Pixels
		for row, value := range tc.want {
			for channel := 0; channel < 3; channel++ {
				if got := pixels[row*3+channel]; got != value {
					t.Fatalf("at %v row %d channel %d: got %d, want %d", tc.elapsed, row, channel, got, value)
				}
			}
		}
	}
	for _, boundary := range []time.Duration{time.Second, 3 * time.Second, 4 * time.Second} {
		if !bytes.Equal(m.Render(boundary-time.Nanosecond).Pixels, m.Render(boundary+time.Nanosecond).Pixels) {
			t.Fatalf("discontinuous subpixel frame at %v", boundary)
		}
	}
}

func TestVerticalBounceCombinedWithHorizontalMotion(t *testing.T) {
	mask := image.NewAlpha(image.Rect(0, 0, 1, 1))
	mask.Pix[0] = 255
	m := &Marquee{
		options: Options{Speed: 1, VerticalBounce: true, VerticalBounceSeconds: 6},
		width:   2, height: 3, frameSize: 18, mask: mask,
		color: color.RGBA{R: 255, G: 128, A: 255},
	}
	// Half a second gives a half-pixel vertical offset and a half-pixel
	// horizontal offset, both with normal right-to-left scrolling and bounce.
	for _, bounce := range []bool{false, true} {
		m.options.Bounce = bounce
		pixels := m.Render(500 * time.Millisecond).Pixels
		for row := 0; row < 3; row++ {
			for col := 0; col < 2; col++ {
				want := []byte{0, 0, 0}
				if row > 0 && (bounce || col == 1) {
					want = []byte{64, 32, 0}
				}
				offset := (row*2 + col) * 3
				if !bytes.Equal(pixels[offset:offset+3], want) {
					t.Fatalf("bounce %v at %d,%d: got %v, want %v", bounce, col, row, pixels[offset:offset+3], want)
				}
			}
		}
	}
	m.options.Bounce = false
	// Wrapping the horizontal position must not reset the vertical phase.
	if got := m.verticalPosition(3); math.Abs(got-1) > 1e-9 {
		t.Fatalf("vertical phase reset at horizontal wrap: %g", got)
	}
	if got := m.verticalPosition(3.5); math.Abs(got-0.5) > 1e-9 {
		t.Fatalf("vertical phase after horizontal wrap: %g", got)
	}
}

func TestVerticalBounceWithoutSpareHeight(t *testing.T) {
	for _, height := range []int{2, 3} {
		mask := image.NewAlpha(image.Rect(0, 0, 1, 3))
		copy(mask.Pix, []byte{255, 128, 64})
		m := &Marquee{options: Options{Speed: 1, Bounce: true}, width: 1, height: height,
			frameSize: height * 3, mask: mask, color: color.RGBA{R: 255, A: 255}}
		want := m.Render(0).Pixels
		m.options.VerticalBounce = true
		m.options.VerticalBounceSeconds = 4
		for _, elapsed := range []time.Duration{0, time.Second, 3 * time.Second} {
			if got := m.Render(elapsed).Pixels; !bytes.Equal(got, want) {
				t.Fatalf("height %d should preserve centered crop at %v", height, elapsed)
			}
		}
	}
}

func TestVerticalBounceDefaultsAndCompatibility(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		m, err := New(64, 32, Options{Text: "Bonjour", VerticalBounce: enabled})
		if err != nil {
			t.Fatal(err)
		}
		options := m.Options()
		if options.VerticalBounce != enabled {
			t.Fatal("normalized options lost vertical bounce")
		}
		if enabled {
			if options.VerticalBounceSeconds != 4 {
				t.Fatalf("default vertical period = %g", options.VerticalBounceSeconds)
			}
		} else {
			encoded, err := json.Marshal(options)
			if err != nil {
				t.Fatal(err)
			}
			if bytes.Contains(encoded, []byte("vertical_bounce")) {
				t.Fatalf("disabled vertical motion should omit new fields for older servers: %s", encoded)
			}
		}
	}
}

func BenchmarkMarqueeRender128(b *testing.B) {
	m, err := New(128, 128, Options{Text: "Bonjour à tous !", Size: 32, Speed: 45, ColorCycle: []string{"#FF0000", "#0000FF"}})
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = m.Render(time.Second + time.Duration(i%60)*playbackInterval)
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
		{Text: "test", VerticalBounceSeconds: -1}, {Text: "test", VerticalBounceSeconds: 0.05},
		{Text: "test", VerticalBounceSeconds: 3601}, {Text: "test", VerticalBounceSeconds: math.NaN()},
		{Text: "test", VerticalBounceSeconds: math.Inf(1)},
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
