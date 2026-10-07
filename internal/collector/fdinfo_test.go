package collector

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

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
		`intel_gpu_client_engine_capacity{comm="ffmpeg",driver="i915",engine="video",pci="0000:00:02.0",pid="1042"} 2`,
		`intel_gpu_client_memory_active_bytes{comm="ffmpeg",driver="i915",pci="0000:00:02.0",pid="1042",region="system"} 0`,
		`intel_gpu_client_memory_purgeable_bytes{comm="ffmpeg",driver="i915",pci="0000:00:02.0",pid="1042",region="system"} 0`,
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

func TestFdinfoRescanReadsOnlyKnownClientsInBetween(t *testing.T) {
	root := fakeProcRoot(t, procSpec{pid: "7", comm: "ffmpeg", fdinfos: []string{"fdinfo_i915.txt"}})
	c := NewFdinfo(root, 0)
	c.Rescan = time.Hour
	_ = samples(t, c) // full walk: pid 7 becomes known
	writeFiles(t, filepath.Join(root, "9", "fdinfo"), map[string]string{"3": testdataFile(t, "fdinfo_i915.txt")})
	writeFiles(t, filepath.Join(root, "9"), map[string]string{"comm": "new\n"})
	got := strings.Join(samples(t, c), "\n")
	if !strings.Contains(got, `pid="7"`) || strings.Contains(got, `pid="9"`) {
		t.Fatalf("between rescans want only known pid 7, got %s", got)
	}
	c.lastScan = time.Time{}
	if got := strings.Join(samples(t, c), "\n"); !strings.Contains(got, `pid="9"`) {
		t.Fatalf("a rescan must pick up pid 9, got %s", got)
	}
}

func TestFdinfoContainersOnlySkipsHostPIDs(t *testing.T) {
	id := strings.Repeat("ab", 32)
	root := fakeProcRoot(t,
		procSpec{pid: "10", comm: "sshd", fdinfos: []string{"fdinfo_i915.txt"}},
		procSpec{pid: "11", comm: "ffmpeg", fdinfos: []string{"fdinfo_i915.txt"}},
		procSpec{pid: "12", comm: "python3", fdinfos: []string{"fdinfo_i915.txt"}},
		procSpec{pid: "13", comm: "nocgroup", fdinfos: []string{"fdinfo_i915.txt"}},
		procSpec{pid: "14", comm: "shim", fdinfos: []string{"fdinfo_i915.txt"}},
		procSpec{pid: "15", comm: "jellyfin", fdinfos: []string{"fdinfo_i915.txt"}},
		procSpec{pid: "16", comm: "almost", fdinfos: []string{"fdinfo_i915.txt"}},
	)
	// cgroupfs driver sibling seen from the exporter's own cgroup namespace.
	writeFiles(t, filepath.Join(root, "15"), map[string]string{"cgroup": "0::/../" + id + "\n"})
	writeFiles(t, filepath.Join(root, "16"), map[string]string{"cgroup": "0::/system.slice/x" + id + ".service\n"})
	writeFiles(t, filepath.Join(root, "10"), map[string]string{"cgroup": "0::/../../system.slice/ssh.service\n"})
	// Docker systemd driver seen from a private cgroup namespace, and cgroupfs driver.
	writeFiles(t, filepath.Join(root, "11"), map[string]string{"cgroup": "0::/../../system.slice/docker-" + id + ".scope\n"})
	writeFiles(t, filepath.Join(root, "12"), map[string]string{"cgroup": "0::/docker/" + id + "\n"})
	writeFiles(t, filepath.Join(root, "14"), map[string]string{"cgroup": "0::/system.slice/containerd.service\n"})

	c := NewFdinfo(root, 0)
	c.ContainersOnly = true
	got := strings.Join(samples(t, c), "\n")
	for _, pid := range []string{"11", "12", "15"} {
		if !strings.Contains(got, `pid="`+pid+`"`) {
			t.Errorf("container pid %s missing:\n%s", pid, got)
		}
	}
	for _, pid := range []string{"10", "13", "14", "16"} {
		if strings.Contains(got, `pid="`+pid+`"`) {
			t.Errorf("non-container pid %s must be skipped:\n%s", pid, got)
		}
	}

	c.ContainersOnly = false
	if got := strings.Join(samples(t, c), "\n"); !strings.Contains(got, `pid="10"`) {
		t.Errorf("without the flag every pid is read, got:\n%s", got)
	}
}

func TestFdinfoContainersOnlyRechecksKnownPIDs(t *testing.T) {
	id := strings.Repeat("cd", 32)
	root := fakeProcRoot(t, procSpec{pid: "20", comm: "ffmpeg", fdinfos: []string{"fdinfo_i915.txt"}})
	writeFiles(t, filepath.Join(root, "20"), map[string]string{"cgroup": "0::/system.slice/docker-" + id + ".scope\n"})
	c := NewFdinfo(root, 0)
	c.ContainersOnly = true
	c.Rescan = time.Hour
	if got := strings.Join(samples(t, c), "\n"); !strings.Contains(got, `pid="20"`) {
		t.Fatalf("pid 20 must be known after the full walk, got:\n%s", got)
	}
	// PID reuse by a host process between rescans: skipped without a full walk.
	writeFiles(t, filepath.Join(root, "20"), map[string]string{"cgroup": "0::/init.scope\n"})
	if got := strings.Join(samples(t, c), "\n"); strings.Contains(got, `pid="20"`) {
		t.Fatalf("a known pid that left its container must be skipped, got:\n%s", got)
	}
}

func TestFdinfoDrmClientsReadsOnlyListedPIDs(t *testing.T) {
	id := strings.Repeat("ef", 32)
	root := fakeProcRoot(t,
		procSpec{pid: "30", comm: "ffmpeg", fdinfos: []string{"fdinfo_i915.txt"}},
		procSpec{pid: "31", comm: "unlisted", fdinfos: []string{"fdinfo_i915.txt"}},
	)
	for _, pid := range []string{"30", "31"} {
		writeFiles(t, filepath.Join(root, pid), map[string]string{"cgroup": "0::/../" + id + "\n"})
	}
	dbg := t.TempDir()
	clients := "             command  tgid dev master a   uid      magic\n" +
		"              ffmpeg    30 128   n    n     0          0\n" +
		"              ffmpeg    30 128   n    n     0          0\n"
	writeFiles(t, filepath.Join(dbg, "128"), map[string]string{"clients": clients})
	writeFiles(t, filepath.Join(dbg, "0000:84:00.0"), map[string]string{"clients": clients})
	writeFiles(t, filepath.Join(dbg, "0"), map[string]string{"clients": "             command  tgid dev master a   uid      magic\n"})

	if got := drmClientPIDs(dbg); !slices.Equal(got, []string{"30"}) {
		t.Fatalf("drmClientPIDs = %v, want [30]", got)
	}
	c := NewFdinfo(root, 0)
	c.ContainersOnly = true
	c.DrmClients = dbg
	c.Rescan = time.Hour
	got := strings.Join(samples(t, c), "\n")
	if !strings.Contains(got, `pid="30"`) || strings.Contains(got, `pid="31"`) {
		t.Fatalf("want only the listed DRM client pid 30, got:\n%s", got)
	}
}
