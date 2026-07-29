package main

import (
	"context"
	"reflect"
	"testing"

	appconfig "github.com/Djoulzy/GoLedMatrix2/internal/config"
	"github.com/Djoulzy/GoLedMatrix2/internal/display"
	"github.com/Djoulzy/GoLedMatrix2/internal/render"
	"github.com/Djoulzy/GoLedMatrix2/internal/server"
)

func TestRPIConfigMapsSupportedFileSettings(t *testing.T) {
	hardware := appconfig.HardwareConfig{
		Rows: 1, Cols: 2, ChainLength: 3, Parallel: 4,
		PWMBits: 5, PWMLSBNanoseconds: 6, PWMDitherBits: 7,
		Brightness: 8, ScanMode: 9, HardwareMapping: "mapping",
		ShowRefreshRate: true, InverseColors: true,
		DisableHardwarePulsing: true, PixelMapperConfig: "mapper",
		LimitRefreshRateHz: 10, Multiplexing: 11,
	}
	runtime := appconfig.RuntimeOptions{
		GPIOSlowdown: 12, Daemon: 13, DropPrivileges: 14, DoGPIOInit: true,
	}
	want := display.RPIConfig{
		Rows: 1, Cols: 2, ChainLength: 3, Parallel: 4,
		PWMBits: 5, PWMLSBNanoseconds: 6, PWMDitherBits: 7,
		Brightness: 8, ScanMode: 9, HardwareMapping: "mapping",
		ShowRefreshRate: true, InverseColors: true,
		DisableHardwarePulsing: true, PixelMapperConfig: "mapper",
		LimitRefreshRateHz: 10, Multiplexing: 11,
		GPIOSlowdown: 12,
	}
	if got := rpiConfig(hardware, runtime); !reflect.DeepEqual(got, want) {
		t.Fatalf("RPI config = %+v, want %+v", got, want)
	}
}

func TestAdvertisedBaseURLsUsesConfiguredAddress(t *testing.T) {
	got := advertisedBaseURLs("192.168.0.18:8080")
	want := []string{"http://192.168.0.18:8080"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("advertised URLs = %v, want %v", got, want)
	}
}

func TestSimulationGeometryUsesHardwareConfiguration(t *testing.T) {
	settings := appconfig.Default()
	settings.Hardware.Rows = 32
	settings.Hardware.Cols = 64
	settings.Hardware.ChainLength = 4
	settings.Hardware.Parallel = 2
	settings.Hardware.PixelMapperConfig = "V-mapper"
	cfg := options{
		backend: "simulation", config: settings,
		simulationPixelPitch: 8,
	}
	width, height, pitch, err := simulationGeometry(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if width != 128 || height != 128 || pitch != 8 {
		t.Fatalf("simulation geometry = %dx%d pitch %d", width, height, pitch)
	}
}

func TestClockControllerSwitchesMode(t *testing.T) {
	target, err := display.NewMemory(128, 128)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	controller, err := startClock(ctx, render.New(target), 128, 128, "fancy", "", "")
	if err != nil {
		t.Fatal(err)
	}
	state, err := controller.Activate(server.ClockSelection{
		Mode: "round", Color1: "#112233", Color2: "#AABBCC",
	})
	if err != nil {
		t.Fatal(err)
	}
	if state.Mode != "round" || state.Color1 != "#112233" || state.Color2 != "#AABBCC" {
		t.Fatalf("active clock = %+v", state)
	}
	if _, err := controller.Activate(server.ClockSelection{Mode: "unknown"}); err == nil {
		t.Fatal("expected invalid clock mode error")
	}
}
