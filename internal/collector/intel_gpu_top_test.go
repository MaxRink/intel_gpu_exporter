package collector

import (
	"bytes"
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/xsaveopt/intel_gpu_exporter/internal/discovery"
)

const gputopStream = `[
{
	"period": {"duration": 1000.4},
	"frequency": {"requested": 350.0, "actual": 348.5},
	"interrupts": {"count": 17.0},
	"rc6": {"value": 97.25},
	"power": {"GPU": 0.05, "Package": 2.5},
	"imc-bandwidth": {"reads": 197.5, "writes": 57.25},
	"engines": {
		"Render/3D/0": {"busy": 0.0, "sema": 0.0, "wait": 0.0}
	}
}
,
{
	"period": {"duration": 1000.1},
	"frequency": {"requested": 1300.0, "actual": 1250.0},
	"interrupts": {"count": 4096.0},
	"rc6": {"value": 12.5},
	"power": {"GPU": 8.25, "Package": 15.5},
	"imc-bandwidth": {"reads": 1024.0, "writes": 512.5},
	"engines": {
		"Render/3D/0": {"busy": 82.5, "sema": 1.25, "wait": 0.5},
		"Video/0": {"busy": 3.0, "sema": 0.0, "wait": 0.0},
		"VideoEnhance/0": {"busy": 0.0, "sema": 0.0, "wait": 0.0}
	}
}
`

func newTestGPUTop(t *testing.T, binPath string) *IntelGPUTop {
	t.Helper()
	return NewIntelGPUTop(binPath, slog.New(slog.DiscardHandler))
}

func TestIntelGPUTopConsumeKeepsLatestSample(t *testing.T) {
	c := newTestGPUTop(t, "intel_gpu_top")
	c.consume(strings.NewReader(gputopStream))

	if c.latest == nil {
		t.Fatal("consume did not store a sample")
	}
	if got := c.latest.Period.Duration; got != 1000.1 {
		t.Errorf("stored period duration = %v, want the last sample in the stream", got)
	}
	if c.last.Load() == 0 {
		t.Error("consume did not record a sample timestamp")
	}
}

func TestIntelGPUTopUpdate(t *testing.T) {
	c := newTestGPUTop(t, "intel_gpu_top")
	c.consume(strings.NewReader(gputopStream))

	const src = `source="intel_gpu_top"`
	assertSamples(t, c, []string{
		`intel_gpu_gputop_frequency_requested_mhz{` + src + `} 1300`,
		`intel_gpu_gputop_frequency_actual_mhz{` + src + `} 1250`,
		`intel_gpu_gputop_rc6_ratio{` + src + `} 12.5`,
		`intel_gpu_gputop_imc_bandwidth_read_mibps{` + src + `} 1024`,
		`intel_gpu_gputop_imc_bandwidth_write_mibps{` + src + `} 512.5`,
		`intel_gpu_gputop_interrupts_per_second{` + src + `} 4096`,
		`intel_gpu_gputop_power_watts{` + src + `,rail="GPU"} 8.25`,
		`intel_gpu_gputop_power_watts{` + src + `,rail="Package"} 15.5`,
		`intel_gpu_gputop_engine_busy_ratio{` + src + `,engine="Render/3D/0",metric="busy"} 82.5`,
		`intel_gpu_gputop_engine_busy_ratio{` + src + `,engine="Render/3D/0",metric="sema"} 1.25`,
		`intel_gpu_gputop_engine_busy_ratio{` + src + `,engine="Render/3D/0",metric="wait"} 0.5`,
		`intel_gpu_gputop_engine_busy_ratio{` + src + `,engine="Video/0",metric="busy"} 3`,
		`intel_gpu_gputop_engine_busy_ratio{` + src + `,engine="Video/0",metric="sema"} 0`,
		`intel_gpu_gputop_engine_busy_ratio{` + src + `,engine="Video/0",metric="wait"} 0`,
		`intel_gpu_gputop_engine_busy_ratio{` + src + `,engine="VideoEnhance/0",metric="busy"} 0`,
		`intel_gpu_gputop_engine_busy_ratio{` + src + `,engine="VideoEnhance/0",metric="sema"} 0`,
		`intel_gpu_gputop_engine_busy_ratio{` + src + `,engine="VideoEnhance/0",metric="wait"} 0`,
	})
}

func TestIntelGPUTopUpdateEverythingIsAGauge(t *testing.T) {
	c := newTestGPUTop(t, "intel_gpu_top")
	c.consume(strings.NewReader(gputopStream))
	for name, typ := range metricTypes(t, c) {
		if typ != "gauge" {
			t.Errorf("%s type = %q, want gauge", name, typ)
		}
	}
}

func TestIntelGPUTopConsumeTerminatedArray(t *testing.T) {
	c := newTestGPUTop(t, "intel_gpu_top")
	c.consume(strings.NewReader(`[{"frequency":{"requested":800,"actual":790}}]`))
	if c.latest == nil {
		t.Fatal("consume did not store a sample")
	}
	if got := c.latest.Frequency.Actual; got != 790 {
		t.Errorf("actual frequency = %v, want 790", got)
	}
}

func TestIntelGPUTopConsumeRejectsNonArray(t *testing.T) {
	for name, input := range map[string]string{
		"object": `{"frequency":{"actual":1}}`,
		"empty":  ``,
		"junk":   `not json at all`,
	} {
		t.Run(name, func(t *testing.T) {
			c := newTestGPUTop(t, "intel_gpu_top")
			c.consume(strings.NewReader(input))
			if c.latest != nil {
				t.Errorf("consume stored a sample from %q", input)
			}
		})
	}
}

func TestIntelGPUTopConsumeAcceptsUnitKeys(t *testing.T) {
	c := newTestGPUTop(t, "intel_gpu_top")
	c.consume(strings.NewReader(testdataFile(t, "gputop_stream.json")))
	if c.latest == nil {
		t.Fatal("consume dropped every sample of real intel_gpu_top output that carries unit keys")
	}

	const src = `source="intel_gpu_top"`
	assertSamples(t, c, []string{
		`intel_gpu_gputop_frequency_requested_mhz{` + src + `} 1300`,
		`intel_gpu_gputop_frequency_actual_mhz{` + src + `} 1250`,
		`intel_gpu_gputop_rc6_ratio{` + src + `} 12.5`,
		`intel_gpu_gputop_imc_bandwidth_read_mibps{` + src + `} 1024`,
		`intel_gpu_gputop_imc_bandwidth_write_mibps{` + src + `} 512.5`,
		`intel_gpu_gputop_interrupts_per_second{` + src + `} 4096`,
		`intel_gpu_gputop_power_watts{` + src + `,rail="GPU"} 8.25`,
		`intel_gpu_gputop_power_watts{` + src + `,rail="Package"} 15.5`,
		`intel_gpu_gputop_engine_busy_ratio{` + src + `,engine="Render/3D/0",metric="busy"} 82.5`,
		`intel_gpu_gputop_engine_busy_ratio{` + src + `,engine="Render/3D/0",metric="sema"} 1.25`,
		`intel_gpu_gputop_engine_busy_ratio{` + src + `,engine="Render/3D/0",metric="wait"} 0.5`,
		`intel_gpu_gputop_engine_busy_ratio{` + src + `,engine="Video/0",metric="busy"} 3`,
		`intel_gpu_gputop_engine_busy_ratio{` + src + `,engine="Video/0",metric="sema"} 0`,
		`intel_gpu_gputop_engine_busy_ratio{` + src + `,engine="Video/0",metric="wait"} 0`,
		`intel_gpu_gputop_engine_busy_ratio{` + src + `,engine="VideoEnhance/0",metric="busy"} 0`,
		`intel_gpu_gputop_engine_busy_ratio{` + src + `,engine="VideoEnhance/0",metric="sema"} 0`,
		`intel_gpu_gputop_engine_busy_ratio{` + src + `,engine="VideoEnhance/0",metric="wait"} 0`,
	})
}

type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func fakeGPUTopBinary(t *testing.T, body string) (bin, argsFile string) {
	t.Helper()
	dir := t.TempDir()
	bin = filepath.Join(dir, "intel_gpu_top")
	argsFile = filepath.Join(dir, "args")
	script := "#!/bin/sh\necho \"$@\" > \"" + argsFile + "\"\n" + body
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake binary: %v", err)
	}
	return bin, argsFile
}

func waitFor(t *testing.T, timeout time.Duration, cond func() bool) bool {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(20 * time.Millisecond)
	}
	return cond()
}

func TestIntelGPUTopStartStreamsSamples(t *testing.T) {
	bin, argsFile := fakeGPUTopBinary(t,
		"printf '[\\n{\"frequency\":{\"requested\":300,\"actual\":299}}\\n'\n"+
			"echo 'fake stderr line' >&2\n"+
			"exec sleep 60\n")
	var logs lockedBuffer
	c := NewIntelGPUTop(bin, slog.New(slog.NewTextHandler(&logs, nil)))
	if err := c.Start(t.Context()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(c.Stop)

	if !waitFor(t, 5*time.Second, func() bool { return c.last.Load() != 0 }) {
		t.Fatal("no sample consumed from the running binary")
	}
	const src = `source="intel_gpu_top"`
	got := samples(t, c)
	for _, want := range []string{
		`intel_gpu_gputop_frequency_actual_mhz{` + src + `} 299`,
		`intel_gpu_gputop_frequency_requested_mhz{` + src + `} 300`,
	} {
		if !slices.Contains(got, want) {
			t.Errorf("missing %s in %v", want, got)
		}
	}

	if !waitFor(t, 5*time.Second, func() bool { return strings.Contains(logs.String(), "fake stderr line") }) {
		t.Errorf("stderr line was not logged: %s", logs.String())
	}
	b, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatalf("read args: %v", err)
	}
	if got := strings.TrimSpace(string(b)); got != "-J -s 1000" {
		t.Errorf("args = %q, want %q", got, "-J -s 1000")
	}

	c.Stop()
	if !waitFor(t, 5*time.Second, func() bool { return strings.Contains(logs.String(), "intel_gpu_top exited") }) {
		t.Fatalf("Stop did not end the process: %s", logs.String())
	}
	if !strings.Contains(logs.String(), "got_any_sample=true") {
		t.Errorf("exit log does not report the received sample: %s", logs.String())
	}
}

func TestIntelGPUTopStartLogsExitWithoutSample(t *testing.T) {
	bin, _ := fakeGPUTopBinary(t, "echo 'permission denied' >&2\nexit 1\n")
	var logs lockedBuffer
	c := NewIntelGPUTop(bin, slog.New(slog.NewTextHandler(&logs, nil)))
	if err := c.Start(t.Context()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(c.Stop)

	if !waitFor(t, 5*time.Second, func() bool { return strings.Contains(logs.String(), "intel_gpu_top exited") }) {
		t.Fatalf("exit was not logged: %s", logs.String())
	}
	out := logs.String()
	for _, want := range []string{"got_any_sample=false", "exit status 1", "permission denied"} {
		if !strings.Contains(out, want) {
			t.Errorf("logs missing %q: %s", want, out)
		}
	}
	ch := make(chan prometheus.Metric, 8)
	if err := c.Update(context.Background(), ch); err == nil {
		t.Error("Update should fail when the process never produced a sample")
	}
}

func TestIntelGPUTopStartMissingBinary(t *testing.T) {
	c := newTestGPUTop(t, filepath.Join(t.TempDir(), "absent"))
	if err := c.Start(t.Context()); err == nil {
		c.Stop()
		t.Fatal("Start should fail when the binary does not exist")
	}
}

func TestIntelGPUTopUpdateFailsAfterProcessDies(t *testing.T) {
	bin, _ := fakeGPUTopBinary(t, "printf '[\\n{\"frequency\":{\"requested\":300,\"actual\":299}}\\n'\n")
	c := newTestGPUTop(t, bin)
	if err := c.Start(t.Context()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(c.Stop)

	if !waitFor(t, 5*time.Second, func() bool { return c.last.Load() != 0 }) {
		t.Fatal("no sample consumed from the running binary")
	}
	stale := waitFor(t, 10*time.Second, func() bool {
		ch := make(chan prometheus.Metric, 32)
		return c.Update(context.Background(), ch) != nil
	})
	if !stale {
		t.Error("Update keeps serving the last sample long after intel_gpu_top exited")
	}
}

func TestIntelGPUTopUpdateWithoutSample(t *testing.T) {
	c := newTestGPUTop(t, "intel_gpu_top")
	ch := make(chan prometheus.Metric, 8)
	err := c.Update(context.Background(), ch)
	if err == nil {
		t.Fatal("Update should fail before the first sample arrives")
	}
	if len(ch) != 0 {
		t.Errorf("Update emitted %d metrics before the first sample", len(ch))
	}
}

func TestIntelGPUTopAvailable(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "intel_gpu_top")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("write fake binary: %v", err)
	}

	i915 := []discovery.GPU{{Driver: discovery.DriverI915}}
	xe := []discovery.GPU{{Driver: discovery.DriverXe}}

	c := newTestGPUTop(t, bin)
	if c.Name() != "intel_gpu_top" {
		t.Errorf("Name = %q", c.Name())
	}
	if !c.Available(i915) {
		t.Error("Available should be true with the binary present and an i915 device")
	}
	if c.Available(xe) {
		t.Error("Available should be false without an i915 device")
	}

	missing := newTestGPUTop(t, filepath.Join(t.TempDir(), "absent"))
	if missing.Available(i915) {
		t.Error("Available should be false when the binary is missing")
	}
}

func TestIntelGPUTopStopWithoutStart(t *testing.T) {
	newTestGPUTop(t, "intel_gpu_top").Stop()
}
