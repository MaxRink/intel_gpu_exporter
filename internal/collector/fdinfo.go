package collector

import (
	"bufio"
	"context"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/xsaveopt/intel_gpu_exporter/internal/discovery"
)

type Fdinfo struct {
	procRoot string
	topN     int

	// Rescan bounds the full /proc walk: in between, only PIDs that held a
	// DRM fd at the last walk are re-read (0 = walk on every scrape).
	Rescan time.Duration
	// ContainersOnly skips every PID whose /proc/<pid>/cgroup (world-readable,
	// no ptrace access check) is not a container cgroup, before /proc/<pid>/fd
	// or fdinfo is touched. Host processes are never ptrace-checked, so an LSM
	// such as AppArmor docker-default logs no denials for them.
	ContainersOnly bool
	// DrmClients is a directory laid out like /sys/kernel/debug/dri: each
	// <minor>/clients file lists the tgid of every open DRM file. When set,
	// only those PIDs are read (no /proc walk, no Rescan); debugfs needs no
	// ptrace access check, so non-GPU processes are never touched.
	DrmClients string
	mu         sync.Mutex
	lastScan   time.Time
	known      []string

	engineTime        *prometheus.Desc
	engineCycles      *prometheus.Desc
	engineTotalCycles *prometheus.Desc
	memTotal          *prometheus.Desc
	memRes            *prometheus.Desc
	memShared         *prometheus.Desc
	memPurgeable      *prometheus.Desc
	memActive         *prometheus.Desc
	engineCapacity    *prometheus.Desc
	dropped           *prometheus.Desc
}

func NewFdinfo(procRoot string, topN int) *Fdinfo {
	engineLabels := []string{"pci", "driver", "pid", "comm", "engine"}
	memLabels := []string{"pci", "driver", "pid", "comm", "region"}
	return &Fdinfo{
		procRoot: procRoot,
		topN:     topN,
		engineTime: prometheus.NewDesc(
			prometheus.BuildFQName(Namespace, "client", "engine_time_seconds_total"),
			"Cumulative per-client GPU engine time, parsed from drm-engine-* fdinfo keys.",
			engineLabels, nil,
		),
		engineCycles: prometheus.NewDesc(
			prometheus.BuildFQName(Namespace, "client", "engine_cycles_total"),
			"Cumulative per-client GPU engine busy cycles, parsed from drm-cycles-* fdinfo keys.",
			engineLabels, nil,
		),
		engineTotalCycles: prometheus.NewDesc(
			prometheus.BuildFQName(Namespace, "client", "engine_total_cycles_total"),
			"Cumulative GPU engine cycles elapsed, parsed from drm-total-cycles-* fdinfo keys.",
			engineLabels, nil,
		),
		memTotal: prometheus.NewDesc(
			prometheus.BuildFQName(Namespace, "client", "memory_total_bytes"),
			"Total memory allocated by the client, per region.",
			memLabels, nil,
		),
		memRes: prometheus.NewDesc(
			prometheus.BuildFQName(Namespace, "client", "memory_resident_bytes"),
			"Resident memory by the client, per region.",
			memLabels, nil,
		),
		memShared: prometheus.NewDesc(
			prometheus.BuildFQName(Namespace, "client", "memory_shared_bytes"),
			"Shared memory by the client, per region.",
			memLabels, nil,
		),
		memPurgeable: prometheus.NewDesc(
			prometheus.BuildFQName(Namespace, "client", "memory_purgeable_bytes"),
			"Memory of the client the kernel may discard (drm-purgeable-*), per region.",
			memLabels, nil,
		),
		memActive: prometheus.NewDesc(
			prometheus.BuildFQName(Namespace, "client", "memory_active_bytes"),
			"Memory of the client in use by submitted GPU work (drm-active-*), per region.",
			memLabels, nil,
		),
		engineCapacity: prometheus.NewDesc(
			prometheus.BuildFQName(Namespace, "client", "engine_capacity"),
			"Engines of this class visible to the client (drm-engine-capacity-*; busy time is summed over them, so divide by it for utilisation).",
			engineLabels, nil,
		),
		dropped: prometheus.NewDesc(
			prometheus.BuildFQName(Namespace, "client", "dropped_processes"),
			"Number of GPU-using processes whose metrics were dropped due to the --collector.fdinfo.top-n cap.",
			nil, nil,
		),
	}
}

func (c *Fdinfo) Name() string { return "fdinfo" }

func (c *Fdinfo) Available(gpus []discovery.GPU) bool { return len(gpus) > 0 }

type procKey struct{ pid, comm, driver, pci string }

type procData struct {
	engine      map[string]uint64
	cycles      map[string]uint64
	totalCycles map[string]uint64
	total       map[string]uint64
	res         map[string]uint64
	shared      map[string]uint64
	purgeable   map[string]uint64
	active      map[string]uint64
	capacity    map[string]uint64
}

func newProcData() *procData {
	return &procData{
		engine:      map[string]uint64{},
		cycles:      map[string]uint64{},
		totalCycles: map[string]uint64{},
		total:       map[string]uint64{},
		res:         map[string]uint64{},
		shared:      map[string]uint64{},
		purgeable:   map[string]uint64{},
		active:      map[string]uint64{},
		capacity:    map[string]uint64{},
	}
}

func (c *Fdinfo) Update(ctx context.Context, ch chan<- prometheus.Metric) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	full := c.Rescan <= 0 || time.Since(c.lastScan) >= c.Rescan
	pids := c.known
	if c.DrmClients != "" {
		full = false
		pids = drmClientPIDs(c.DrmClients)
	} else if full {
		entries, err := os.ReadDir(c.procRoot)
		if err != nil {
			return err
		}
		pids = pids[:0:0]
		for _, e := range entries {
			if _, err := strconv.Atoi(e.Name()); err == nil && e.IsDir() {
				pids = append(pids, e.Name())
			}
		}
	}

	procs := map[procKey]*procData{}
	seen := map[string]bool{}

	for _, pid := range pids {
		if c.ContainersOnly && !inContainer(filepath.Join(c.procRoot, pid, "cgroup")) {
			continue
		}
		comm := readComm(filepath.Join(c.procRoot, pid, "comm"))
		fdinfoDir := filepath.Join(c.procRoot, pid, "fdinfo")
		fds, err := os.ReadDir(fdinfoDir)
		if err != nil {
			continue
		}
		for _, fd := range fds {
			// Cheap prefilter: only DRM nodes carry drm-* keys. readlink needs
			// the same access as fdinfo, so a failure falls through to parsing.
			if target, err := os.Readlink(filepath.Join(c.procRoot, pid, "fd", fd.Name())); err == nil &&
				!strings.HasPrefix(target, "/dev/dri/") {
				continue
			}
			data := parseFdinfo(filepath.Join(fdinfoDir, fd.Name()))
			driver, ok := data["drm-driver"]
			if !ok {
				continue
			}
			if driver != "i915" && driver != "xe" {
				continue
			}
			seen[pid] = true
			key := procKey{pid: pid, comm: comm, driver: driver, pci: data["drm-pdev"]}
			pd, ok := procs[key]
			if !ok {
				pd = newProcData()
				procs[key] = pd
			}
			for k, v := range data {
				switch {
				case strings.HasPrefix(k, "drm-engine-capacity-"):
					pd.capacity[strings.TrimPrefix(k, "drm-engine-capacity-")] = parseNs(v)
				case strings.HasPrefix(k, "drm-engine-"):
					pd.engine[strings.TrimPrefix(k, "drm-engine-")] += parseNs(v)
				case strings.HasPrefix(k, "drm-cycles-"):
					pd.cycles[strings.TrimPrefix(k, "drm-cycles-")] += parseNs(v)
				case strings.HasPrefix(k, "drm-total-cycles-"):
					pd.totalCycles[strings.TrimPrefix(k, "drm-total-cycles-")] += parseNs(v)
				case strings.HasPrefix(k, "drm-total-"):
					pd.total[strings.TrimPrefix(k, "drm-total-")] += parseBytes(v)
				case strings.HasPrefix(k, "drm-resident-"):
					pd.res[strings.TrimPrefix(k, "drm-resident-")] += parseBytes(v)
				case strings.HasPrefix(k, "drm-shared-"):
					pd.shared[strings.TrimPrefix(k, "drm-shared-")] += parseBytes(v)
				case strings.HasPrefix(k, "drm-purgeable-"):
					pd.purgeable[strings.TrimPrefix(k, "drm-purgeable-")] += parseBytes(v)
				case strings.HasPrefix(k, "drm-active-"):
					pd.active[strings.TrimPrefix(k, "drm-active-")] += parseBytes(v)
				}
			}
		}
	}

	if full {
		c.known = c.known[:0]
		for _, pid := range pids {
			if seen[pid] {
				c.known = append(c.known, pid)
			}
		}
		c.lastScan = time.Now()
	}

	keys := topNByActivity(procs, c.topN)
	dropped := 0
	if c.topN > 0 && len(procs) > c.topN {
		dropped = len(procs) - c.topN
	}

	for _, k := range keys {
		pd := procs[k]
		for engine, ns := range pd.engine {
			ch <- prometheus.MustNewConstMetric(c.engineTime, prometheus.CounterValue,
				float64(ns)/1e9, k.pci, k.driver, k.pid, k.comm, engine)
		}
		for engine, v := range pd.cycles {
			ch <- prometheus.MustNewConstMetric(c.engineCycles, prometheus.CounterValue,
				float64(v), k.pci, k.driver, k.pid, k.comm, engine)
		}
		for engine, v := range pd.totalCycles {
			ch <- prometheus.MustNewConstMetric(c.engineTotalCycles, prometheus.CounterValue,
				float64(v), k.pci, k.driver, k.pid, k.comm, engine)
		}
		for region, v := range pd.total {
			ch <- prometheus.MustNewConstMetric(c.memTotal, prometheus.GaugeValue,
				float64(v), k.pci, k.driver, k.pid, k.comm, region)
		}
		for region, v := range pd.res {
			ch <- prometheus.MustNewConstMetric(c.memRes, prometheus.GaugeValue,
				float64(v), k.pci, k.driver, k.pid, k.comm, region)
		}
		for region, v := range pd.shared {
			ch <- prometheus.MustNewConstMetric(c.memShared, prometheus.GaugeValue,
				float64(v), k.pci, k.driver, k.pid, k.comm, region)
		}
		for region, v := range pd.purgeable {
			ch <- prometheus.MustNewConstMetric(c.memPurgeable, prometheus.GaugeValue,
				float64(v), k.pci, k.driver, k.pid, k.comm, region)
		}
		for region, v := range pd.active {
			ch <- prometheus.MustNewConstMetric(c.memActive, prometheus.GaugeValue,
				float64(v), k.pci, k.driver, k.pid, k.comm, region)
		}
		for engine, v := range pd.capacity {
			ch <- prometheus.MustNewConstMetric(c.engineCapacity, prometheus.GaugeValue,
				float64(v), k.pci, k.driver, k.pid, k.comm, engine)
		}
	}
	ch <- prometheus.MustNewConstMetric(c.dropped, prometheus.GaugeValue, float64(dropped))
	return nil
}

func topNByActivity(procs map[procKey]*procData, n int) []procKey {
	keys := make([]procKey, 0, len(procs))
	for k := range procs {
		keys = append(keys, k)
	}
	if n <= 0 || len(keys) <= n {
		return keys
	}
	totals := make(map[procKey]uint64, len(procs))
	for k, pd := range procs {
		var t uint64
		for _, v := range pd.engine {
			t += v
		}
		totals[k] = t
	}
	sort.Slice(keys, func(i, j int) bool {
		if totals[keys[i]] != totals[keys[j]] {
			return totals[keys[i]] > totals[keys[j]]
		}
		a, b := keys[i], keys[j]
		if a.pid != b.pid {
			return a.pid < b.pid
		}
		if a.comm != b.comm {
			return a.comm < b.comm
		}
		if a.driver != b.driver {
			return a.driver < b.driver
		}
		return a.pci < b.pci
	})
	return keys[:n]
}

// drmClientPIDs returns the distinct tgids listed in <dir>/*/clients (DRM
// debugfs: a header line, then "command tgid dev master a uid magic").
func drmClientPIDs(dir string) []string {
	files, _ := filepath.Glob(filepath.Join(dir, "*", "clients"))
	seen := map[string]bool{}
	var out []string
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		for _, line := range strings.Split(string(b), "\n") {
			fields := strings.Fields(line)
			if len(fields) < 2 {
				continue
			}
			if _, err := strconv.Atoi(fields[1]); err != nil || seen[fields[1]] {
				continue
			}
			seen[fields[1]] = true
			out = append(out, fields[1])
		}
	}
	sort.Strings(out)
	return out
}

// containerCgroup matches a path segment that is a 64-hex container id, bare
// or with a runtime prefix/.scope suffix: Docker (systemd and cgroupfs
// drivers), Podman, CRI-O, containerd CRI; plus Kubernetes pods. Seen from a
// container's private cgroup namespace, a sibling container under the cgroupfs
// driver ("/docker/<id>") reads "/../<id>": the "docker" segment is above the
// namespace root, so the id itself is the match. Host services (systemd units,
// containerd.service with the shims) have no such segment.
var containerCgroup = regexp.MustCompile(`(?m)/((docker|libpod|crio|cri-containerd)-)?[0-9a-f]{64}(\.scope)?(/|$)|kubepods`)

func inContainer(cgroupPath string) bool {
	b, err := os.ReadFile(cgroupPath)
	return err == nil && containerCgroup.Match(b)
}

func readComm(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

func parseFdinfo(path string) map[string]string {
	out := map[string]string{}
	f, err := os.Open(path)
	if err != nil {
		return out
	}
	defer func() { _ = f.Close() }()
	s := bufio.NewScanner(f)
	for s.Scan() {
		line := s.Text()
		if !strings.HasPrefix(line, "drm-") {
			continue
		}
		idx := strings.IndexByte(line, ':')
		if idx <= 0 {
			continue
		}
		out[line[:idx]] = strings.TrimSpace(line[idx+1:])
	}
	return out
}

func parseNs(v string) uint64 {
	v = strings.TrimSpace(v)
	v = strings.TrimSuffix(v, " ns")
	n, _ := strconv.ParseUint(v, 10, 64)
	return n
}

func parseBytes(v string) uint64 {
	v = strings.TrimSpace(v)
	parts := strings.Fields(v)
	if len(parts) == 0 {
		return 0
	}
	n, _ := strconv.ParseUint(parts[0], 10, 64)
	if len(parts) >= 2 {
		switch strings.ToLower(parts[1]) {
		case "kib":
			n *= 1024
		case "mib":
			n *= 1024 * 1024
		case "gib":
			n *= 1024 * 1024 * 1024
		}
	}
	return n
}
