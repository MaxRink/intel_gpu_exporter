package collector

import (
	"context"
	"path/filepath"
	"strings"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/xsaveopt/intel_gpu_exporter/internal/discovery"
	"github.com/xsaveopt/intel_gpu_exporter/internal/sysutil"
)

type Hwmon struct {
	gpus []discovery.GPU

	powerMax    *prometheus.Desc
	powerRated  *prometheus.Desc
	powerCrit   *prometheus.Desc
	powerWindow *prometheus.Desc
	energy      *prometheus.Desc
	temperature *prometheus.Desc
	fan         *prometheus.Desc
	curr        *prometheus.Desc
	voltage     *prometheus.Desc
}

func NewHwmon(gpus []discovery.GPU) *Hwmon {
	lbls := append(CommonLabels(), "hwmon", "channel")
	d := func(name, help string) *prometheus.Desc {
		return prometheus.NewDesc(prometheus.BuildFQName(Namespace, "hwmon", name), help, lbls, nil)
	}
	return &Hwmon{
		gpus:        gpus,
		powerMax:    d("power_max_watts", "Sustained power limit (PL1) reported by hwmon."),
		powerRated:  d("power_rated_max_watts", "Default/rated TDP power limit."),
		powerWindow: d("power_max_interval_seconds", "Averaging window (tau) of the sustained power limit (PL1)."),
		powerCrit:   d("power_crit_watts", "Critical power limit."),
		energy:      d("energy_joules_total", "Cumulative energy consumption."),
		temperature: d("temperature_celsius", "Device temperature."),
		fan:         d("fan_rpm", "Fan tachometer."),
		curr:        d("current_amperes", "Current sensor."),
		voltage:     d("voltage_volts", "Voltage sensor."),
	}
}

func (c *Hwmon) Name() string { return "hwmon" }

func (c *Hwmon) Available(gpus []discovery.GPU) bool {
	for _, g := range gpus {
		if len(g.HwmonPaths) > 0 {
			return true
		}
	}
	return false
}

func (c *Hwmon) Update(ctx context.Context, ch chan<- prometheus.Metric) error {
	for _, g := range c.gpus {
		for _, hwmonPath := range g.HwmonPaths {
			hwmonName, _ := sysutil.ReadString(filepath.Join(hwmonPath, "name"))
			base := append(LabelValues(g), hwmonName)

			files, _ := filepath.Glob(filepath.Join(hwmonPath, "*"))
			for _, f := range files {
				name := filepath.Base(f)
				switch {
				case strings.HasPrefix(name, "power") && strings.HasSuffix(name, "_max_interval"):
					channel := strings.TrimSuffix(strings.TrimPrefix(name, "power"), "_max_interval")
					c.emitScaled(ch, c.powerWindow, f, 1e-3, base, channel)
				case strings.HasPrefix(name, "power") && strings.HasSuffix(name, "_rated_max"):
					channel := strings.TrimSuffix(strings.TrimPrefix(name, "power"), "_rated_max")
					c.emitScaled(ch, c.powerRated, f, 1e-6, base, channel)
				case strings.HasPrefix(name, "power") && strings.HasSuffix(name, "_max"):
					channel := strings.TrimSuffix(strings.TrimPrefix(name, "power"), "_max")
					c.emitScaled(ch, c.powerMax, f, 1e-6, base, channel)
				case strings.HasPrefix(name, "power") && strings.HasSuffix(name, "_crit"):
					channel := strings.TrimSuffix(strings.TrimPrefix(name, "power"), "_crit")
					c.emitScaled(ch, c.powerCrit, f, 1e-6, base, channel)
				case strings.HasPrefix(name, "energy") && strings.HasSuffix(name, "_input"):
					channel := strings.TrimSuffix(strings.TrimPrefix(name, "energy"), "_input")
					c.emitScaled(ch, c.energy, f, 1e-6, base, channel)
				case strings.HasPrefix(name, "temp") && strings.HasSuffix(name, "_input"):
					channel := strings.TrimSuffix(strings.TrimPrefix(name, "temp"), "_input")
					c.emitScaled(ch, c.temperature, f, 1e-3, base, channel)
				case strings.HasPrefix(name, "fan") && strings.HasSuffix(name, "_input"):
					channel := strings.TrimSuffix(strings.TrimPrefix(name, "fan"), "_input")
					c.emitScaled(ch, c.fan, f, 1.0, base, channel)
				case strings.HasPrefix(name, "curr") && strings.HasSuffix(name, "_input"):
					channel := strings.TrimSuffix(strings.TrimPrefix(name, "curr"), "_input")
					c.emitScaled(ch, c.curr, f, 1e-3, base, channel)
				case strings.HasPrefix(name, "in") && strings.HasSuffix(name, "_input"):
					channel := strings.TrimSuffix(strings.TrimPrefix(name, "in"), "_input")
					c.emitScaled(ch, c.voltage, f, 1e-3, base, channel)
				}
			}
		}
	}
	return nil
}

func (c *Hwmon) emitScaled(ch chan<- prometheus.Metric, desc *prometheus.Desc, path string, scale float64, base []string, channel string) {
	v, err := sysutil.ReadFloat64(path)
	if err != nil {
		return
	}
	mtype := prometheus.GaugeValue
	if strings.Contains(desc.String(), "energy_joules_total") {
		mtype = prometheus.CounterValue
	}
	ch <- prometheus.MustNewConstMetric(desc, mtype, v*scale, append(base, channel)...)
}
