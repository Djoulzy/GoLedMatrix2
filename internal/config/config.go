// Package config loads and validates the server TOML configuration.
package config

import (
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"

	"github.com/BurntSushi/toml"
	matrixclock "github.com/Djoulzy/GoLedMatrix2/internal/clock"
)

type Config struct {
	Hardware HardwareConfig `toml:"HardwareConfig"`
	Runtime  RuntimeOptions `toml:"RuntimeOptions"`
	HTTP     HTTPServer     `toml:"HTTPserver"`
	Clock    ClockConfig    `toml:"Clock"`
}

type HardwareConfig struct {
	Rows                   int    `toml:"Rows"`
	Cols                   int    `toml:"Cols"`
	ChainLength            int    `toml:"ChainLength"`
	Parallel               int    `toml:"Parallel"`
	PWMBits                int    `toml:"PWMBits"`
	PWMLSBNanoseconds      int    `toml:"PWMLSBNanoseconds"`
	PWMDitherBits          int    `toml:"PWMDitherBits"`
	Brightness             int    `toml:"Brightness"`
	ScanMode               int    `toml:"ScanMode"`
	HardwareMapping        string `toml:"HardwareMapping"`
	ShowRefreshRate        bool   `toml:"ShowRefreshRate"`
	InverseColors          bool   `toml:"InverseColors"`
	DisableHardwarePulsing bool   `toml:"DisableHardwarePulsing"`
	PixelMapperConfig      string `toml:"PixelMapperConfig"`
	LimitRefreshRateHz     int    `toml:"LimitRefreshRateHz"`
	Multiplexing           int    `toml:"Multiplexing"`
}

type RuntimeOptions struct {
	GPIOSlowdown   int  `toml:"GpioSlowdown"`
	Daemon         int  `toml:"Daemon"`
	DropPrivileges int  `toml:"DropPrivileges"`
	DoGPIOInit     bool `toml:"DoGpioInit"`
}

type HTTPServer struct {
	Addr               string `toml:"Addr"`
	Port               int    `toml:"Port"`
	Enabled            bool   `toml:"Enabled"`
	InfoDisplaySeconds int    `toml:"InfoDisplaySeconds"`
}

type ClockConfig struct {
	DefaultMode string `toml:"DefaultMode"`
	Color1      string `toml:"Color1"`
	Color2      string `toml:"Color2"`
}

func Default() Config {
	return Config{
		Hardware: HardwareConfig{
			Rows: 32, Cols: 64, ChainLength: 1, Parallel: 1,
			PWMBits: 11, PWMLSBNanoseconds: 130, Brightness: 100,
			HardwareMapping: "regular",
		},
		Runtime: RuntimeOptions{
			GPIOSlowdown: 1, Daemon: 0, DropPrivileges: -1, DoGPIOInit: true,
		},
		HTTP:  HTTPServer{Addr: "detect", Port: 8080, Enabled: true, InfoDisplaySeconds: 5},
		Clock: ClockConfig{DefaultMode: string(matrixclock.Simple)},
	}
}

// Load decodes path over the defaults, so omitted TOML keys keep documented
// defaults. Unknown keys are rejected to catch configuration typos.
func Load(path string) (Config, error) {
	result := Default()
	metadata, err := toml.DecodeFile(path, &result)
	if err != nil {
		return Config{}, fmt.Errorf("decode config %q: %w", path, err)
	}
	if undecoded := metadata.Undecoded(); len(undecoded) > 0 {
		keys := make([]string, len(undecoded))
		for index, key := range undecoded {
			keys[index] = key.String()
		}
		return Config{}, fmt.Errorf("unknown configuration keys: %s", strings.Join(keys, ", "))
	}
	if err := result.Validate(); err != nil {
		return Config{}, fmt.Errorf("validate config %q: %w", path, err)
	}
	return result, nil
}

func (c Config) Validate() error {
	hardware := c.Hardware
	switch {
	case hardware.Rows <= 0:
		return errors.New("HardwareConfig.Rows must be positive")
	case hardware.Cols <= 0:
		return errors.New("HardwareConfig.Cols must be positive")
	case hardware.ChainLength <= 0:
		return errors.New("HardwareConfig.ChainLength must be positive")
	case hardware.Parallel <= 0:
		return errors.New("HardwareConfig.Parallel must be positive")
	case hardware.PWMBits < 1 || hardware.PWMBits > 11:
		return errors.New("HardwareConfig.PWMBits must be between 1 and 11")
	case hardware.PWMLSBNanoseconds <= 0:
		return errors.New("HardwareConfig.PWMLSBNanoseconds must be positive")
	case hardware.PWMDitherBits < 0 || hardware.PWMDitherBits > 2:
		return errors.New("HardwareConfig.PWMDitherBits must be between 0 and 2")
	case hardware.Brightness < 1 || hardware.Brightness > 100:
		return errors.New("HardwareConfig.Brightness must be between 1 and 100")
	case hardware.ScanMode < 0 || hardware.ScanMode > 1:
		return errors.New("HardwareConfig.ScanMode must be 0 or 1")
	case hardware.HardwareMapping == "":
		return errors.New("HardwareConfig.HardwareMapping must not be empty")
	case hardware.LimitRefreshRateHz < 0:
		return errors.New("HardwareConfig.LimitRefreshRateHz must not be negative")
	case hardware.Multiplexing < 0:
		return errors.New("HardwareConfig.Multiplexing must not be negative")
	}

	runtime := c.Runtime
	if runtime.GPIOSlowdown < 0 || runtime.GPIOSlowdown > 60 {
		return errors.New("RuntimeOptions.GpioSlowdown must be between 0 and 60")
	}

	if c.HTTP.Port < 1 || c.HTTP.Port > 65535 {
		return errors.New("HTTPserver.Port must be between 1 and 65535")
	}
	if c.HTTP.InfoDisplaySeconds < 0 || c.HTTP.InfoDisplaySeconds > 60 {
		return errors.New("HTTPserver.InfoDisplaySeconds must be between 0 and 60")
	}
	if _, err := c.HTTP.ListenAddress(); err != nil {
		return err
	}
	mode, err := matrixclock.ParseMode(c.Clock.DefaultMode)
	if err != nil {
		return fmt.Errorf("Clock.DefaultMode: %w", err)
	}
	if _, err := matrixclock.ResolvePalette(mode, c.Clock.Color1, c.Clock.Color2); err != nil {
		return fmt.Errorf("Clock: %w", err)
	}
	return nil
}

func (c HTTPServer) ListenAddress() (string, error) {
	addr := strings.TrimSpace(c.Addr)
	if addr == "" || strings.EqualFold(addr, "detect") {
		return ":" + strconv.Itoa(c.Port), nil
	}
	if strings.Contains(addr, "://") {
		return "", errors.New("HTTPserver.Addr must be a host or IP address, not a URL")
	}
	return net.JoinHostPort(addr, strconv.Itoa(c.Port)), nil
}
