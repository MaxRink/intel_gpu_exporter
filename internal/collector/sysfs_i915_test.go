package collector

import (
	"testing"

	"github.com/xsaveopt/intel_gpu_exporter/internal/discovery"
)

func TestI915SysfsUpdatePerGT(t *testing.T) {
	g := i915GPU(t, map[string]string{
		"gt/gt0/rps_cur_freq_mhz":   "1300\n",
		"gt/gt0/rps_act_freq_mhz":   "1250\n",
		"gt/gt0/rps_min_freq_mhz":   "350\n",
		"gt/gt0/rps_max_freq_mhz":   "1450\n",
		"gt/gt0/rps_RP0_freq_mhz":   "1450\n",
		"gt/gt0/rps_RPn_freq_mhz":   "350\n",
		"gt/gt0/rps_boost_freq_mhz": "1450\n",
		"power/rc6_residency_ms":    "987654\n",
	}, nil)

	assertSamples(t, NewI915Sysfs([]discovery.GPU{g}), []string{
		`intel_gpu_i915_frequency_actual_mhz{` + i915Labels + `,gt="0"} 1250`,
		`intel_gpu_i915_frequency_boost_mhz{` + i915Labels + `,gt="0"} 1450`,
		`intel_gpu_i915_frequency_max_mhz{` + i915Labels + `,gt="0"} 1450`,
		`intel_gpu_i915_frequency_min_mhz{` + i915Labels + `,gt="0"} 350`,
		`intel_gpu_i915_frequency_requested_mhz{` + i915Labels + `,gt="0"} 1300`,
		`intel_gpu_i915_frequency_rp0_mhz{` + i915Labels + `,gt="0"} 1450`,
		`intel_gpu_i915_frequency_rpn_mhz{` + i915Labels + `,gt="0"} 350`,
		`intel_gpu_i915_rc6_residency_ms{` + i915Labels + `} 987654`,
	})
}

func TestI915SysfsUpdateMultipleGTs(t *testing.T) {
	g := i915GPU(t, map[string]string{
		"gt/gt0/rps_act_freq_mhz": "1250\n",
		"gt/gt1/rps_act_freq_mhz": "900\n",
	}, nil)
	assertSamples(t, NewI915Sysfs([]discovery.GPU{g}), []string{
		`intel_gpu_i915_frequency_actual_mhz{` + i915Labels + `,gt="0"} 1250`,
		`intel_gpu_i915_frequency_actual_mhz{` + i915Labels + `,gt="1"} 900`,
	})
}

func TestI915SysfsUpdateLegacyLayout(t *testing.T) {
	g := i915GPU(t, map[string]string{
		"gt_cur_freq_mhz":        "900\n",
		"gt_act_freq_mhz":        "850\n",
		"gt_min_freq_mhz":        "300\n",
		"gt_max_freq_mhz":        "1150\n",
		"gt_RP0_freq_mhz":        "1150\n",
		"gt_RPn_freq_mhz":        "300\n",
		"gt_boost_freq_mhz":      "1150\n",
		"power/rc6_residency_ms": "42\n",
	}, nil)

	assertSamples(t, NewI915Sysfs([]discovery.GPU{g}), []string{
		`intel_gpu_i915_frequency_actual_mhz{` + i915Labels + `,gt="0"} 850`,
		`intel_gpu_i915_frequency_max_mhz{` + i915Labels + `,gt="0"} 1150`,
		`intel_gpu_i915_frequency_min_mhz{` + i915Labels + `,gt="0"} 300`,
		`intel_gpu_i915_frequency_requested_mhz{` + i915Labels + `,gt="0"} 900`,
		`intel_gpu_i915_frequency_rp0_mhz{` + i915Labels + `,gt="0"} 1150`,
		`intel_gpu_i915_frequency_rpn_mhz{` + i915Labels + `,gt="0"} 300`,
		`intel_gpu_i915_rc6_residency_ms{` + i915Labels + `} 42`,
	})
}

func TestI915SysfsUpdatePrefersPerGTOverLegacy(t *testing.T) {
	g := i915GPU(t, map[string]string{
		"gt_act_freq_mhz":         "850\n",
		"gt/gt0/rps_act_freq_mhz": "1250\n",
	}, nil)
	assertSamples(t, NewI915Sysfs([]discovery.GPU{g}), []string{
		`intel_gpu_i915_frequency_actual_mhz{` + i915Labels + `,gt="0"} 1250`,
	})
}

func TestI915SysfsRC6IsACounter(t *testing.T) {
	g := i915GPU(t, map[string]string{
		"power/rc6_residency_ms":  "42\n",
		"gt/gt0/rps_act_freq_mhz": "1250\n",
	}, nil)
	types := metricTypes(t, NewI915Sysfs([]discovery.GPU{g}))
	if got := types["intel_gpu_i915_rc6_residency_ms"]; got != "counter" {
		t.Errorf("rc6 type = %q, want counter", got)
	}
	if got := types["intel_gpu_i915_frequency_actual_mhz"]; got != "gauge" {
		t.Errorf("frequency type = %q, want gauge", got)
	}
}

func TestI915SysfsUpdateSkipsOtherDrivers(t *testing.T) {
	g := xeGPU(t, 1, 1, map[string]string{"gt_act_freq_mhz": "850\n"}, nil)
	if got := samples(t, NewI915Sysfs([]discovery.GPU{g})); len(got) != 0 {
		t.Errorf("got %v, want no samples for an xe device", got)
	}
}

func TestI915SysfsUpdateEmptyTree(t *testing.T) {
	g := i915GPU(t, nil, nil)
	if got := samples(t, NewI915Sysfs([]discovery.GPU{g})); len(got) != 0 {
		t.Errorf("got %v, want no samples", got)
	}
}

func TestI915SysfsAvailable(t *testing.T) {
	c := NewI915Sysfs(nil)
	if c.Name() != "i915_sysfs" {
		t.Errorf("Name = %q", c.Name())
	}
	if c.Available([]discovery.GPU{{Driver: discovery.DriverXe}}) {
		t.Error("Available should be false without an i915 device")
	}
	if !c.Available([]discovery.GPU{{Driver: discovery.DriverI915}}) {
		t.Error("Available should be true with an i915 device")
	}
}

func TestI915SysfsThrottleReasons(t *testing.T) {
	g := i915GPU(t, map[string]string{
		"gt/gt0/throttle_reason_status":  "1\n",
		"gt/gt0/throttle_reason_thermal": "1\n",
		"gt/gt0/throttle_reason_pl1":     "0\n",
	}, nil)
	assertSamples(t, NewI915Sysfs([]discovery.GPU{g}), []string{
		`intel_gpu_i915_throttle_reason{` + i915Labels + `,gt="0",reason="pl1"} 0`,
		`intel_gpu_i915_throttle_reason{` + i915Labels + `,gt="0",reason="status"} 1`,
		`intel_gpu_i915_throttle_reason{` + i915Labels + `,gt="0",reason="thermal"} 1`,
	})
}
