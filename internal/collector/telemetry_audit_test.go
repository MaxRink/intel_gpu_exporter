package collector

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/xsaveopt/intel_gpu_exporter/internal/discovery"
)

func TestI915SysfsExtendedGTFiles(t *testing.T) {
	g := i915GPU(t, map[string]string{
		"gt/gt0/rps_RP1_freq_mhz":        "600\n",
		"gt/gt0/punit_req_freq_mhz":      "2067\n",
		"gt/gt0/media_RP0_freq_mhz":      "1400\n",
		"gt/gt0/media_RPn_freq_mhz":      "1400\n",
		"gt/gt0/media_freq_factor":       "128\n",
		"gt/gt0/media_freq_factor.scale": "0.00390625\n",
		"gt/gt0/rc6_enable":              "1\n",
		"gt/gt0/slpc_ignore_eff_freq":    "0\n",
		"error":                          "No error state collected\n",
	}, nil)
	assertSamples(t, NewI915Sysfs([]discovery.GPU{g}), []string{
		`intel_gpu_i915_frequency_rp1_mhz{` + i915Labels + `,gt="0"} 600`,
		`intel_gpu_i915_frequency_punit_request_mhz{` + i915Labels + `,gt="0"} 2067`,
		`intel_gpu_i915_media_frequency_rp0_mhz{` + i915Labels + `,gt="0"} 1400`,
		`intel_gpu_i915_media_frequency_rpn_mhz{` + i915Labels + `,gt="0"} 1400`,
		`intel_gpu_i915_media_frequency_factor_ratio{` + i915Labels + `,gt="0"} 0.5`,
		`intel_gpu_i915_rc6_enabled_bool{` + i915Labels + `,gt="0"} 1`,
		`intel_gpu_i915_slpc_ignore_efficient_frequency_bool{` + i915Labels + `,gt="0"} 0`,
		`intel_gpu_i915_error_state_present{` + i915Labels + `} 0`,
	})
	writeFiles(t, g.DRMPath, map[string]string{"error": "GPU HANG: ecode 12:1:85dffffb\n"})
	got := samples(t, NewI915Sysfs([]discovery.GPU{g}))
	if !containsSample(got, `intel_gpu_i915_error_state_present{`+i915Labels+`} 1`) {
		t.Errorf("captured error state must report 1, got %v", got)
	}
}

func containsSample(got []string, want string) bool {
	w := canonicalSample(want)
	for _, g := range got {
		if g == w {
			return true
		}
	}
	return false
}

func TestHwmonPowerMaxInterval(t *testing.T) {
	g := hwmonGPU(t, map[string]string{"name": "i915\n", "power1_max_interval": "28000\n"})
	assertSamples(t, NewHwmon([]discovery.GPU{g}), []string{
		`intel_gpu_hwmon_power_max_interval_seconds{` + i915Labels + `,channel="1",hwmon="i915"} 28`,
	})
}

func TestPCIeUpstreamChain(t *testing.T) {
	root := t.TempDir()
	rootPort := filepath.Join(root, "pci0000:80", "0000:80:03.1")
	sw := filepath.Join(rootPort, "0000:82:00.0")
	down := filepath.Join(sw, "0000:83:01.0")
	dev := filepath.Join(down, "0000:84:00.0")
	link := func(dir, cur, w, max, mw string) {
		writeFiles(t, dir, map[string]string{"current_link_speed": cur, "current_link_width": w, "max_link_speed": max, "max_link_width": mw})
	}
	link(dev, "2.5 GT/s PCIe\n", "1\n", "2.5 GT/s PCIe\n", "1\n")
	link(down, "2.5 GT/s PCIe\n", "1\n", "2.5 GT/s PCIe\n", "1\n")
	link(sw, "16.0 GT/s PCIe\n", "8\n", "16.0 GT/s PCIe\n", "8\n")
	link(rootPort, "16.0 GT/s PCIe\n", "8\n", "16.0 GT/s PCIe\n", "16\n")
	g := discovery.GPU{Card: "card0", DevicePath: dev, PCIAddr: "0000:00:02.0", DeviceID: "0x9a49", Driver: discovery.DriverI915}
	got := samples(t, NewPCIe([]discovery.GPU{g}))
	for _, want := range []string{
		`intel_gpu_pcie_upstream_current_link_speed_gtps{` + i915Labels + `,hop="1",port="0000:83:01.0"} 2.5`,
		`intel_gpu_pcie_upstream_current_link_width{` + i915Labels + `,hop="2",port="0000:82:00.0"} 8`,
		`intel_gpu_pcie_upstream_max_link_width{` + i915Labels + `,hop="3",port="0000:80:03.1"} 16`,
	} {
		if !containsSample(got, want) {
			t.Errorf("missing %s in %v", want, got)
		}
	}
	if containsSample(got, `intel_gpu_pcie_upstream_current_link_width{`+i915Labels+`,hop="4",port="pci0000:80"} 0`) {
		t.Error("walk must stop at the host bridge")
	}
}

func TestDebugfsUCAndRPe(t *testing.T) {
	g := i915GPU(t, nil, nil)
	dir := t.TempDir()
	base := filepath.Join(dir, g.PCIAddr)
	writeFiles(t, filepath.Join(base, "gt0", "uc"), map[string]string{
		"guc_info": "GuC firmware: i915/dg2_guc_70.bin\n\tstatus: RUNNING\n\tversion: found 70.36.0\n\tuCode: 377088 bytes\nGuC status 0x800301ec:\n",
		"huc_info": "HuC firmware: i915/dg2_huc_gsc.bin\n\tstatus: LOAD FAILED\n\tversion: found 7.10.16\n",
	})
	writeFiles(t, base, map[string]string{"i915_frequency_info": "Max freq: 2450 MHz\nefficient (RPe) frequency: 600 MHz\n"})
	c := NewDebugfs([]discovery.GPU{g}, dir)
	if !c.Available([]discovery.GPU{g}) {
		t.Fatal("debugfs dir present: want available")
	}
	assertSamples(t, c, []string{
		`intel_gpu_uc_firmware_info{` + i915Labels + `,file="i915/dg2_guc_70.bin",fw="guc",gt="0",status="RUNNING",version="70.36.0"} 1`,
		`intel_gpu_uc_firmware_info{` + i915Labels + `,file="i915/dg2_huc_gsc.bin",fw="huc",gt="0",status="LOAD FAILED",version="7.10.16"} 1`,
		`intel_gpu_uc_running{` + i915Labels + `,fw="guc",gt="0"} 1`,
		`intel_gpu_uc_running{` + i915Labels + `,fw="huc",gt="0"} 0`,
		`intel_gpu_i915_frequency_rpe_mhz{` + i915Labels + `,gt="0"} 600`,
	})
	if NewDebugfs(nil, "").Available([]discovery.GPU{g}) {
		t.Error("no dir: want unavailable")
	}
	_ = os.Remove(dir)
}
