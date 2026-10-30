package collector

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/xsaveopt/intel_gpu_exporter/internal/discovery"
)

func TestInfoUpdate(t *testing.T) {
	g := i915GPU(t, nil, map[string]string{
		"subsystem_vendor": "0x8086\n",
		"subsystem_device": "0x2212\n",
		"revision":         "0x0c\n",
		"numa_node":        "-1\n",
		"modalias":         "pci:v00008086d00009A49sv00008086sd00002212bc03sc00i00\n",
	})
	assertSamples(t, NewInfo([]discovery.GPU{g}), []string{
		`intel_gpu_info{card="card0",device="0x9a49",driver="i915",modalias="pci:v00008086d00009A49sv00008086sd00002212bc03sc00i00",numa_node="-1",pci="0000:00:02.0",revision="0x0c",subsystem_device="0x2212",subsystem_vendor="0x8086",tiles="1"} 1`,
	})
}

func TestInfoUpdateFallsBackToUevent(t *testing.T) {
	g := i915GPU(t, nil, map[string]string{
		"revision": "0x04\n",
		"uevent": "DRIVER=i915\n" +
			"PCI_CLASS=30000\n" +
			"PCI_ID=8086:56A0\n" +
			"PCI_SUBSYS_ID=1849:6005\n" +
			"PCI_SLOT_NAME=0000:00:02.0\n" +
			"MODALIAS=pci:v00008086d000056A0sv00001849sd00006005bc03sc00i00\n",
	})
	assertSamples(t, NewInfo([]discovery.GPU{g}), []string{
		`intel_gpu_info{card="card0",device="0x9a49",driver="i915",modalias="",numa_node="",pci="0000:00:02.0",revision="0x04",subsystem_device="0x6005",subsystem_vendor="0x1849",tiles="1"} 1`,
	})
}

func TestInfoUpdateKeepsSysfsOverUevent(t *testing.T) {
	g := i915GPU(t, nil, map[string]string{
		"subsystem_vendor": "0x8086\n",
		"subsystem_device": "0x2212\n",
		"uevent":           "PCI_SUBSYS_ID=1849:6005\n",
	})
	for _, s := range samples(t, NewInfo([]discovery.GPU{g})) {
		if want := `subsystem_device="0x2212"`; !strings.Contains(s, want) {
			t.Errorf("got %s, want it to keep %s", s, want)
		}
	}
}

func TestInfoUpdateEmptyDevice(t *testing.T) {
	g := i915GPU(t, nil, nil)
	assertSamples(t, NewInfo([]discovery.GPU{g}), []string{
		`intel_gpu_info{card="card0",device="0x9a49",driver="i915",modalias="",numa_node="",pci="0000:00:02.0",revision="",subsystem_device="",subsystem_vendor="",tiles="1"} 1`,
	})
}

func TestInfoUpdateMultipleGPUs(t *testing.T) {
	a := i915GPU(t, nil, map[string]string{"revision": "0x0c\n"})
	b := xeGPU(t, 2, 1, nil, map[string]string{"revision": "0x08\n"})
	names := samples(t, NewInfo([]discovery.GPU{a, b}))
	if len(names) != 2 {
		t.Fatalf("got %d samples, want 2: %v", len(names), names)
	}
	if !strings.Contains(names[1], `tiles="2"`) {
		t.Errorf("xe sample should report two tiles: %s", names[1])
	}
}

func TestInfoAvailable(t *testing.T) {
	c := NewInfo(nil)
	if c.Name() != "info" {
		t.Errorf("Name = %q", c.Name())
	}
	if c.Available(nil) {
		t.Error("Available should be false without GPUs")
	}
	if !c.Available([]discovery.GPU{{Card: "card0"}}) {
		t.Error("Available should be true with a GPU present")
	}
}

func TestParseUevent(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{
		"uevent": "DRIVER=xe\nPCI_ID=8086:56A0\nno-equals-sign\n=leading\nEMPTY=\n",
	})
	got := parseUevent(filepath.Join(dir, "uevent"))
	want := map[string]string{"DRIVER": "xe", "PCI_ID": "8086:56A0", "EMPTY": ""}
	if len(got) != len(want) {
		t.Errorf("got %v, want %v", got, want)
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %q, want %q", k, got[k], v)
		}
	}
}

func TestParseUeventMissingFile(t *testing.T) {
	got := parseUevent(filepath.Join(t.TempDir(), "absent"))
	if got == nil || len(got) != 0 {
		t.Errorf("got %v, want an empty map", got)
	}
}

func TestItoa(t *testing.T) {
	cases := map[int]string{0: "0", 1: "1", 9: "9", 10: "10", 42: "42", 1024: "1024"}
	for in, want := range cases {
		if got := itoa(in); got != want {
			t.Errorf("itoa(%d) = %q, want %q", in, got, want)
		}
	}
}

func TestInfoRuntimeSuspended(t *testing.T) {
	g := i915GPU(t, nil, map[string]string{"power/runtime_status": "suspended\n"})
	for _, s := range samples(t, NewInfo([]discovery.GPU{g})) {
		if s == `intel_gpu_runtime_suspended{`+i915Labels+`} 1` {
			return
		}
	}
	t.Errorf("want intel_gpu_runtime_suspended 1, got %v", samples(t, NewInfo([]discovery.GPU{g})))
}
