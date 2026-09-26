package collector

import (
	"path/filepath"
	"testing"

	"github.com/xsaveopt/intel_gpu_exporter/internal/discovery"
)

func hwmonGPU(t *testing.T, files map[string]string) discovery.GPU {
	t.Helper()
	g := i915GPU(t, nil, nil)
	hwmonPath := filepath.Join(g.DevicePath, "hwmon", "hwmon3")
	mkdirAll(t, hwmonPath)
	writeFiles(t, hwmonPath, files)
	g.HwmonPaths = []string{hwmonPath}
	return g
}

func TestHwmonUpdate(t *testing.T) {
	g := hwmonGPU(t, map[string]string{
		"name":          "i915\n",
		"power1_max":    "28000000\n",
		"power1_crit":   "40000000\n",
		"energy1_input": "123456789\n",
		"temp1_input":   "52000\n",
		"fan1_input":    "1800\n",
		"curr1_input":   "2500\n",
		"in0_input":     "950\n",
		"power1_label":  "card\n",
		"uevent":        "OF_NAME=hwmon\n",
	})
	assertSamples(t, NewHwmon([]discovery.GPU{g}), []string{
		`intel_gpu_hwmon_current_amperes{` + i915Labels + `,channel="1",hwmon="i915"} 2.5`,
		`intel_gpu_hwmon_energy_joules_total{` + i915Labels + `,channel="1",hwmon="i915"} 123.456789`,
		`intel_gpu_hwmon_fan_rpm{` + i915Labels + `,channel="1",hwmon="i915"} 1800`,
		`intel_gpu_hwmon_power_crit_watts{` + i915Labels + `,channel="1",hwmon="i915"} 40`,
		`intel_gpu_hwmon_power_max_watts{` + i915Labels + `,channel="1",hwmon="i915"} 28`,
		`intel_gpu_hwmon_temperature_celsius{` + i915Labels + `,channel="1",hwmon="i915"} 52`,
		`intel_gpu_hwmon_voltage_volts{` + i915Labels + `,channel="0",hwmon="i915"} 0.9500000000000001`,
	})
}

func TestHwmonUpdateRatedMax(t *testing.T) {
	g := hwmonGPU(t, map[string]string{"name": "i915\n", "power1_rated_max": "35000000\n"})
	assertSamples(t, NewHwmon([]discovery.GPU{g}), []string{
		`intel_gpu_hwmon_power_rated_max_watts{` + i915Labels + `,channel="1",hwmon="i915"} 35`,
	})
}

func TestHwmonUpdateMaxAndRatedMaxTogether(t *testing.T) {
	g := hwmonGPU(t, map[string]string{
		"name":             "i915\n",
		"power1_max":       "28000000\n",
		"power1_rated_max": "35000000\n",
	})
	assertSamples(t, NewHwmon([]discovery.GPU{g}), []string{
		`intel_gpu_hwmon_power_max_watts{` + i915Labels + `,channel="1",hwmon="i915"} 28`,
		`intel_gpu_hwmon_power_rated_max_watts{` + i915Labels + `,channel="1",hwmon="i915"} 35`,
	})
}

func TestHwmonUpdateMultipleChannels(t *testing.T) {
	g := hwmonGPU(t, map[string]string{
		"name":        "xe\n",
		"power1_max":  "150000000\n",
		"power2_max":  "175000000\n",
		"temp1_input": "48000\n",
		"temp2_input": "55000\n",
	})
	assertSamples(t, NewHwmon([]discovery.GPU{g}), []string{
		`intel_gpu_hwmon_power_max_watts{` + i915Labels + `,channel="1",hwmon="xe"} 150`,
		`intel_gpu_hwmon_power_max_watts{` + i915Labels + `,channel="2",hwmon="xe"} 175`,
		`intel_gpu_hwmon_temperature_celsius{` + i915Labels + `,channel="1",hwmon="xe"} 48`,
		`intel_gpu_hwmon_temperature_celsius{` + i915Labels + `,channel="2",hwmon="xe"} 55`,
	})
}

func TestHwmonUpdateEnergyIsACounter(t *testing.T) {
	g := hwmonGPU(t, map[string]string{"name": "i915\n", "energy1_input": "1000000\n", "temp1_input": "40000\n"})
	types := metricTypes(t, NewHwmon([]discovery.GPU{g}))
	if got := types["intel_gpu_hwmon_energy_joules_total"]; got != "counter" {
		t.Errorf("energy type = %q, want counter", got)
	}
	if got := types["intel_gpu_hwmon_temperature_celsius"]; got != "gauge" {
		t.Errorf("temperature type = %q, want gauge", got)
	}
}

func TestHwmonUpdateUnreadableValues(t *testing.T) {
	g := hwmonGPU(t, map[string]string{"name": "i915\n", "temp1_input": "n/a\n", "fan1_input": "\n"})
	if got := samples(t, NewHwmon([]discovery.GPU{g})); len(got) != 0 {
		t.Errorf("got %v, want no samples for unparsable values", got)
	}
}

func TestHwmonUpdateMissingName(t *testing.T) {
	g := hwmonGPU(t, map[string]string{"power1_max": "15000000\n"})
	assertSamples(t, NewHwmon([]discovery.GPU{g}), []string{
		`intel_gpu_hwmon_power_max_watts{` + i915Labels + `,channel="1",hwmon=""} 15`,
	})
}

func TestHwmonAvailable(t *testing.T) {
	c := NewHwmon(nil)
	if c.Name() != "hwmon" {
		t.Errorf("Name = %q", c.Name())
	}
	if c.Available([]discovery.GPU{{Card: "card0"}}) {
		t.Error("Available should be false without hwmon paths")
	}
	if !c.Available([]discovery.GPU{{Card: "card0", HwmonPaths: []string{"/sys/x"}}}) {
		t.Error("Available should be true when a hwmon path was discovered")
	}
}
