//go:build cgo && rpistub

package display

import (
	"bytes"
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/Djoulzy/GoLedMatrix2/internal/frame"
)

func rpiTestConfig() RPIConfig {
	return RPIConfig{
		Rows: 64, Cols: 128, ChainLength: 2, Parallel: 3, Multiplexing: 6,
		HardwareMapping: "regular", PixelMapperConfig: "Rotate:90", RGBSequence: "BRG",
		Brightness: 75, PWMBits: 11, PWMLSBNanoseconds: 130, PWMDitherBits: 2,
		ScanMode: 1, RowAddressType: 3, ShowRefreshRate: true, LimitRefreshRateHz: 120,
		DisableHardwarePulsing: true, InverseColors: true, GPIOSlowdown: 5,
	}
}

func TestRPIBindingKeepsReturnedBackBuffer(t *testing.T) {
	resetRPIStub(0)
	target, err := NewRPI(rpiTestConfig())
	if err != nil {
		t.Fatal(err)
	}
	defer target.Close()
	width, height := target.Geometry()
	if width != 3 || height != 2 {
		t.Fatalf("native mapped geometry = %dx%d", width, height)
	}
	for n := 0; n < 100; n++ {
		pixels := make([]byte, width*height*3)
		for i := range pixels {
			pixels[i] = byte(n + i)
		}
		if err := target.Present(context.Background(), frame.Frame{Width: width, Height: height, Pixels: pixels}); err != nil {
			t.Fatal(err)
		}
		if got := rpiStubPixels(); !bytes.Equal(got, pixels) {
			t.Fatalf("frame %d RGB24 pixels = %v, want %v", n, got, pixels)
		}
	}
	created, _, swaps, writes, activeWrites, invalidImages := rpiStubCounts()
	if created != 1 || swaps != 100 || writes != 100 || activeWrites != 0 || invalidImages != 0 {
		t.Fatalf("native operations: created=%d swaps=%d writes=%d activeWrites=%d invalid=%d", created, swaps, writes, activeWrites, invalidImages)
	}
	if got, want := rpiStubConfig(), rpiTestConfig(); got != want {
		t.Fatalf("native config = %+v, want %+v", got, want)
	}
}

func TestRPIBindingRejectsInvalidFramesAndClosesOnce(t *testing.T) {
	resetRPIStub(0)
	target, err := NewRPI(rpiTestConfig())
	if err != nil {
		t.Fatal(err)
	}
	defer target.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	valid := frame.Frame{Width: 3, Height: 2, Pixels: make([]byte, 18)}
	if err := target.Present(ctx, valid); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled render = %v", err)
	}
	for _, next := range []frame.Frame{
		{Width: 2, Height: 3, Pixels: make([]byte, 18)},
		{Width: 3, Height: 2, Pixels: nil},
		{Width: 3, Height: 2, Pixels: make([]byte, 17)},
		{Width: 3, Height: 2, Pixels: make([]byte, 19)},
	} {
		if err := target.Present(context.Background(), next); err == nil {
			t.Fatalf("invalid frame accepted: %+v", next)
		}
	}
	if err := target.Close(); err != nil {
		t.Fatal(err)
	}
	if err := target.Close(); err != nil {
		t.Fatal(err)
	}
	if err := target.Present(context.Background(), valid); err == nil {
		t.Fatal("render after Close succeeded")
	}
	_, deleted, swaps, writes, _, _ := rpiStubCounts()
	if deleted != 1 || swaps != 0 || writes != 0 {
		t.Fatalf("rejected operations reached C: deleted=%d swaps=%d writes=%d", deleted, swaps, writes)
	}
}

func TestRPIBindingInitializationFailures(t *testing.T) {
	for failure := 1; failure <= 3; failure++ {
		resetRPIStub(failure)
		if target, err := NewRPI(rpiTestConfig()); err == nil || target != nil {
			t.Fatalf("failure %d: target=%v, err=%v", failure, target, err)
		}
		created, deleted, _, _, _, _ := rpiStubCounts()
		if created != deleted {
			t.Fatalf("failure %d leaked matrix: created=%d deleted=%d", failure, created, deleted)
		}
	}
}

func TestRPIBindingConcurrentCloseAndPresent(t *testing.T) {
	resetRPIStub(0)
	target, err := NewRPI(rpiTestConfig())
	if err != nil {
		t.Fatal(err)
	}
	var workers sync.WaitGroup
	for i := 0; i < 8; i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for j := 0; j < 20; j++ {
				_ = target.Present(context.Background(), frame.Frame{Width: 3, Height: 2, Pixels: make([]byte, 18)})
			}
		}()
	}
	workers.Add(1)
	go func() { defer workers.Done(); _ = target.Close() }()
	workers.Wait()
	_, deleted, _, _, activeWrites, invalidImages := rpiStubCounts()
	if deleted != 1 || activeWrites != 0 || invalidImages != 0 {
		t.Fatalf("concurrent operations: deleted=%d activeWrites=%d invalid=%d", deleted, activeWrites, invalidImages)
	}
}
