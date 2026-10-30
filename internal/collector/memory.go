package collector

import (
	"context"
	"path/filepath"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/xsaveopt/intel_gpu_exporter/internal/discovery"
	"github.com/xsaveopt/intel_gpu_exporter/internal/sysutil"
)

type Memory struct {
	gpus []discovery.GPU

	// DevRoot is the /dev mountpoint used to open i915 render nodes.
	DevRoot      string
	queryRegions func(node string) ([]memRegion, error)

	lmemTotal   *prometheus.Desc
	vramTotal   *prometheus.Desc
	regionTotal *prometheus.Desc
	regionFree  *prometheus.Desc
}

func NewMemory(gpus []discovery.GPU) *Memory {
	return &Memory{
		gpus:         gpus,
		DevRoot:      "/dev",
		queryRegions: queryI915MemRegions,
		regionTotal: prometheus.NewDesc(
			prometheus.BuildFQName(Namespace, "memory", "region_total_bytes"),
			"i915 memory region size (DRM_I915_QUERY_MEMORY_REGIONS probed_size); local* is VRAM.",
			append(CommonLabels(), "region"), nil,
		),
		regionFree: prometheus.NewDesc(
			prometheus.BuildFQName(Namespace, "memory", "region_free_bytes"),
			"i915 memory region unallocated_size; only accurate with CAP_PERFMON, otherwise equal to the size.",
			append(CommonLabels(), "region"), nil,
		),
		lmemTotal: prometheus.NewDesc(
			prometheus.BuildFQName(Namespace, "memory", "lmem_total_bytes"),
			"Total local memory exposed by /sys/class/drm/cardN/lmem_total_bytes (discrete cards).",
			CommonLabels(), nil,
		),
		vramTotal: prometheus.NewDesc(
			prometheus.BuildFQName(Namespace, "memory", "vram_total_bytes"),
			"Physical VRAM size from xe sysfs (tile-level).",
			append(CommonLabels(), "tile"), nil,
		),
	}
}

func (c *Memory) Name() string { return "memory" }

func (c *Memory) Available(gpus []discovery.GPU) bool { return len(gpus) > 0 }

func (c *Memory) Update(ctx context.Context, ch chan<- prometheus.Metric) error {
	for _, g := range c.gpus {
		base := LabelValues(g)
		if v, err := sysutil.ReadFloat64(filepath.Join(g.DRMPath, "lmem_total_bytes")); err == nil {
			ch <- prometheus.MustNewConstMetric(c.lmemTotal, prometheus.GaugeValue, v, base...)
		}
		if g.Driver == discovery.DriverI915 {
			c.emitI915Regions(g, base, ch)
		}
		for _, tile := range g.Tiles {
			if tile.Path == "" || tile.Path == g.DevicePath {
				continue
			}
			if v, err := sysutil.ReadFloat64(filepath.Join(tile.Path, "physical_vram_size_bytes")); err == nil {
				ch <- prometheus.MustNewConstMetric(c.vramTotal, prometheus.GaugeValue, v,
					append(base, itoa(tile.Index))...)
			}
		}
	}
	return nil
}

func (c *Memory) emitI915Regions(g discovery.GPU, base []string, ch chan<- prometheus.Metric) {
	node, err := renderNode(c.DevRoot, g.DevicePath)
	if err != nil {
		return
	}
	regions, err := c.queryRegions(node)
	if err != nil {
		return
	}
	for _, r := range regions {
		lv := append(append([]string{}, base...), r.name())
		ch <- prometheus.MustNewConstMetric(c.regionTotal, prometheus.GaugeValue, float64(r.Probed), lv...)
		ch <- prometheus.MustNewConstMetric(c.regionFree, prometheus.GaugeValue, float64(r.Unallocated), lv...)
	}
}
