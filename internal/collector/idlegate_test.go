package collector

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/xsaveopt/intel_gpu_exporter/internal/discovery"
)

type countingSource struct {
	calls int
	desc  *prometheus.Desc
}

func (c *countingSource) Name() string                        { return "counting" }
func (c *countingSource) Available(gpus []discovery.GPU) bool { return true }
func (c *countingSource) Update(ctx context.Context, ch chan<- prometheus.Metric) error {
	c.calls++
	ch <- prometheus.MustNewConstMetric(c.desc, prometheus.GaugeValue, float64(c.calls))
	return nil
}

func TestIdleGateReplaysWhileSuspended(t *testing.T) {
	g := i915GPU(t, nil, map[string]string{"power/runtime_status": "active\n"})
	src := &countingSource{desc: prometheus.NewDesc("intel_gpu_test_calls", "test", nil, nil)}
	gate := NewIdleGate(src, []discovery.GPU{g})
	assertSamples(t, gate, []string{`intel_gpu_test_calls 1`})
	if err := os.WriteFile(filepath.Join(g.DevicePath, "power", "runtime_status"), []byte("suspended\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	assertSamples(t, gate, []string{`intel_gpu_test_calls 1`})
	if src.calls != 1 {
		t.Fatalf("suspended GPU must not be read, calls = %d", src.calls)
	}
	if err := os.WriteFile(filepath.Join(g.DevicePath, "power", "runtime_status"), []byte("active\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	assertSamples(t, gate, []string{`intel_gpu_test_calls 2`})
}

func TestIdleGateReadsOnceEvenIfSuspended(t *testing.T) {
	g := i915GPU(t, nil, map[string]string{"power/runtime_status": "suspended\n"})
	src := &countingSource{desc: prometheus.NewDesc("intel_gpu_test_calls", "test", nil, nil)}
	assertSamples(t, NewIdleGate(src, []discovery.GPU{g}), []string{`intel_gpu_test_calls 1`})
}
