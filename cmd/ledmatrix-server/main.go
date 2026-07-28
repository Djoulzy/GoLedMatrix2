package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"sort"
	"syscall"
	"time"

	appconfig "github.com/Djoulzy/GoLedMatrix2/internal/config"
	"github.com/Djoulzy/GoLedMatrix2/internal/display"
	"github.com/Djoulzy/GoLedMatrix2/internal/frame"
	"github.com/Djoulzy/GoLedMatrix2/internal/render"
	"github.com/Djoulzy/GoLedMatrix2/internal/server"
	"github.com/Djoulzy/GoLedMatrix2/internal/technical"
)

type options struct {
	backend              string
	listen               string
	config               appconfig.Config
	check                bool
	simulationPixelPitch int
}

func main() {
	cfg, err := parseFlags()
	if err == nil && cfg.check {
		fmt.Println("configuration valid")
		return
	}
	if err == nil {
		err = run(cfg)
	}
	if err != nil {
		slog.Error("server stopped", "error", err)
		os.Exit(1)
	}
}

func parseFlags() (options, error) {
	defaults := appconfig.Default()
	configPath := flag.String("config", "", "TOML configuration file")
	checkConfig := flag.Bool("check-config", false, "validate configuration and exit")
	backend := flag.String("backend", "memory", "display backend: memory, simulation, or rpi")
	simulationPixelPitch := flag.Int("simulation-pixel-pitch", 6, "simulated LED size in screen pixels (minimum 2)")
	listen := flag.String("listen", "", "override HTTP listen address (for example :8080)")

	rows := flag.Int("rows", defaults.Hardware.Rows, "rows per physical panel")
	cols := flag.Int("cols", defaults.Hardware.Cols, "columns per physical panel")
	chain := flag.Int("chain", defaults.Hardware.ChainLength, "number of panels chained horizontally")
	parallel := flag.Int("parallel", defaults.Hardware.Parallel, "number of parallel chains")
	pwmBits := flag.Int("pwm-bits", defaults.Hardware.PWMBits, "native PWM color depth")
	pwmLSB := flag.Int("pwm-lsb-ns", defaults.Hardware.PWMLSBNanoseconds, "native PWM LSB duration in nanoseconds")
	pwmDither := flag.Int("pwm-dither-bits", defaults.Hardware.PWMDitherBits, "native PWM time dithering bits")
	brightness := flag.Int("brightness", defaults.Hardware.Brightness, "panel brightness (1-100)")
	scanMode := flag.Int("scan-mode", defaults.Hardware.ScanMode, "scan mode: 0 progressive, 1 interlaced")
	mapping := flag.String("hardware-mapping", defaults.Hardware.HardwareMapping, "GPIO hardware mapping")
	showRefresh := flag.Bool("show-refresh", defaults.Hardware.ShowRefreshRate, "print native refresh rate")
	inverseColors := flag.Bool("inverse-colors", defaults.Hardware.InverseColors, "invert panel colors")
	disablePulsing := flag.Bool("disable-hardware-pulsing", defaults.Hardware.DisableHardwarePulsing, "disable hardware pulse generation")
	pixelMapper := flag.String("pixel-mapper", defaults.Hardware.PixelMapperConfig, "native pixel mapper configuration")
	limitRefresh := flag.Int("limit-refresh", defaults.Hardware.LimitRefreshRateHz, "maximum native refresh rate in Hz; 0 disables the limit")
	multiplexing := flag.Int("multiplexing", defaults.Hardware.Multiplexing, "native multiplexing mode")

	gpioSlowdown := flag.Int("gpio-slowdown", defaults.Runtime.GPIOSlowdown, "GPIO slowdown (0-60)")

	httpAddr := flag.String("http-addr", defaults.HTTP.Addr, `HTTP host/IP or "detect"`)
	httpPort := flag.Int("http-port", defaults.HTTP.Port, "HTTP port")
	httpEnabled := flag.Bool("http-enabled", defaults.HTTP.Enabled, "enable the HTTP server")
	infoDisplaySeconds := flag.Int("info-display-seconds", defaults.HTTP.InfoDisplaySeconds, "seconds to show technical information; 0 disables it")
	flag.Parse()

	settings := defaults
	var err error
	if *configPath != "" {
		settings, err = appconfig.Load(*configPath)
		if err != nil {
			return options{}, err
		}
	}

	flag.Visit(func(item *flag.Flag) {
		switch item.Name {
		case "rows":
			settings.Hardware.Rows = *rows
		case "cols":
			settings.Hardware.Cols = *cols
		case "chain":
			settings.Hardware.ChainLength = *chain
		case "parallel":
			settings.Hardware.Parallel = *parallel
		case "pwm-bits":
			settings.Hardware.PWMBits = *pwmBits
		case "pwm-lsb-ns":
			settings.Hardware.PWMLSBNanoseconds = *pwmLSB
		case "pwm-dither-bits":
			settings.Hardware.PWMDitherBits = *pwmDither
		case "brightness":
			settings.Hardware.Brightness = *brightness
		case "scan-mode":
			settings.Hardware.ScanMode = *scanMode
		case "hardware-mapping":
			settings.Hardware.HardwareMapping = *mapping
		case "show-refresh":
			settings.Hardware.ShowRefreshRate = *showRefresh
		case "inverse-colors":
			settings.Hardware.InverseColors = *inverseColors
		case "disable-hardware-pulsing":
			settings.Hardware.DisableHardwarePulsing = *disablePulsing
		case "pixel-mapper":
			settings.Hardware.PixelMapperConfig = *pixelMapper
		case "limit-refresh":
			settings.Hardware.LimitRefreshRateHz = *limitRefresh
		case "multiplexing":
			settings.Hardware.Multiplexing = *multiplexing
		case "gpio-slowdown":
			settings.Runtime.GPIOSlowdown = *gpioSlowdown
		case "http-addr":
			settings.HTTP.Addr = *httpAddr
		case "http-port":
			settings.HTTP.Port = *httpPort
		case "http-enabled":
			settings.HTTP.Enabled = *httpEnabled
		case "info-display-seconds":
			settings.HTTP.InfoDisplaySeconds = *infoDisplaySeconds
		}
	})
	if err := settings.Validate(); err != nil {
		return options{}, err
	}
	return options{
		backend: *backend, listen: *listen, config: settings, check: *checkConfig,
		simulationPixelPitch: *simulationPixelPitch,
	}, nil
}

func run(cfg options) error {
	if cfg.backend == "simulation" {
		return runSimulation(cfg)
	}
	target, backendName, err := openDisplay(cfg)
	if err != nil {
		return err
	}
	return serve(cfg, target, backendName, nil)
}

func runSimulation(cfg options) error {
	width, height, pixelPitch, err := simulationGeometry(cfg)
	if err != nil {
		return err
	}
	return display.RunSimulator(width, height, pixelPitch, func(target display.Display, windowClosed <-chan struct{}) error {
		return serve(cfg, target, "simulation", windowClosed)
	})
}

func serve(cfg options, target display.Display, backendName string, externalDone <-chan struct{}) error {
	defer target.Close()

	signalContext, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithCancel(signalContext)
	defer cancel()
	if externalDone != nil {
		go func() {
			select {
			case <-externalDone:
				cancel()
			case <-ctx.Done():
			}
		}()
	}

	width, height := target.Geometry()
	renderer := render.New(target)
	go renderer.Run(ctx)

	if !cfg.config.HTTP.Enabled {
		slog.Info("HTTP server disabled; matrix process waiting for shutdown",
			"backend", backendName, "width", width, "height", height)
		<-ctx.Done()
		return nil
	}

	listen := cfg.listen
	var err error
	if listen == "" {
		listen, err = cfg.config.HTTP.ListenAddress()
		if err != nil {
			return err
		}
	}
	baseURLs := advertisedBaseURLs(listen)
	apiOptions := make([]server.Option, 0, 1)
	if seconds := cfg.config.HTTP.InfoDisplaySeconds; seconds > 0 {
		apiOptions = append(apiOptions, server.WithTechnicalDisplay(
			baseURLs,
			time.Duration(seconds)*time.Second,
			func(info server.Info) (frame.Frame, error) {
				baseURL := ""
				if len(info.BaseURLs) > 0 {
					baseURL = info.BaseURLs[0]
				}
				return technical.Render(technical.State{
					Backend: info.Backend, BaseURL: baseURL,
					Width: info.Width, Height: info.Height,
					Uptime:   time.Duration(info.UptimeSeconds) * time.Second,
					Protocol: info.ProtocolVersion,
				})
			},
		))
	}
	api, err := server.New(width, height, backendName, renderer, apiOptions...)
	if err != nil {
		return err
	}
	httpServer := &http.Server{
		Addr:              listen,
		Handler:           api.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	listener, err := net.Listen("tcp", listen)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", listen, err)
	}
	errs := make(chan error, 1)
	go func() {
		slog.Info("matrix server listening", "address", listen, "backend", backendName, "width", width, "height", height)
		errs <- httpServer.Serve(listener)
	}()
	if cfg.config.HTTP.InfoDisplaySeconds > 0 {
		if err := api.ShowTechnicalInfo(); err != nil {
			slog.Warn("unable to display startup technical information", "error", err)
		}
	}

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return httpServer.Shutdown(shutdownCtx)
	case err := <-errs:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}

func advertisedBaseURLs(listen string) []string {
	host, port, err := net.SplitHostPort(listen)
	if err != nil {
		return nil
	}
	if host != "" && net.ParseIP(host) == nil {
		return []string{"http://" + net.JoinHostPort(host, port)}
	}
	if parsed := net.ParseIP(host); parsed != nil && !parsed.IsUnspecified() {
		return []string{"http://" + net.JoinHostPort(host, port)}
	}

	var ipv4, ipv6 []string
	addresses, _ := net.InterfaceAddrs()
	for _, address := range addresses {
		ip, _, err := net.ParseCIDR(address.String())
		if err != nil || !ip.IsGlobalUnicast() || ip.IsLoopback() {
			continue
		}
		url := "http://" + net.JoinHostPort(ip.String(), port)
		if ip.To4() != nil {
			ipv4 = append(ipv4, url)
		} else {
			ipv6 = append(ipv6, url)
		}
	}
	sort.Strings(ipv4)
	sort.Strings(ipv6)
	result := append(ipv4, ipv6...)
	if len(result) == 0 {
		result = []string{"http://" + net.JoinHostPort("127.0.0.1", port)}
	}
	return result
}

func openDisplay(cfg options) (display.Display, string, error) {
	hardware := cfg.config.Hardware
	width := hardware.Cols * hardware.ChainLength
	height := hardware.Rows * hardware.Parallel
	switch cfg.backend {
	case "memory":
		target, err := display.NewMemory(width, height)
		return target, "memory", err
	case "rpi":
		target, err := display.NewRPI(rpiConfig(hardware, cfg.config.Runtime))
		return target, "rpi", err
	default:
		return nil, "", fmt.Errorf("unknown backend %q (want memory, simulation, or rpi)", cfg.backend)
	}
}

func simulationGeometry(cfg options) (width, height, pixelPitch int, err error) {
	hardware := cfg.config.Hardware
	width, height, err = display.MappedGeometry(
		hardware.Cols,
		hardware.Rows,
		hardware.ChainLength,
		hardware.Parallel,
		hardware.PixelMapperConfig,
	)
	return width, height, cfg.simulationPixelPitch, err
}

func rpiConfig(hardware appconfig.HardwareConfig, runtime appconfig.RuntimeOptions) display.RPIConfig {
	return display.RPIConfig{
		Rows: hardware.Rows, Cols: hardware.Cols,
		ChainLength: hardware.ChainLength, Parallel: hardware.Parallel,
		PWMBits: hardware.PWMBits, PWMLSBNanoseconds: hardware.PWMLSBNanoseconds,
		PWMDitherBits: hardware.PWMDitherBits, Brightness: hardware.Brightness,
		ScanMode: hardware.ScanMode, HardwareMapping: hardware.HardwareMapping,
		ShowRefreshRate: hardware.ShowRefreshRate, InverseColors: hardware.InverseColors,
		DisableHardwarePulsing: hardware.DisableHardwarePulsing,
		PixelMapperConfig:      hardware.PixelMapperConfig,
		LimitRefreshRateHz:     hardware.LimitRefreshRateHz, Multiplexing: hardware.Multiplexing,
		GPIOSlowdown: runtime.GPIOSlowdown,
	}
}
