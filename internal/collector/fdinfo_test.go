package collector

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/xsaveopt/intel_gpu_exporter/internal/discovery"
)

func TestParseFdinfoI915Fixture(t *testing.T) {
	path := filepath.Join("testdata", "fdinfo_i915.txt")
	got := parseFdinfo(path)

	want := map[string]string{
		"drm-driver":                 "i915",
		"drm-pdev":                   "0000:00:02.0",
		"drm-client-id":              "17",
		"drm-total-system":           "16384 KiB",
		"drm-shared-system":          "4096 KiB",
		"drm-resident-system":        "12288 KiB",
		"drm-purgeable-system":       "0",
		"drm-active-system":          "0",
		"drm-total-stolen-system":    "0",
		"drm-shared-stolen-system":   "0",
		"drm-resident-stolen-system": "0",
		"drm-engine-render":          "9204536832 ns",
		"drm-engine-copy":            "0 ns",
		"drm-engine-video":           "1024000000 ns",
		"drm-engine-video-enhance":   "0 ns",
		"drm-engine-capacity-video":  "2",
	}
	if len(got) != len(want) {
		t.Errorf("got %d keys, want %d: %v", len(got), len(want), got)
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %q, want %q", k, got[k], v)
		}
	}
	for _, k := range []string{"pos", "flags", "mnt_id", "ino"} {
		if _, ok := got[k]; ok {
			t.Errorf("non-drm key %q should have been skipped", k)
		}
	}
}

func TestParseFdinfoXeFixture(t *testing.T) {
	got := parseFdinfo(filepath.Join("testdata", "fdinfo_xe.txt"))
	if got["drm-driver"] != "xe" {
		t.Errorf("drm-driver = %q, want %q", got["drm-driver"], "xe")
	}
	if got["drm-pdev"] != "0000:03:00.0" {
		t.Errorf("drm-pdev = %q", got["drm-pdev"])
	}
	if got["drm-total-vram0"] != "2 GiB" {
		t.Errorf("drm-total-vram0 = %q", got["drm-total-vram0"])
	}
	if got["drm-engine-vcs"] != "987654321 ns" {
		t.Errorf("drm-engine-vcs = %q", got["drm-engine-vcs"])
	}
}

func TestParseFdinfoNonGPU(t *testing.T) {
	got := parseFdinfo(filepath.Join("testdata", "fdinfo_nongpu.txt"))
	if len(got) != 0 {
		t.Errorf("got %v, want no drm keys", got)
	}
}

func TestParseFdinfoMissingFile(t *testing.T) {
	got := parseFdinfo(filepath.Join(t.TempDir(), "absent"))
	if got == nil || len(got) != 0 {
		t.Errorf("got %v, want an empty map", got)
	}
}

func TestParseNs(t *testing.T) {
	cases := []struct {
		in   string
		want uint64
	}{
		{"9204536832 ns", 9204536832},
		{"0 ns", 0},
		{"  1024000000 ns  ", 1024000000},
		{"42", 42},
		{"", 0},
		{"nonsense", 0},
	}
	for _, tc := range cases {
		if got := parseNs(tc.in); got != tc.want {
			t.Errorf("parseNs(%q) = %d, want %d", tc.in, got, tc.want)
		}
	}
}

func TestParseBytes(t *testing.T) {
	cases := []struct {
		in   string
		want uint64
	}{
		{"16384 KiB", 16 * 1024 * 1024},
		{"1024 MiB", 1024 * 1024 * 1024},
		{"2 GiB", 2 * 1024 * 1024 * 1024},
		{"4096", 4096},
		{"0", 0},
		{" 512 kib ", 512 * 1024},
		{"7 TiB", 7},
		{"", 0},
		{"garbage KiB", 0},
	}
	for _, tc := range cases {
		if got := parseBytes(tc.in); got != tc.want {
			t.Errorf("parseBytes(%q) = %d, want %d", tc.in, got, tc.want)
		}
	}
}

func TestReadComm(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "comm")
	if err := os.WriteFile(path, []byte("ffmpeg\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if got := readComm(path); got != "ffmpeg" {
		t.Errorf("got %q, want %q", got, "ffmpeg")
	}
	if got := readComm(filepath.Join(dir, "absent")); got != "" {
		t.Errorf("got %q, want an empty string", got)
	}
}

func TestTopNByActivity(t *testing.T) {
	procs := map[procKey]*procData{}
	mk := func(pid string, ns uint64) procKey {
		k := procKey{pid: pid, comm: "app", driver: "i915", pci: "0000:00:02.0"}
		pd := newProcData()
		pd.engine["render"] = ns
		procs[k] = pd
		return k
	}
	low := mk("100", 10)
	mid := mk("200", 200)
	high := mk("300", 3000)

	got := topNByActivity(procs, 2)
	if len(got) != 2 {
		t.Fatalf("got %d keys, want 2", len(got))
	}
	if got[0] != high || got[1] != mid {
		t.Errorf("got %v, want [%v %v]", got, high, mid)
	}

	if all := topNByActivity(procs, 0); len(all) != 3 {
		t.Errorf("n=0 returned %d keys, want all 3", len(all))
	}
	if all := topNByActivity(procs, -1); len(all) != 3 {
		t.Errorf("n=-1 returned %d keys, want all 3", len(all))
	}
	if all := topNByActivity(procs, 10); len(all) != 3 {
		t.Errorf("n greater than the population returned %d keys, want 3", len(all))
	}
	if only := topNByActivity(procs, 1); len(only) != 1 || only[0] == low {
		t.Errorf("n=1 returned %v, want only the busiest process", only)
	}
}

type procSpec struct {
	pid     string
	comm    string
	fdinfos []string
}

func fakeProcRoot(t *testing.T, procs ...procSpec) string {
	t.Helper()
	root := t.TempDir()
	for _, p := range procs {
		dir := filepath.Join(root, p.pid)
		mkdirAll(t, filepath.Join(dir, "fdinfo"))
		writeFiles(t, dir, map[string]string{"comm": p.comm + "\n"})
		for i, fixture := range p.fdinfos {
			writeFiles(t, filepath.Join(dir, "fdinfo"), map[string]string{
				string(rune('3' + i)): testdataFile(t, fixture),
			})
		}
	}
	mkdirAll(t, filepath.Join(root, "self"))
	writeFiles(t, root, map[string]string{"uptime": "1234.56 9876.54\n"})
	return root
}

func TestFdinfoUpdate(t *testing.T) {
	root := fakeProcRoot(t,
		procSpec{pid: "1042", comm: "ffmpeg", fdinfos: []string{"fdinfo_i915.txt", "fdinfo_nongpu.txt"}},
	)
	c := NewFdinfo(root, 0)
	assertSamples(t, c, []string{
		`intel_gpu_client_dropped_processes 0`,
		`intel_gpu_client_engine_time_seconds_total{comm="ffmpeg",driver="i915",engine="copy",pci="0000:00:02.0",pid="1042"} 0`,
		`intel_gpu_client_engine_time_seconds_total{comm="ffmpeg",driver="i915",engine="render",pci="0000:00:02.0",pid="1042"} 9.204536832`,
		`intel_gpu_client_engine_time_seconds_total{comm="ffmpeg",driver="i915",engine="video",pci="0000:00:02.0",pid="1042"} 1.024`,
		`intel_gpu_client_engine_time_seconds_total{comm="ffmpeg",driver="i915",engine="video-enhance",pci="0000:00:02.0",pid="1042"} 0`,
		`intel_gpu_client_memory_resident_bytes{comm="ffmpeg",driver="i915",pci="0000:00:02.0",pid="1042",region="stolen-system"} 0`,
		`intel_gpu_client_memory_resident_bytes{comm="ffmpeg",driver="i915",pci="0000:00:02.0",pid="1042",region="system"} 1.2582912e+07`,
		`intel_gpu_client_memory_shared_bytes{comm="ffmpeg",driver="i915",pci="0000:00:02.0",pid="1042",region="stolen-system"} 0`,
		`intel_gpu_client_memory_shared_bytes{comm="ffmpeg",driver="i915",pci="0000:00:02.0",pid="1042",region="system"} 4.194304e+06`,
		`intel_gpu_client_memory_total_bytes{comm="ffmpeg",driver="i915",pci="0000:00:02.0",pid="1042",region="stolen-system"} 0`,
		`intel_gpu_client_memory_total_bytes{comm="ffmpeg",driver="i915",pci="0000:00:02.0",pid="1042",region="system"} 1.6777216e+07`,
	})
}

func TestFdinfoUpdateIgnoresForeignDrivers(t *testing.T) {
	root := fakeProcRoot(t, procSpec{pid: "77", comm: "blender", fdinfos: []string{"fdinfo_amdgpu.txt"}})
	names := metricNames(t, NewFdinfo(root, 0))
	if len(names) != 1 || names[0] != "intel_gpu_client_dropped_processes" {
		t.Errorf("got %v, want only the dropped-processes gauge", names)
	}
}

func TestFdinfoUpdateAggregatesFdsPerProcess(t *testing.T) {
	root := fakeProcRoot(t, procSpec{
		pid: "2048", comm: "glxgears",
		fdinfos: []string{"fdinfo_i915.txt", "fdinfo_i915.txt"},
	})
	for _, s := range samples(t, NewFdinfo(root, 0)) {
		if s == `intel_gpu_client_engine_time_seconds_total{comm="glxgears",driver="i915",engine="render",pci="0000:00:02.0",pid="2048"} 18.409073664` {
			return
		}
	}
	t.Errorf("two identical fds should sum into one series, got %v", samples(t, NewFdinfo(root, 0)))
}

func TestFdinfoUpdateTopNCap(t *testing.T) {
	root := fakeProcRoot(t,
		procSpec{pid: "10", comm: "a", fdinfos: []string{"fdinfo_i915.txt"}},
		procSpec{pid: "20", comm: "b", fdinfos: []string{"fdinfo_xe.txt"}},
		procSpec{pid: "30", comm: "c", fdinfos: []string{"fdinfo_i915.txt"}},
	)
	c := NewFdinfo(root, 1)
	dropped := false
	engines := 0
	for _, s := range samples(t, c) {
		switch {
		case s == `intel_gpu_client_dropped_processes 2`:
			dropped = true
		case strings.HasPrefix(s, "intel_gpu_client_engine_time_seconds_total{"):
			engines++
		}
	}
	if !dropped {
		t.Error("expected intel_gpu_client_dropped_processes to report 2")
	}
	if engines == 0 {
		t.Error("expected the surviving process to still emit engine series")
	}
}

func TestFdinfoUpdateMissingProcRoot(t *testing.T) {
	c := NewFdinfo(filepath.Join(t.TempDir(), "absent"), 0)
	if err := c.Update(t.Context(), make(chan prometheus.Metric, 1)); err == nil {
		t.Fatal("expected an error for a missing procfs root")
	}
}

func TestFdinfoAvailable(t *testing.T) {
	c := NewFdinfo(t.TempDir(), 0)
	if c.Name() != "fdinfo" {
		t.Errorf("Name = %q", c.Name())
	}
	if c.Available(nil) {
		t.Error("Available should be false without GPUs")
	}
	if !c.Available([]discovery.GPU{{Card: "card0"}}) {
		t.Error("Available should be true with a GPU present")
	}
}

func TestFdinfoUpdateSkipsEngineCapacity(t *testing.T) {
	root := fakeProcRoot(t, procSpec{pid: "1042", comm: "ffmpeg", fdinfos: []string{"fdinfo_i915.txt"}})
	for _, s := range samples(t, NewFdinfo(root, 0)) {
		if strings.Contains(s, `engine="capacity-`) {
			t.Errorf("drm-engine-capacity-* is an engine count, not engine time: %s", s)
		}
	}
}

func TestFdinfoUpdateExportsXeCycles(t *testing.T) {
	root := fakeProcRoot(t, procSpec{pid: "3141", comm: "vainfo", fdinfos: []string{"fdinfo_xe_cycles.txt"}})
	got := samples(t, NewFdinfo(root, 0))
	for _, engine := range []string{"rcs", "vcs"} {
		found := slices.ContainsFunc(got, func(s string) bool {
			return strings.HasPrefix(s, "intel_gpu_client_") &&
				strings.Contains(s, `engine="`+engine+`"`) &&
				strings.Contains(s, `pid="3141"`) &&
				strings.Contains(s, `driver="xe"`)
		})
		if !found {
			t.Errorf("no per-client series for xe engine %q from drm-cycles-%s, got %v", engine, engine, got)
		}
	}
	for _, s := range got {
		if strings.Contains(s, `region="cycles-`) {
			t.Errorf("drm-total-cycles-* is a GPU cycle count, not a memory region: %s", s)
		}
	}
}

func TestTopNByActivityTiesAreDeterministic(t *testing.T) {
	procs := map[procKey]*procData{}
	for _, pid := range []string{"11", "12", "13", "14", "15", "16", "17", "18"} {
		pd := newProcData()
		pd.engine["render"] = 500
		procs[procKey{pid: pid, comm: "app", driver: "i915", pci: "0000:00:02.0"}] = pd
	}
	first := topNByActivity(procs, 3)
	for range 50 {
		got := topNByActivity(procs, 3)
		if !slices.Equal(got, first) {
			t.Fatalf("topN selection among equally busy processes changed between calls: %v then %v", first, got)
		}
	}
}

func TestFdinfoUpdateSkipsNonDRMFds(t *testing.T) {
	root := fakeProcRoot(t, procSpec{pid: "5", comm: "ffmpeg", fdinfos: []string{"fdinfo_i915.txt", "fdinfo_i915.txt"}})
	mkdirAll(t, filepath.Join(root, "5", "fd"))
	if err := os.Symlink("/dev/null", filepath.Join(root, "5", "fd", "3")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("/dev/dri/renderD128", filepath.Join(root, "5", "fd", "4")); err != nil {
		t.Fatal(err)
	}
	want := `intel_gpu_client_engine_time_seconds_total{comm="ffmpeg",driver="i915",engine="render",pci="0000:00:02.0",pid="5"} 9.204536832`
	if !slices.Contains(samples(t, NewFdinfo(root, 0)), want) {
		t.Errorf("fd 3 (/dev/null) must be skipped, fd 4 counted once; got %v", samples(t, NewFdinfo(root, 0)))
	}
}
