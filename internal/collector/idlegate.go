package collector

import (
	"context"
	"path/filepath"
	"sync"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/xsaveopt/intel_gpu_exporter/internal/discovery"
	"github.com/xsaveopt/intel_gpu_exporter/internal/sysutil"
)

// runtimeSuspended reports whether the GPU's PCI device is runtime-suspended.
// Reading power/runtime_status never resumes the device.
func runtimeSuspended(g discovery.GPU) bool {
	s, err := sysutil.ReadString(filepath.Join(g.DevicePath, "power", "runtime_status"))
	return err == nil && s == "suspended"
}

// IdleGate wraps a source whose reads resume a runtime-suspended GPU (hwmon
// energy, i915 rc6/throttle sysfs, PCIe config space, DRM ioctls). While every
// GPU is suspended it replays the last samples instead, so scraping never keeps
// an idle GPU awake.
type IdleGate struct {
	Source
	gpus []discovery.GPU
	mu   sync.Mutex
	last []prometheus.Metric
}

func NewIdleGate(s Source, gpus []discovery.GPU) *IdleGate { return &IdleGate{Source: s, gpus: gpus} }

func (g *IdleGate) allSuspended() bool {
	for _, gpu := range g.gpus {
		if !runtimeSuspended(gpu) {
			return false
		}
	}
	return len(g.gpus) > 0
}

func (g *IdleGate) Update(ctx context.Context, ch chan<- prometheus.Metric) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.last != nil && g.allSuspended() {
		for _, m := range g.last {
			ch <- m
		}
		return nil
	}
	tmp := make(chan prometheus.Metric)
	done := make(chan []prometheus.Metric)
	go func() {
		var ms []prometheus.Metric
		for m := range tmp {
			ms = append(ms, m)
		}
		done <- ms
	}()
	err := g.Source.Update(ctx, tmp)
	close(tmp)
	ms := <-done
	for _, m := range ms {
		ch <- m
	}
	if err == nil {
		g.last = ms
	}
	return err
}
