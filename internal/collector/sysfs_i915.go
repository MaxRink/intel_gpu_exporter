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

type I915Sysfs struct {
	gpus []discovery.GPU

	freqCur *prometheus.Desc
	freqAct *prometheus.Desc
	freqMin *prometheus.Desc
	freqMax *prometheus.Desc
	freqRP0 *prometheus.Desc
	freqRPn *prometheus.Desc
	freqBst *prometheus.Desc
	rc6     *prometheus.Desc
	thrott  *prometheus.Desc
}

func NewI915Sysfs(gpus []discovery.GPU) *I915Sysfs {
	lbls := append(CommonLabels(), "gt")
	d := func(name, help, unit string) *prometheus.Desc {
		return prometheus.NewDesc(prometheus.BuildFQName(Namespace, "i915", name+"_"+unit), help, lbls, nil)
	}
	return &I915Sysfs{
		gpus:    gpus,
		freqCur: d("frequency_requested", "GuC requested GT frequency.", "mhz"),
		freqAct: d("frequency_actual", "Actual GT frequency reported by hardware.", "mhz"),
		freqMin: d("frequency_min", "Minimum software-allowed GT frequency.", "mhz"),
		freqMax: d("frequency_max", "Maximum software-allowed GT frequency.", "mhz"),
		freqRP0: d("frequency_rp0", "Hardware maximum (RP0) frequency.", "mhz"),
		freqRPn: d("frequency_rpn", "Hardware minimum (RPn) frequency.", "mhz"),
		freqBst: d("frequency_boost", "Boost frequency hint (per-GT only).", "mhz"),
		thrott: prometheus.NewDesc(
			prometheus.BuildFQName(Namespace, "i915", "throttle_reason"),
			"1 while the GT frequency is throttled for this reason (gt/gtN/throttle_reason_*; reason status = any).",
			append(CommonLabels(), "gt", "reason"), nil,
		),
		rc6: prometheus.NewDesc(
			prometheus.BuildFQName(Namespace, "i915", "rc6_residency_ms"),
			"RC6 residency counter.", CommonLabels(), nil,
		),
	}
}

func (c *I915Sysfs) Name() string { return "i915_sysfs" }

func (c *I915Sysfs) Available(gpus []discovery.GPU) bool {
	for _, g := range gpus {
		if g.Driver == discovery.DriverI915 {
			return true
		}
	}
	return false
}

func (c *I915Sysfs) Update(ctx context.Context, ch chan<- prometheus.Metric) error {
	for _, g := range c.gpus {
		if g.Driver != discovery.DriverI915 {
			continue
		}
		base := LabelValues(g)

		legacyMap := map[*prometheus.Desc]string{
			c.freqCur: "gt_cur_freq_mhz",
			c.freqAct: "gt_act_freq_mhz",
			c.freqMin: "gt_min_freq_mhz",
			c.freqMax: "gt_max_freq_mhz",
			c.freqRP0: "gt_RP0_freq_mhz",
			c.freqRPn: "gt_RPn_freq_mhz",
		}
		gts := discovery.I915GTs(g)

		if len(gts) == 0 {
			for desc, file := range legacyMap {
				if v, err := sysutil.ReadFloat64(filepath.Join(g.DRMPath, file)); err == nil {
					ch <- prometheus.MustNewConstMetric(desc, prometheus.GaugeValue, v,
						append(base, "0")...)
				}
			}
		}

		for _, gt := range gts {
			lv := append(append([]string{}, base...), strconv.Itoa(gt.Index))
			emit := func(desc *prometheus.Desc, file string) {
				if v, err := sysutil.ReadFloat64(filepath.Join(gt.Path, file)); err == nil {
					ch <- prometheus.MustNewConstMetric(desc, prometheus.GaugeValue, v, lv...)
				}
			}
			emit(c.freqCur, "rps_cur_freq_mhz")
			emit(c.freqAct, "rps_act_freq_mhz")
			emit(c.freqMin, "rps_min_freq_mhz")
			emit(c.freqMax, "rps_max_freq_mhz")
			emit(c.freqRP0, "rps_RP0_freq_mhz")
			emit(c.freqRPn, "rps_RPn_freq_mhz")
			emit(c.freqBst, "rps_boost_freq_mhz")
			files, _ := filepath.Glob(filepath.Join(gt.Path, "throttle_reason_*"))
			for _, f := range files {
				if v, err := sysutil.ReadFloat64(f); err == nil {
					reason := strings.TrimPrefix(filepath.Base(f), "throttle_reason_")
					ch <- prometheus.MustNewConstMetric(c.thrott, prometheus.GaugeValue, v, append(lv, reason)...)
				}
			}
		}

		if v, err := sysutil.ReadFloat64(filepath.Join(g.DRMPath, "power", "rc6_residency_ms")); err == nil {
			ch <- prometheus.MustNewConstMetric(c.rc6, prometheus.CounterValue, v, base...)
		}
	}
	return nil
}
