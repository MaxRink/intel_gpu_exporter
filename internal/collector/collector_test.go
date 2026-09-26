package collector

import (
	"context"
	"errors"
	"log/slog"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"

	"github.com/xsaveopt/intel_gpu_exporter/internal/discovery"
)

type fakeSource struct {
	name      string
	available bool
	err       error
	desc      *prometheus.Desc
	value     float64
	block     time.Duration
}

func (f *fakeSource) Name() string { return f.name }

func (f *fakeSource) Available([]discovery.GPU) bool { return f.available }

func (f *fakeSource) Update(ctx context.Context, ch chan<- prometheus.Metric) error {
	if f.block > 0 {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(f.block):
		}
	}
	if f.desc != nil {
		ch <- prometheus.MustNewConstMetric(f.desc, prometheus.GaugeValue, f.value)
	}
	return f.err
}

func gatherRegistry(t *testing.T, r *Registry) []*dto.MetricFamily {
	t.Helper()
	reg := prometheus.NewRegistry()
	reg.MustRegister(r)
	mfs, err := reg.Gather()
	if err != nil {
		t.Fatalf("gather: %v", err)
	}
	return mfs
}

func registrySamples(t *testing.T, r *Registry) []string {
	t.Helper()
	var out []string
	for _, mf := range gatherRegistry(t, r) {
		for _, m := range mf.GetMetric() {
			v := valueString(m)
			if mf.GetName() == "intel_gpu_scrape_duration_seconds" {
				v = "<duration>"
			}
			out = append(out, mf.GetName()+labelString(m)+" "+v)
		}
	}
	slices.Sort(out)
	return out
}

func testRegistry(t *testing.T, timeout time.Duration, sources ...Source) *Registry {
	t.Helper()
	return NewRegistry(slog.New(slog.DiscardHandler), nil, timeout, sources...)
}

func TestRegistryOnlyKeepsAvailableSources(t *testing.T) {
	r := testRegistry(t, time.Second,
		&fakeSource{name: "yes", available: true},
		&fakeSource{name: "no", available: false},
	)
	if len(r.sources) != 1 || r.sources[0].Name() != "yes" {
		t.Fatalf("active sources = %v, want only the available one", r.sources)
	}
	got := registrySamples(t, r)
	want := []string{
		`intel_gpu_scrape_duration_seconds{source="yes"} <duration>`,
		`intel_gpu_scrape_success{source="yes"} 1`,
	}
	if !slices.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestRegistryScrapeMetrics(t *testing.T) {
	ok := &fakeSource{
		name:      "ok",
		available: true,
		desc:      prometheus.NewDesc("intel_gpu_fake_value", "fake.", nil, nil),
		value:     7,
	}
	bad := &fakeSource{name: "bad", available: true, err: errors.New("boom")}

	got := registrySamples(t, testRegistry(t, time.Second, ok, bad))
	want := []string{
		`intel_gpu_fake_value 7`,
		`intel_gpu_scrape_duration_seconds{source="bad"} <duration>`,
		`intel_gpu_scrape_duration_seconds{source="ok"} <duration>`,
		`intel_gpu_scrape_success{source="bad"} 0`,
		`intel_gpu_scrape_success{source="ok"} 1`,
	}
	if !slices.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestRegistryScrapeMetricsAreGauges(t *testing.T) {
	r := testRegistry(t, time.Second, &fakeSource{name: "ok", available: true})
	for _, mf := range gatherRegistry(t, r) {
		if got := strings.ToLower(mf.GetType().String()); got != "gauge" {
			t.Errorf("%s type = %q, want gauge", mf.GetName(), got)
		}
	}
}

func TestRegistryDescribe(t *testing.T) {
	r := testRegistry(t, time.Second)
	ch := make(chan *prometheus.Desc, 8)
	r.Describe(ch)
	close(ch)

	var got []string
	for d := range ch {
		got = append(got, d.String())
	}
	if len(got) != 2 {
		t.Fatalf("Describe sent %d descriptors, want 2", len(got))
	}
	for _, want := range []string{"intel_gpu_scrape_duration_seconds", "intel_gpu_scrape_success"} {
		if !slices.ContainsFunc(got, func(d string) bool { return strings.Contains(d, want) }) {
			t.Errorf("Describe did not mention %s: %v", want, got)
		}
	}
}

func TestRegistryTimesOutSlowSources(t *testing.T) {
	slow := &fakeSource{name: "slow", available: true, block: time.Minute}
	got := registrySamples(t, testRegistry(t, 10*time.Millisecond, slow))
	want := []string{
		`intel_gpu_scrape_duration_seconds{source="slow"} <duration>`,
		`intel_gpu_scrape_success{source="slow"} 0`,
	}
	if !slices.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

type stuckSource struct {
	name    string
	release chan struct{}
}

func (s *stuckSource) Name() string { return s.name }

func (s *stuckSource) Available([]discovery.GPU) bool { return true }

func (s *stuckSource) Update(context.Context, chan<- prometheus.Metric) error {
	<-s.release
	return nil
}

func TestRegistryCollectReturnsWhenSourceIgnoresContext(t *testing.T) {
	stuck := &stuckSource{name: "stuck", release: make(chan struct{})}
	t.Cleanup(func() { close(stuck.release) })
	fast := &fakeSource{name: "fast", available: true}
	r := testRegistry(t, 20*time.Millisecond, stuck, fast)

	ch := make(chan prometheus.Metric, 64)
	done := make(chan struct{})
	go func() {
		r.Collect(ch)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Collect is still blocked long after the scrape timeout because a source ignored its context")
	}

	var got []string
	for len(ch) > 0 {
		m := <-ch
		var pb dto.Metric
		if err := m.Write(&pb); err != nil {
			t.Fatalf("write metric: %v", err)
		}
		if strings.Contains(m.Desc().String(), "scrape_success") {
			got = append(got, labelString(&pb)+" "+valueString(&pb))
		}
	}
	slices.Sort(got)
	want := []string{`{source="fast"} 1`, `{source="stuck"} 0`}
	if !slices.Equal(got, want) {
		t.Errorf("scrape_success = %v, want %v", got, want)
	}
}

func TestRegistryHealthyWithoutGPUs(t *testing.T) {
	r := NewRegistry(slog.New(slog.DiscardHandler), nil, time.Second)
	if !r.Healthy() {
		t.Error("Healthy() = false with no discovered GPUs, want true")
	}
}

func TestRegistryHealthyWhenDRMPathExists(t *testing.T) {
	r := NewRegistry(slog.New(slog.DiscardHandler), []discovery.GPU{{Card: "card0", DRMPath: t.TempDir()}}, time.Second)
	if !r.Healthy() {
		t.Error("Healthy() = false with a readable DRM path, want true")
	}
}

func TestRegistryDegradedWhenDRMPathGone(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(dir, "card0")
	r := NewRegistry(slog.New(slog.DiscardHandler), []discovery.GPU{{Card: "card0", DRMPath: missing}}, time.Second)
	if r.Healthy() {
		t.Error("Healthy() = true with the discovered GPU's sysfs path gone, want false")
	}
}

func TestRegistryHealthyIfAnyGPUStillPresent(t *testing.T) {
	gpus := []discovery.GPU{
		{Card: "card0", DRMPath: filepath.Join(t.TempDir(), "gone")},
		{Card: "card1", DRMPath: t.TempDir()},
	}
	r := NewRegistry(slog.New(slog.DiscardHandler), gpus, time.Second)
	if !r.Healthy() {
		t.Error("Healthy() = false with one of two GPUs still present, want true")
	}
}

func TestRegistryWithoutSources(t *testing.T) {
	if got := registrySamples(t, testRegistry(t, time.Second)); len(got) != 0 {
		t.Errorf("got %v, want no samples", got)
	}
}

func TestCommonLabels(t *testing.T) {
	want := []string{"card", "pci", "device", "driver"}
	if got := CommonLabels(); !slices.Equal(got, want) {
		t.Errorf("CommonLabels = %v, want %v", got, want)
	}
}

func TestLabelValues(t *testing.T) {
	g := discovery.GPU{
		Card:     "card0",
		PCIAddr:  "0000:00:02.0",
		DeviceID: "0x9a49",
		Driver:   discovery.DriverI915,
	}
	want := []string{"card0", "0000:00:02.0", "0x9a49", "i915"}
	if got := LabelValues(g); !slices.Equal(got, want) {
		t.Errorf("LabelValues = %v, want %v", got, want)
	}
	if len(CommonLabels()) != len(LabelValues(g)) {
		t.Error("CommonLabels and LabelValues have different lengths")
	}
}

func TestNamespace(t *testing.T) {
	if Namespace != "intel_gpu" {
		t.Errorf("Namespace = %q, want intel_gpu", Namespace)
	}
}
