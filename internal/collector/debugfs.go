package collector

import (
	"bufio"
	"context"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/xsaveopt/intel_gpu_exporter/internal/discovery"
)

// Debugfs reads i915 state that only DRM debugfs exposes: GuC/HuC firmware
// status and version (gt*/uc/{guc,huc,gsc}_info) and the efficient (RPe)
// frequency (i915_frequency_info). Dir is laid out like /sys/kernel/debug/dri
// (bind-mount it read-only); entries are found by PCI address. These reads
// take a runtime-PM wakeref, so wrap the source in an IdleGate.
type Debugfs struct {
	gpus []discovery.GPU
	Dir  string

	ucInfo    *prometheus.Desc
	ucRunning *prometheus.Desc
	rpe       *prometheus.Desc
}

func NewDebugfs(gpus []discovery.GPU, dir string) *Debugfs {
	return &Debugfs{
		gpus: gpus,
		Dir:  dir,
		ucInfo: prometheus.NewDesc(prometheus.BuildFQName(Namespace, "uc", "firmware_info"),
			"GuC/HuC/GSC firmware loaded by i915 (debugfs gt*/uc/*_info): status, version, file.",
			append(CommonLabels(), "gt", "fw", "status", "version", "file"), nil),
		ucRunning: prometheus.NewDesc(prometheus.BuildFQName(Namespace, "uc", "running"),
			"1 if the GuC/HuC/GSC firmware status is RUNNING.",
			append(CommonLabels(), "gt", "fw"), nil),
		rpe: prometheus.NewDesc(prometheus.BuildFQName(Namespace, "i915", "frequency_rpe_mhz"),
			"Efficient (RPe) GT frequency (debugfs i915_frequency_info).",
			append(CommonLabels(), "gt"), nil),
	}
}

func (c *Debugfs) Name() string { return "debugfs" }

func (c *Debugfs) Available(gpus []discovery.GPU) bool {
	_, err := os.Stat(c.Dir)
	return c.Dir != "" && err == nil && len(gpus) > 0
}

var (
	ucHeader  = regexp.MustCompile(`^(GuC|HuC|GSC) firmware:\s*(\S*)`)
	ucStatus  = regexp.MustCompile(`^\s*status:\s*(.*?)\s*$`)
	ucVersion = regexp.MustCompile(`^\s*version:\s*found\s+(\S+)`)
	rpeLine   = regexp.MustCompile(`efficient \(RPe\) frequency:\s*(\d+)\s*MHz`)
)

// parseUCInfo returns the firmware kind, file, status and version from the
// head of a guc_info/huc_info/gsc_info file.
func parseUCInfo(path string) (fw, file, status, version string, ok bool) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer func() { _ = f.Close() }()
	s := bufio.NewScanner(f)
	for i := 0; i < 8 && s.Scan(); i++ {
		line := s.Text()
		if m := ucHeader.FindStringSubmatch(line); m != nil {
			fw, file = strings.ToLower(m[1]), m[2]
		} else if m := ucStatus.FindStringSubmatch(line); m != nil && status == "" {
			status = m[1]
		} else if m := ucVersion.FindStringSubmatch(line); m != nil && version == "" {
			version = m[1]
		}
	}
	return fw, file, status, version, fw != "" && status != ""
}

func (c *Debugfs) Update(ctx context.Context, ch chan<- prometheus.Metric) error {
	for _, g := range c.gpus {
		if g.Driver != discovery.DriverI915 {
			continue
		}
		base := filepath.Join(c.Dir, g.PCIAddr)
		lv := LabelValues(g)
		gts, _ := filepath.Glob(filepath.Join(base, "gt[0-9]*"))
		for _, gtDir := range gts {
			gt := strings.TrimPrefix(filepath.Base(gtDir), "gt")
			for _, name := range []string{"guc_info", "huc_info", "gsc_info"} {
				fw, file, status, version, ok := parseUCInfo(filepath.Join(gtDir, "uc", name))
				if !ok {
					continue
				}
				ch <- prometheus.MustNewConstMetric(c.ucInfo, prometheus.GaugeValue, 1,
					append(append([]string{}, lv...), gt, fw, status, version, file)...)
				running := 0.0
				if status == "RUNNING" {
					running = 1
				}
				ch <- prometheus.MustNewConstMetric(c.ucRunning, prometheus.GaugeValue, running,
					append(append([]string{}, lv...), gt, fw)...)
			}
		}
		if b, err := os.ReadFile(filepath.Join(base, "i915_frequency_info")); err == nil {
			if m := rpeLine.FindSubmatch(b); m != nil {
				var v float64
				for _, d := range m[1] {
					v = v*10 + float64(d-'0')
				}
				ch <- prometheus.MustNewConstMetric(c.rpe, prometheus.GaugeValue, v, append(append([]string{}, lv...), "0")...)
			}
		}
	}
	return nil
}
