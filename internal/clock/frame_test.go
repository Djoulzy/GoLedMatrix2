package clock

import (
	"testing"
	"time"
)

func TestRenderSimpleAndFancy(t *testing.T) {
	for _, mode := range []Mode{Simple, Fancy} {
		t.Run(string(mode), func(t *testing.T) {
			next, err := Render(time.Date(2026, time.July, 29, 15, 4, 30, 0, time.Local), 128, 128, mode)
			if err != nil {
				t.Fatal(err)
			}
			if next.Width != 128 || next.Height != 128 {
				t.Fatalf("frame geometry = %dx%d", next.Width, next.Height)
			}
			var lit int
			for _, value := range next.Pixels {
				if value != 0 {
					lit++
				}
			}
			if lit == 0 {
				t.Fatal("clock frame is blank")
			}
		})
	}
}

func TestRenderOfficeRound(t *testing.T) {
	next, err := Render(
		time.Date(2026, time.July, 29, 15, 4, 30, 0, time.Local),
		128,
		128,
		Round,
	)
	if err != nil {
		t.Fatal(err)
	}
	if next.Width != 128 || next.Height != 128 {
		t.Fatalf("frame geometry = %dx%d", next.Width, next.Height)
	}
	var red, white int
	for offset := 0; offset < len(next.Pixels); offset += 3 {
		r, g, b := next.Pixels[offset], next.Pixels[offset+1], next.Pixels[offset+2]
		switch {
		case r > 0 && g == 0 && b == 0:
			red++
		case r > 0 && r == g && g == b:
			white++
		}
	}
	if red == 0 || white == 0 {
		t.Fatalf("OfficeRound colors: red=%d white=%d", red, white)
	}
}

func TestRenderClockChangesEverySecond(t *testing.T) {
	at := time.Date(2026, time.July, 29, 15, 4, 0, 0, time.Local)
	first, err := Render(at, 128, 128, Simple)
	if err != nil {
		t.Fatal(err)
	}
	second, err := Render(at.Add(time.Second), 128, 128, Simple)
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
