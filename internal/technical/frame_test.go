package technical

import (
	"testing"
	"time"
)

func TestRenderTechnicalInformation(t *testing.T) {
	next, err := Render(State{
		Backend: "rpi", BaseURL: "http://192.168.0.18:8080",
		Width: 128, Height: 128, Uptime: 83 * time.Second, Protocol: "1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if next.Width != 128 || next.Height != 128 {
		t.Fatalf("frame geometry = %dx%d", next.Width, next.Height)
	}
	var litPixels int
	for offset := 0; offset < len(next.Pixels); offset += 3 {
		if next.Pixels[offset] != 0 || next.Pixels[offset+1] != 0 || next.Pixels[offset+2] != 0 {
			litPixels++
		}
	}
	if litPixels == 0 {
		t.Fatal("technical information frame is blank")
	}
}

func TestEndpointParts(t *testing.T) {
	host, port := endpointParts("http://192.168.0.18:8080")
	if host != "192.168.0.18" || port != "8080" {
		t.Fatalf("endpoint = %s:%s", host, port)
	}
}

func TestRenderCompactTechnicalInformation(t *testing.T) {
	state := State{
		Backend: "memory", BaseURL: "http://127.0.0.1:8080",
		Width: 64, Height: 32, Protocol: "1",
	}
	next, err := Render(state)
	if err != nil {
		t.Fatal(err)
	}
	if next.Width != 64 || next.Height != 32 {
		t.Fatalf("frame geometry = %dx%d", next.Width, next.Height)
	}
	assertBitmapColors(t, next.Pixels)

	host, port := endpointParts(state.BaseURL)
	lines := technicalLines(state, host, port)
	if len(lines) != 2 {
		t.Fatalf("compact line count = %d, want 2", len(lines))
	}
	if lines[0].text != "MEM 64X32 V1" || lines[1].text != "LOCALHOST" {
		t.Fatalf("compact lines = %q, %q", lines[0].text, lines[1].text)
	}
}

func assertBitmapColors(t *testing.T, pixels []byte) {
	t.Helper()
	allowed := map[[3]byte]bool{
		{0, 0, 0}:       true,
		{64, 255, 96}:   true,
		{255, 220, 64}:  true,
		{80, 200, 255}:  true,
		{220, 220, 220}: true,
	}
	for offset := 0; offset < len(pixels); offset += 3 {
		rgb := [3]byte{pixels[offset], pixels[offset+1], pixels[offset+2]}
		if !allowed[rgb] {
			t.Fatalf("pixel %d has antialiased color %v", offset/3, rgb)
		}
	}
}
