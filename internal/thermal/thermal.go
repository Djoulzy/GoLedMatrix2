// Package thermal samples Linux thermal zones without external dependencies.
package thermal

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

const SampleInterval = 5 * time.Second

type Temperature struct {
	Sensor  string  `json:"sensor"`
	Name    string  `json:"name"`
	Celsius float64 `json:"celsius"`
}

type Stats struct {
	Temperatures []Temperature `json:"temperatures"`
	SampledAt    *time.Time    `json:"sampled_at,omitempty"`
}

// Collector keeps a cached snapshot so HTTP requests never read sysfs.
type Collector struct {
	root  string
	mu    sync.RWMutex
	stats Stats
}

// New takes an initial sample. Unsupported platforms report an empty list.
func New() *Collector {
	root := ""
	if runtime.GOOS == "linux" {
		root = "/sys/class/thermal"
	}
	return newCollector(root)
}

func newCollector(root string) *Collector {
	c := &Collector{root: root, stats: Stats{Temperatures: []Temperature{}}}
	if root != "" {
		c.sample()
	}
	return c
}

// Run refreshes the snapshot until shutdown. Call it in a background goroutine.
func (c *Collector) Run(ctx context.Context) {
	c.run(ctx, SampleInterval)
}

func (c *Collector) run(ctx context.Context, interval time.Duration) {
	if c.root == "" {
		return
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			c.sample()
		}
	}
}

// Snapshot returns an independent copy safe for concurrent callers.
func (c *Collector) Snapshot() Stats {
	c.mu.RLock()
	defer c.mu.RUnlock()
	stats := c.stats
	stats.Temperatures = append([]Temperature{}, stats.Temperatures...)
	if stats.SampledAt != nil {
		sampledAt := *stats.SampledAt
		stats.SampledAt = &sampledAt
	}
	return stats
}

func (c *Collector) sample() {
	temperatures := readTemperatures(c.root)
	now := time.Now().UTC()
	c.mu.Lock()
	c.stats = Stats{Temperatures: temperatures, SampledAt: &now}
	c.mu.Unlock()
}

func readTemperatures(root string) []Temperature {
	temperatures := []Temperature{}
	entries, err := os.ReadDir(root)
	if err != nil {
		return temperatures
	}
	for _, entry := range entries {
		sensor := entry.Name()
		if !strings.HasPrefix(sensor, "thermal_zone") {
			continue
		}
		if index, err := strconv.Atoi(strings.TrimPrefix(sensor, "thermal_zone")); err != nil || index < 0 {
			continue
		}
		dir := filepath.Join(root, sensor)
		rawName, err := os.ReadFile(filepath.Join(dir, "type"))
		if err != nil {
			continue
		}
		name := strings.TrimSpace(string(rawName))
		if name == "" {
			continue
		}
		rawTemp, err := os.ReadFile(filepath.Join(dir, "temp"))
		if err != nil {
			continue
		}
		millidegrees, err := strconv.ParseInt(strings.TrimSpace(string(rawTemp)), 10, 64)
		if err != nil || millidegrees < -273150 {
			continue
		}
		temperatures = append(temperatures, Temperature{
			Sensor: sensor, Name: name, Celsius: float64(millidegrees) / 1000,
		})
	}
	return temperatures
}
