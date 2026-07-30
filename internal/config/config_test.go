package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadExample(t *testing.T) {
	loaded := loadString(t, `
[HardwareConfig]
Rows = 32
Cols = 64
ChainLength = 4
Parallel = 2
PWMDitherBits = 2
PixelMapperConfig = "V-mapper"
LimitRefreshRateHz = 70

[RuntimeOptions]
GpioSlowdown = 5
DoGpioInit = true

[HTTPserver]
Addr = "127.0.0.1"
Port = 9090
Enabled = true

[Clock]
DefaultMode = "round"
Color1 = "#112233"
Color2 = "#AABBCC"

[Animations]
Directory = "stored"
MaxUploadMB = 64
`)
	if loaded.Hardware.ChainLength != 4 || loaded.Hardware.Parallel != 2 {
		t.Fatalf("unexpected hardware config: %+v", loaded.Hardware)
	}
	if loaded.Hardware.PWMBits != 11 {
		t.Fatalf("omitted PWMBits = %d, want default 11", loaded.Hardware.PWMBits)
	}
	if loaded.Runtime.GPIOSlowdown != 5 {
		t.Fatalf("GpioSlowdown = %d, want 5", loaded.Runtime.GPIOSlowdown)
	}
	if loaded.HTTP.InfoDisplaySeconds != 5 {
		t.Fatalf("InfoDisplaySeconds = %d, want default 5", loaded.HTTP.InfoDisplaySeconds)
	}
	if loaded.Clock.DefaultMode != "round" {
		t.Fatalf("Clock.DefaultMode = %q, want round", loaded.Clock.DefaultMode)
	}
	if loaded.Clock.Color1 != "#112233" || loaded.Clock.Color2 != "#AABBCC" {
		t.Fatalf("unexpected clock colors: %+v", loaded.Clock)
	}
	if loaded.Animation.Directory != "stored" || loaded.Animation.MaxUploadMB != 64 {
		t.Fatalf("unexpected animation config: %+v", loaded.Animation)
	}
	address, err := loaded.HTTP.ListenAddress()
	if err != nil {
		t.Fatal(err)
	}
	if address != "127.0.0.1:9090" {
		t.Fatalf("listen address = %q", address)
	}
}

func TestLoadRejectsUnknownKey(t *testing.T) {
	path := writeConfig(t, "[HardwareConfig]\nRowz = 32\n")
	_, err := Load(path)
	if err == nil || !strings.Contains(err.Error(), "HardwareConfig.Rowz") {
		t.Fatalf("error = %v, want unknown key", err)
	}
}

func TestValidateRejectsInvalidValues(t *testing.T) {
	config := Default()
	config.Hardware.PWMDitherBits = 3
	if err := config.Validate(); err == nil {
		t.Fatal("expected validation error")
	}
}

func TestValidateRejectsInvalidInfoDisplayDuration(t *testing.T) {
	config := Default()
	config.HTTP.InfoDisplaySeconds = 61
	if err := config.Validate(); err == nil {
		t.Fatal("expected validation error")
	}
}

func TestValidateRejectsInvalidClockMode(t *testing.T) {
	config := Default()
	config.Clock.DefaultMode = "analog"
	if err := config.Validate(); err == nil {
		t.Fatal("expected validation error")
	}
}

func TestValidateRejectsInvalidClockColor(t *testing.T) {
	config := Default()
	config.Clock.Color1 = "red"
	if err := config.Validate(); err == nil {
		t.Fatal("expected validation error")
	}
}

func TestValidateRejectsInvalidAnimationConfig(t *testing.T) {
	config := Default()
	config.Animation.MaxUploadMB = 0
	if err := config.Validate(); err == nil {
		t.Fatal("expected validation error")
	}
}

func TestDetectListensOnAllInterfaces(t *testing.T) {
	got, err := (HTTPServer{Addr: "detect", Port: 8080}).ListenAddress()
	if err != nil {
		t.Fatal(err)
	}
	if got != ":8080" {
		t.Fatalf("address = %q, want :8080", got)
	}
}

func TestRepositoryExampleIsValid(t *testing.T) {
	if _, err := Load(filepath.Join("..", "..", "config.example.toml")); err != nil {
		t.Fatal(err)
	}
}

func loadString(t *testing.T, content string) Config {
	t.Helper()
	loaded, err := Load(writeConfig(t, content))
	if err != nil {
		t.Fatal(err)
	}
	return loaded
}

func writeConfig(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "server.toml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}
