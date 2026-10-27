package display

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/Djoulzy/GoLedMatrix2/internal/frame"
)

func TestMemoryBrightnessPreservesOriginalFrame(t *testing.T) {
	target, err := NewMemory(1, 1)
	if err != nil {
		t.Fatal(err)
	}
	next, _ := frame.New(1, 1, []byte{200, 100, 50})
	if err := target.Present(context.Background(), next); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		brightness int
		pixels     []byte
	}{
		{50, []byte{100, 50, 25}},
		{25, []byte{50, 25, 13}},
		{100, []byte{200, 100, 50}},
	} {
		if err := target.SetBrightness(context.Background(), tc.brightness); err != nil {
			t.Fatal(err)
		}
		if got := target.Latest(); !bytes.Equal(got.Pixels, tc.pixels) || target.Brightness() != tc.brightness {
			t.Fatalf("brightness %d: pixels = %v, setting = %d", tc.brightness, got.Pixels, target.Brightness())
		}
	}
	for _, value := range []int{-1, 0, 101, 256} {
		if err := target.SetBrightness(context.Background(), value); err == nil {
			t.Fatalf("brightness %d accepted", value)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := target.SetBrightness(ctx, 30); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled change = %v", err)
	}
	if target.Brightness() != 100 || !bytes.Equal(next.Pixels, []byte{200, 100, 50}) {
		t.Fatal("rejected request changed brightness or original pixels")
	}
}
