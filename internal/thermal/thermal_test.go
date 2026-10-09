package thermal

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"
)

func zone(t *testing.T, root, sensor, name, temperature string) string {
	t.Helper()
	dir := filepath.Join(root, sensor)
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	for filename, content := range map[string]string{"type": name, "temp": temperature} {
		if err := os.WriteFile(filepath.Join(dir, filename), []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestDetectThermalZones(t *testing.T) {
	root := t.TempDir()
	zone(t, root, "thermal_zone0", "other-sensor\n", "0\n")
	zone(t, root, "thermal_zone7", " cpu-thermal \n", "48562\n")
	zone(t, root, "thermal_zone9", "cold-sensor", "-10500")
	zone(t, root, "cooling_device0", "ignored", "99999")
	zone(t, root, "thermal_zone_bad", "ignored", "99999")
	got := newCollector(root).Snapshot()
	want := []Temperature{
		{Sensor: "thermal_zone0", Name: "other-sensor", Celsius: 0},
		{Sensor: "thermal_zone7", Name: "cpu-thermal", Celsius: 48.562},
		{Sensor: "thermal_zone9", Name: "cold-sensor", Celsius: -10.5},
	}
	if len(got.Temperatures) != len(want) {
		t.Fatalf("temperatures = %+v", got)
	}
	for i := range want {
		if got.Temperatures[i] != want[i] {
			t.Fatalf("reading = %+v, want %+v", got.Temperatures[i], want[i])
		}
	}
	if got.SampledAt == nil || time.Since(*got.SampledAt) > time.Second {
		t.Fatalf("initial sample time = %v", got.SampledAt)
	}
}

func TestInvalidSensorsDoNotHideHealthySensors(t *testing.T) {
	root := t.TempDir()
	zone(t, root, "thermal_zone0", "cpu-thermal", "42000")
	for i, value := range []string{"", "NaN", "inf", "48.5", "broken", "99999999999999999999", "-274000"} {
		zone(t, root, "thermal_zone"+strconv.Itoa(i+1), "bad-sensor", value)
	}
	zone(t, root, "thermal_zone8", " \n", "42000")
	missingType := zone(t, root, "thermal_zone9", "sensor", "42000")
	if err := os.Remove(filepath.Join(missingType, "type")); err != nil {
		t.Fatal(err)
	}
	missingTemp := zone(t, root, "thermal_zone10", "sensor", "42000")
	if err := os.Remove(filepath.Join(missingTemp, "temp")); err != nil {
		t.Fatal(err)
	}
	// A directory instead of a readable temperature file simulates read failure
	// even when the tests run as root.
	if err := os.Mkdir(filepath.Join(missingTemp, "temp"), 0700); err != nil {
		t.Fatal(err)
	}
	got := newCollector(root).Snapshot()
	if len(got.Temperatures) != 1 || got.Temperatures[0].Celsius != 42 {
		t.Fatalf("partial sample = %+v", got)
	}
}

func TestSysfsSymlinks(t *testing.T) {
	root := t.TempDir()
	device := zone(t, t.TempDir(), "device", "cpu-thermal", "51000")
	if err := os.Symlink(device, filepath.Join(root, "thermal_zone3")); err != nil {
		t.Fatal(err)
	}
	got := newCollector(root).Snapshot()
	if len(got.Temperatures) != 1 || got.Temperatures[0].Sensor != "thermal_zone3" {
		t.Fatalf("symlinked sensor = %+v", got)
	}
}

func TestMissingOrUnsupportedSensors(t *testing.T) {
	for _, root := range []string{"", t.TempDir(), filepath.Join(t.TempDir(), "missing")} {
		collector := newCollector(root)
		got := collector.Snapshot()
		if got.Temperatures == nil || len(got.Temperatures) != 0 {
			t.Fatalf("root %q: temperatures = %+v", root, got)
		}
		if root == "" {
			body, err := json.Marshal(got)
			if err != nil || string(body) != `{"temperatures":[]}` {
				t.Fatalf("unsupported platform JSON = %s, %v", body, err)
			}
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	newCollector("").Run(ctx)
}

func TestSnapshotOwnershipAndRefresh(t *testing.T) {
	root := t.TempDir()
	dir := zone(t, root, "thermal_zone0", "cpu-thermal", "42000")
	collector := newCollector(root)
	first := collector.Snapshot()
	first.Temperatures[0].Celsius = 999
	*first.SampledAt = time.Time{}
	if got := collector.Snapshot(); got.Temperatures[0].Celsius != 42 || got.SampledAt.IsZero() {
		t.Fatalf("snapshot changed collector state: %+v", got)
	}
	zone(t, root, "thermal_zone0", "cpu-thermal", "53000")
	if got := collector.Snapshot(); got.Temperatures[0].Celsius != 42 {
		t.Fatalf("snapshot read sysfs instead of cached value: %+v", got)
	}
	collector.sample()
	if got := collector.Snapshot(); got.Temperatures[0].Celsius != 53 {
		t.Fatalf("refreshed sample = %+v", got)
	}
	if err := os.Remove(filepath.Join(dir, "temp")); err != nil {
		t.Fatal(err)
	}
	collector.sample()
	if got := collector.Snapshot(); len(got.Temperatures) != 0 {
		t.Fatalf("retained stale readings: %+v", got)
	}
}

func TestPeriodicSamplingAndShutdown(t *testing.T) {
	root := t.TempDir()
	zone(t, root, "thermal_zone0", "cpu-thermal", "42000")
	collector := newCollector(root)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() {
		collector.run(ctx, 5*time.Millisecond)
		close(done)
	}()
	zone(t, root, "thermal_zone0", "cpu-thermal", "54000")
	deadline := time.Now().Add(time.Second)
	for {
		got := collector.Snapshot()
		if len(got.Temperatures) == 1 && got.Temperatures[0].Celsius == 54 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("periodic sampling did not refresh temperature")
		}
		time.Sleep(time.Millisecond)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("collector did not stop on shutdown")
	}
	zone(t, root, "thermal_zone0", "cpu-thermal", "56000")
	if got := collector.Snapshot(); got.Temperatures[0].Celsius != 54 {
		t.Fatalf("unexpected sample after shutdown: %+v", got)
	}
}

func TestConcurrentSnapshotsAndSamples(t *testing.T) {
	root := t.TempDir()
	zone(t, root, "thermal_zone0", "cpu-thermal", "42000")
	collector := newCollector(root)
	var workers sync.WaitGroup
	for i := 0; i < 8; i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for j := 0; j < 30; j++ {
				got := collector.Snapshot()
				got.Temperatures[0].Name = "modified"
				*got.SampledAt = time.Time{}
				collector.sample()
			}
		}()
	}
	workers.Wait()
	if got := collector.Snapshot(); got.Temperatures[0].Name != "cpu-thermal" || got.SampledAt.IsZero() {
		t.Fatalf("concurrent callers mutated state: %+v", got)
	}
}
