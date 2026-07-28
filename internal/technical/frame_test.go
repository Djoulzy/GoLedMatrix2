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
	next, err := Render(State{
		Backend: "memory", BaseURL: "http://127.0.0.1:8080",
		Width: 64, Height: 32, Protocol: "1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if next.Width != 64 || next.Height != 32 {
		t.Fatalf("frame geometry = %dx%d", next.Width, next.Height)
	}
}
