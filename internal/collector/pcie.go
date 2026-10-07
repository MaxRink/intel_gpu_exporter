package collector

import (
	"context"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/xsaveopt/intel_gpu_exporter/internal/discovery"
	"github.com/xsaveopt/intel_gpu_exporter/internal/sysutil"
)

type PCIe struct {
	gpus []discovery.GPU

	curSpeed *prometheus.Desc
	curWidth *prometheus.Desc
	maxSpeed *prometheus.Desc
	maxWidth *prometheus.Desc
	curGen   *prometheus.Desc
	maxGen   *prometheus.Desc
	upSpeed  *prometheus.Desc
	upWidth  *prometheus.Desc
	upMaxSp  *prometheus.Desc
	upMaxW   *prometheus.Desc
}

func NewPCIe(gpus []discovery.GPU) *PCIe {
	lbls := CommonLabels()
	d := func(name, help string) *prometheus.Desc {
		return prometheus.NewDesc(prometheus.BuildFQName(Namespace, "pcie", name), help, lbls, nil)
	}
	u := func(name, help string) *prometheus.Desc {
		return prometheus.NewDesc(prometheus.BuildFQName(Namespace, "pcie", name), help, append(CommonLabels(), "hop", "port"), nil)
	}
	return &PCIe{
		gpus:     gpus,
		curSpeed: d("current_link_speed_gtps", "Current PCIe link speed in GT/s."),
		curWidth: d("current_link_width", "Current PCIe link width (lanes)."),
		maxSpeed: d("max_link_speed_gtps", "Maximum supported PCIe link speed in GT/s."),
		maxWidth: d("max_link_width", "Maximum supported PCIe link width (lanes)."),
		curGen:   d("current_generation", "Current PCIe generation (1..6) derived from link speed."),
		maxGen:   d("max_generation", "Maximum supported PCIe generation."),
		upSpeed:  u("upstream_current_link_speed_gtps", "Current link speed of each upstream port (hop 1 = the GPU's parent; discrete cards sit behind an internal switch, so the slot link is a higher hop)."),
		upWidth:  u("upstream_current_link_width", "Current link width of each upstream port."),
		upMaxSp:  u("upstream_max_link_speed_gtps", "Maximum link speed of each upstream port."),
		upMaxW:   u("upstream_max_link_width", "Maximum link width of each upstream port."),
	}
}

func (c *PCIe) Name() string { return "pcie" }

func (c *PCIe) Available(gpus []discovery.GPU) bool { return len(gpus) > 0 }

func (c *PCIe) Update(ctx context.Context, ch chan<- prometheus.Metric) error {
	for _, g := range c.gpus {
		lv := LabelValues(g)
		readSpeed := func(file string) (gtps float64, gen float64, ok bool) {
			s, err := sysutil.ReadString(filepath.Join(g.DevicePath, file))
			if err != nil {
				return 0, 0, false
			}
			return parseLinkSpeed(s)
		}
		if speed, gen, ok := readSpeed("current_link_speed"); ok {
			ch <- prometheus.MustNewConstMetric(c.curSpeed, prometheus.GaugeValue, speed, lv...)
			ch <- prometheus.MustNewConstMetric(c.curGen, prometheus.GaugeValue, gen, lv...)
		}
		if speed, gen, ok := readSpeed("max_link_speed"); ok {
			ch <- prometheus.MustNewConstMetric(c.maxSpeed, prometheus.GaugeValue, speed, lv...)
			ch <- prometheus.MustNewConstMetric(c.maxGen, prometheus.GaugeValue, gen, lv...)
		}
		if v, err := sysutil.ReadFloat64(filepath.Join(g.DevicePath, "current_link_width")); err == nil {
			ch <- prometheus.MustNewConstMetric(c.curWidth, prometheus.GaugeValue, v, lv...)
		}
		if v, err := sysutil.ReadFloat64(filepath.Join(g.DevicePath, "max_link_width")); err == nil {
			ch <- prometheus.MustNewConstMetric(c.maxWidth, prometheus.GaugeValue, v, lv...)
		}
		if g.DevicePath == "" {
			continue
		}
		dev, err := filepath.EvalSymlinks(g.DevicePath)
		if err != nil {
			continue
		}
		for hop, p := 1, filepath.Dir(dev); hop <= 8; hop, p = hop+1, filepath.Dir(p) {
			sp, err := sysutil.ReadString(filepath.Join(p, "current_link_speed"))
			if err != nil {
				break
			}
			ulv := append(append([]string{}, lv...), strconv.Itoa(hop), filepath.Base(p))
			if v, _, ok := parseLinkSpeed(sp); ok {
				ch <- prometheus.MustNewConstMetric(c.upSpeed, prometheus.GaugeValue, v, ulv...)
			}
			if s, err := sysutil.ReadString(filepath.Join(p, "max_link_speed")); err == nil {
				if v, _, ok := parseLinkSpeed(s); ok {
					ch <- prometheus.MustNewConstMetric(c.upMaxSp, prometheus.GaugeValue, v, ulv...)
				}
			}
			if v, err := sysutil.ReadFloat64(filepath.Join(p, "current_link_width")); err == nil {
				ch <- prometheus.MustNewConstMetric(c.upWidth, prometheus.GaugeValue, v, ulv...)
			}
			if v, err := sysutil.ReadFloat64(filepath.Join(p, "max_link_width")); err == nil {
				ch <- prometheus.MustNewConstMetric(c.upMaxW, prometheus.GaugeValue, v, ulv...)
			}
		}
	}
	return nil
}

func parseLinkSpeed(s string) (gtps float64, gen float64, ok bool) {
	fields := strings.Fields(s)
	if len(fields) == 0 {
		return 0, 0, false
	}
	v, err := strconv.ParseFloat(fields[0], 64)
	if err != nil {
		return 0, 0, false
	}
	switch v {
	case 2.5:
		gen = 1
	case 5.0:
		gen = 2
	case 8.0:
		gen = 3
	case 16.0:
		gen = 4
	case 32.0:
		gen = 5
	case 64.0:
		gen = 6
	}
	return v, gen, true
}
