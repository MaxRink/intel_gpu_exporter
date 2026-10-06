package collector

import (
	"bufio"
	"context"
	"os"
	"path/filepath"
	"strings"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/xsaveopt/intel_gpu_exporter/internal/discovery"
	"github.com/xsaveopt/intel_gpu_exporter/internal/sysutil"
)

type Info struct {
	gpus      []discovery.GPU
	desc      *prometheus.Desc
	suspended *prometheus.Desc
}

func NewInfo(gpus []discovery.GPU) *Info {
	return &Info{
		gpus: gpus,
		suspended: prometheus.NewDesc(
			prometheus.BuildFQName(Namespace, "", "runtime_suspended"),
			"1 while the GPU is runtime-suspended (power/runtime_status); wake-inducing sources replay their last samples meanwhile.",
			CommonLabels(), nil,
		),
		desc: prometheus.NewDesc(
			prometheus.BuildFQName(Namespace, "", "info"),
			"Constant 1 gauge labelled with static device metadata.",
			[]string{
				"card", "pci", "device", "driver",
				"subsystem_vendor", "subsystem_device", "revision",
				"numa_node", "tiles", "modalias",
			},
			nil,
		),
	}
}

func (c *Info) Name() string { return "info" }

func (c *Info) Available(gpus []discovery.GPU) bool { return len(gpus) > 0 }

func (c *Info) Update(ctx context.Context, ch chan<- prometheus.Metric) error {
	for _, g := range c.gpus {
		subVendor, _ := sysutil.ReadString(filepath.Join(g.DevicePath, "subsystem_vendor"))
		subDevice, _ := sysutil.ReadString(filepath.Join(g.DevicePath, "subsystem_device"))
		revision, _ := sysutil.ReadString(filepath.Join(g.DevicePath, "revision"))
		numa, _ := sysutil.ReadString(filepath.Join(g.DevicePath, "numa_node"))
		modalias, _ := sysutil.ReadString(filepath.Join(g.DevicePath, "modalias"))

		uevent := parseUevent(filepath.Join(g.DevicePath, "uevent"))
		if v, ok := uevent["PCI_SUBSYS_ID"]; ok && (subVendor == "" || subDevice == "") {
			parts := strings.Split(v, ":")
			if len(parts) == 2 {
				if subVendor == "" {
					subVendor = "0x" + strings.ToLower(parts[0])
				}
				if subDevice == "" {
					subDevice = "0x" + strings.ToLower(parts[1])
				}
			}
		}

		ch <- prometheus.MustNewConstMetric(c.desc, prometheus.GaugeValue, 1,
			g.Card, g.PCIAddr, g.DeviceID, string(g.Driver),
			subVendor, subDevice, revision,
			numa, itoa(len(g.Tiles)), modalias,
		)
		if st, err := sysutil.ReadString(filepath.Join(g.DevicePath, "power", "runtime_status")); err == nil {
			v := 0.0
			if st == "suspended" {
				v = 1
			}
			ch <- prometheus.MustNewConstMetric(c.suspended, prometheus.GaugeValue, v, LabelValues(g)...)
		}
	}
	return nil
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}

func parseUevent(path string) map[string]string {
	out := map[string]string{}
	f, err := os.Open(path)
	if err != nil {
		return out
	}
	defer func() { _ = f.Close() }()
	s := bufio.NewScanner(f)
	for s.Scan() {
		line := s.Text()
		eq := strings.IndexByte(line, '=')
		if eq <= 0 {
			continue
		}
		out[line[:eq]] = line[eq+1:]
	}
	return out
}
