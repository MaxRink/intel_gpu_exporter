//go:build linux

package collector

import (
	"context"
	"encoding/binary"
	"errors"
	"log/slog"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"golang.org/x/sys/unix"
)

func TestDecodeEventName(t *testing.T) {
	cases := []struct {
		in, family, engine, kind string
	}{
		{"rcs0-busy", "engine", "rcs0", "busy"},
		{"vcs1-sema", "engine", "vcs1", "sema"},
		{"bcs0-wait", "engine", "bcs0", "wait"},
		{"actual-frequency", "frequency", "", "actual-frequency"},
		{"requested-frequency", "frequency", "", "requested-frequency"},
		{"gt-actual-frequency", "frequency", "", "actual-frequency"},
		{"rc6-residency", "rc6", "", ""},
		{"gt-c6-residency", "rc6", "", ""},
		{"interrupts", "interrupts", "", ""},
		{"software-gt-awake-time", "awake", "", ""},
		{"engine-active-ticks", "engine", "", "active"},
		{"engine-total-ticks", "engine", "", "total"},
		{"something-new", "other", "", ""},
		{"", "other", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			family, engine, kind := decodeEventName(tc.in)
			if family != tc.family || engine != tc.engine || kind != tc.kind {
				t.Errorf("got (%q, %q, %q), want (%q, %q, %q)",
					family, engine, kind, tc.family, tc.engine, tc.kind)
			}
		})
	}
}

func TestParseBitRange(t *testing.T) {
	cases := []struct {
		in     string
		lo, hi uint
	}{
		{"0-20", 0, 20},
		{"60-63", 60, 63},
		{"7", 7, 7},
		{"", 0, 0},
		{"x-y", 0, 0},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			lo, hi := parseBitRange(tc.in)
			if lo != tc.lo || hi != tc.hi {
				t.Errorf("got (%d, %d), want (%d, %d)", lo, hi, tc.lo, tc.hi)
			}
		})
	}
}

func TestBitMask(t *testing.T) {
	cases := []struct {
		width uint
		want  uint64
	}{
		{0, 0},
		{1, 0x1},
		{4, 0xf},
		{12, 0xfff},
		{63, 0x7fffffffffffffff},
		{64, ^uint64(0)},
		{80, ^uint64(0)},
	}
	for _, tc := range cases {
		if got := bitMask(tc.width); got != tc.want {
			t.Errorf("bitMask(%d) = %#x, want %#x", tc.width, got, tc.want)
		}
	}
}

func TestEncodeEventLine(t *testing.T) {
	format := map[string]formatSpec{
		"event": {shift: 0, mask: 0xfff},
		"gt":    {shift: 60, mask: 0xf},
	}
	cases := []struct {
		name   string
		in     string
		want   uint64
		reason string
	}{
		{"raw config", "config=0x100000", 0x100000, ""},
		{"no hex prefix", "config=10", 0x10, ""},
		{"empty", "", 0, ""},
		{"trailing comma", "config=0x1, ", 0x1, ""},
		{"format keys", "event=0x02,gt=1", 0x2 | 1<<60, ""},
		{"masked", "event=0x1fff", 0xfff, ""},
		{"config and format", "config=0x100,event=0x2", 0x102, ""},
		{"no equals", "event", 0, "malformed token"},
		{"bad hex", "config=zz", 0, "bad hex value"},
		{"config1", "config1=0x1", 0, "config1"},
		{"config2", "config2=0x1", 0, "config2"},
		{"unknown key", "umask=0x1", 0, "unknown format key"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, reason := encodeEventLine(tc.in, format)
			if tc.reason == "" {
				if reason != "" {
					t.Fatalf("unexpected reason %q", reason)
				}
				if got != tc.want {
					t.Errorf("got %#x, want %#x", got, tc.want)
				}
				return
			}
			if !strings.Contains(reason, tc.reason) {
				t.Errorf("reason = %q, want it to contain %q", reason, tc.reason)
			}
			if got != 0 {
				t.Errorf("got %#x on failure, want 0", got)
			}
		})
	}
}

func TestLoadFormats(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{
		"event":   "config:0-11\n",
		"gt":      "config:60-63\n",
		"bit":     "config:5",
		"other":   "config1:0-7\n",
		"nocolon": "garbage\n",
		"sub/x":   "config:0-1\n",
	})
	got, err := loadFormats(dir)
	if err != nil {
		t.Fatalf("loadFormats: %v", err)
	}
	want := map[string]formatSpec{
		"event": {shift: 0, mask: 0xfff},
		"gt":    {shift: 60, mask: 0xf},
		"bit":   {shift: 5, mask: 0x1},
	}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for k, w := range want {
		if got[k] != w {
			t.Errorf("%s = %+v, want %+v", k, got[k], w)
		}
	}
}

func TestLoadFormatsMissingDir(t *testing.T) {
	got, err := loadFormats(filepath.Join(t.TempDir(), "missing"))
	if err != nil {
		t.Fatalf("loadFormats: %v", err)
	}
	if got == nil || len(got) != 0 {
		t.Errorf("got %v, want empty non-nil map", got)
	}
}

func TestLoadFormatsNotADir(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{"format": "config:0-1"})
	if _, err := loadFormats(filepath.Join(dir, "format")); err == nil {
		t.Error("expected an error when format is a regular file")
	}
}

func TestLoadEvents(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{
		"rc6-residency":       "config=0x100001\n",
		"rc6-residency.unit":  "ns\n",
		"rc6-residency.scale": "1\n",
		"bcs0-busy":           "event=0x2,gt=1\n",
		"uses-config2":        "config2=0x1\n",
		"broken":              "junk\n",
		"nested/x":            "config=0x1\n",
	})
	format := map[string]formatSpec{
		"event": {shift: 0, mask: 0xfff},
		"gt":    {shift: 60, mask: 0xf},
	}
	events, dropped, err := loadEvents(dir, format)
	if err != nil {
		t.Fatalf("loadEvents: %v", err)
	}
	wantEvents := map[string]uint64{
		"rc6-residency": 0x100001,
		"bcs0-busy":     0x2 | 1<<60,
	}
	if len(events) != len(wantEvents) {
		t.Fatalf("events = %v, want %v", events, wantEvents)
	}
	for k, v := range wantEvents {
		if events[k] != v {
			t.Errorf("%s = %#x, want %#x", k, events[k], v)
		}
	}
	var names []string
	for _, d := range dropped {
		names = append(names, d.name)
		if d.reason == "" {
			t.Errorf("%s: dropped without a reason", d.name)
		}
	}
	slices.Sort(names)
	if !slices.Equal(names, []string{"broken", "uses-config2"}) {
		t.Errorf("dropped = %v", names)
	}
}

func TestLoadEventsMissingDir(t *testing.T) {
	events, dropped, err := loadEvents(filepath.Join(t.TempDir(), "missing"), nil)
	if err != nil {
		t.Fatalf("loadEvents: %v", err)
	}
	if events == nil || len(events) != 0 || len(dropped) != 0 {
		t.Errorf("got (%v, %v), want empty", events, dropped)
	}
}

func TestLoadEventsNotADir(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{"events": "config=0x1"})
	if _, _, err := loadEvents(filepath.Join(dir, "events"), nil); err == nil {
		t.Error("expected an error when events is a regular file")
	}
}

func TestReadCpumaskFirst(t *testing.T) {
	cases := []struct {
		name, content string
		missing       bool
		want          int
	}{
		{name: "missing", missing: true, want: 0},
		{name: "empty", content: "\n", want: 0},
		{name: "single", content: "3\n", want: 3},
		{name: "range", content: "2-5\n", want: 2},
		{name: "list", content: "4,6\n", want: 4},
		{name: "list of ranges", content: "8-11,16-19\n", want: 8},
		{name: "garbage", content: "x\n", want: 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			if !tc.missing {
				writeFiles(t, dir, map[string]string{"cpumask": tc.content})
			}
			if got := readCpumaskFirst(filepath.Join(dir, "cpumask")); got != tc.want {
				t.Errorf("got %d, want %d", got, tc.want)
			}
		})
	}
}

func TestLoadPMU(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{
		"type":             "19\n",
		"cpumask":          "3,5\n",
		"format/event":     "config:0-11\n",
		"format/gt":        "config:60-63\n",
		"events/rcs0-busy": "event=0x1,gt=0\n",
		"events/bad":       "config1=0x1\n",
	})
	dev, err := loadPMU(dir, "xe_0000_03_00.0")
	if err != nil {
		t.Fatalf("loadPMU: %v", err)
	}
	if dev.name != "xe_0000_03_00.0" || dev.typeID != 19 || dev.cpu != 3 {
		t.Errorf("got name=%q type=%d cpu=%d", dev.name, dev.typeID, dev.cpu)
	}
	if len(dev.events) != 1 || dev.events["rcs0-busy"] != 0x1 {
		t.Errorf("events = %v", dev.events)
	}
	if len(dev.droppedEvents) != 1 || dev.droppedEvents[0].name != "bad" || dev.droppedEvents[0].raw != "config1=0x1" {
		t.Errorf("dropped = %+v", dev.droppedEvents)
	}
	keys := slices.Clone(dev.formatKeys)
	slices.Sort(keys)
	if !slices.Equal(keys, []string{"event", "gt"}) {
		t.Errorf("formatKeys = %v", keys)
	}
}

func TestLoadPMUNoFormatOrEvents(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{"type": "7"})
	dev, err := loadPMU(dir, "i915")
	if err != nil {
		t.Fatalf("loadPMU: %v", err)
	}
	if dev.typeID != 7 || dev.cpu != 0 || len(dev.events) != 0 || len(dev.formatKeys) != 0 {
		t.Errorf("got %+v", dev)
	}
}

func TestLoadPMUErrors(t *testing.T) {
	cases := map[string]map[string]string{
		"missing type":     {"cpumask": "0"},
		"bad type":         {"type": "abc"},
		"type overflow":    {"type": "4294967296"},
		"format is a file": {"type": "7", "format": "config:0-1"},
		"events is a file": {"type": "7", "events": "config=0x1"},
	}
	for name, files := range cases {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			writeFiles(t, dir, files)
			if _, err := loadPMU(dir, "i915"); err == nil {
				t.Error("expected an error")
			}
		})
	}
}

func pmuPipe(t *testing.T, data []byte) int {
	t.Helper()
	var fds [2]int
	if err := unix.Pipe2(fds[:], unix.O_CLOEXEC); err != nil {
		t.Fatalf("pipe: %v", err)
	}
	if len(data) > 0 {
		if _, err := unix.Write(fds[1], data); err != nil {
			t.Fatalf("write: %v", err)
		}
	}
	_ = unix.Close(fds[1])
	return fds[0]
}

func counterBytes(v uint64) []byte {
	b := make([]byte, 8)
	binary.LittleEndian.PutUint64(b, v)
	return b
}

func closeFDs(t *testing.T, fds ...int) {
	t.Cleanup(func() {
		for _, fd := range fds {
			_ = unix.Close(fd)
		}
	})
}

func TestPerfRead(t *testing.T) {
	fd := pmuPipe(t, counterBytes(0x0102030405060708))
	closeFDs(t, fd)
	got, err := perfRead(fd)
	if err != nil {
		t.Fatalf("perfRead: %v", err)
	}
	if got != 0x0102030405060708 {
		t.Errorf("got %#x", got)
	}
}

func TestPerfReadShort(t *testing.T) {
	fd := pmuPipe(t, []byte{1, 2, 3})
	closeFDs(t, fd)
	if _, err := perfRead(fd); err == nil || !strings.Contains(err.Error(), "short read: 3") {
		t.Errorf("err = %v, want short read", err)
	}
}

func TestPerfReadBadFD(t *testing.T) {
	if _, err := perfRead(-1); !errors.Is(err, unix.EBADF) {
		t.Errorf("err = %v, want EBADF", err)
	}
}

func newTestPMU() *PMU {
	return NewPMU(slog.New(slog.DiscardHandler))
}

func TestPMUUpdate(t *testing.T) {
	busy := pmuPipe(t, counterBytes(123456))
	rc6 := pmuPipe(t, counterBytes(42))
	closeFDs(t, busy, rc6)
	p := newTestPMU()
	p.events = []*pmuEvent{
		{pmu: "i915", name: "rcs0-busy", fd: busy, family: "engine", engine: "rcs0", kind: "busy"},
		{pmu: "i915", name: "rc6-residency", fd: rc6, family: "rc6"},
	}
	assertSamples(t, p, []string{
		`intel_gpu_pmu_counter{engine="rcs0",event="rcs0-busy",family="engine",kind="busy",pmu="i915"} 123456`,
		`intel_gpu_pmu_counter{engine="",event="rc6-residency",family="rc6",kind="",pmu="i915"} 42`,
	})
}

func TestPMUUpdateReadErrors(t *testing.T) {
	good := pmuPipe(t, counterBytes(7))
	short := pmuPipe(t, []byte{1})
	closeFDs(t, good, short)
	p := newTestPMU()
	p.events = []*pmuEvent{
		{pmu: "i915", name: "short", fd: short, family: "other"},
		{pmu: "i915", name: "bad", fd: -1, family: "other"},
		{pmu: "i915", name: "interrupts", fd: good, family: "interrupts"},
	}
	ch := make(chan prometheus.Metric, 10)
	err := p.Update(context.Background(), ch)
	close(ch)
	if err == nil || !strings.Contains(err.Error(), "read i915/short") {
		t.Errorf("err = %v, want the first failing event", err)
	}
	var n int
	for range ch {
		n++
	}
	if n != 1 {
		t.Errorf("got %d metrics, want 1", n)
	}
}

func TestPMUUpdateNoEvents(t *testing.T) {
	ch := make(chan prometheus.Metric, 1)
	if err := newTestPMU().Update(context.Background(), ch); err == nil {
		t.Error("expected an error without events")
	}
}

func hostHasDRMPMU(t *testing.T) bool {
	t.Helper()
	pmus, err := discoverPMUs()
	return err == nil && len(pmus) > 0
}

func TestDiscoverPMUsIgnoresNonDRMPMUs(t *testing.T) {
	pmus, err := discoverPMUs()
	if err != nil {
		t.Skipf("event_source devices not readable here: %v", err)
	}
	for _, pm := range pmus {
		if pm.name != "i915" && !strings.HasPrefix(pm.name, "i915_") && !strings.HasPrefix(pm.name, "xe_") {
			t.Errorf("discoverPMUs returned non-DRM PMU %q", pm.name)
		}
	}
}

func TestPMUOpenWithoutDRMPMU(t *testing.T) {
	if hostHasDRMPMU(t) {
		t.Skip("host exposes an i915 or xe PMU")
	}
	p := newTestPMU()
	if err := p.open(); err == nil {
		t.Error("open should fail without an i915 or xe PMU")
	}
	if len(p.events) != 0 {
		t.Errorf("open registered %d events without a DRM PMU", len(p.events))
	}
	if p.Available(nil) {
		t.Error("Available should be false without an i915 or xe PMU")
	}
}

func TestPerfOpenSoftwareEvent(t *testing.T) {
	fd, err := perfOpen(unix.PERF_TYPE_SOFTWARE, unix.PERF_COUNT_SW_TASK_CLOCK, 0)
	if errors.Is(err, unix.EACCES) || errors.Is(err, unix.EPERM) || errors.Is(err, unix.ENOSYS) || errors.Is(err, unix.ENOENT) {
		t.Skipf("perf_event_open not permitted here: %v", err)
	}
	if err != nil {
		t.Fatalf("perfOpen: %v", err)
	}
	closeFDs(t, fd)
	if _, err := perfRead(fd); err != nil {
		t.Errorf("perfRead on an opened event: %v", err)
	}
}

func TestPerfOpenInvalidType(t *testing.T) {
	fd, err := perfOpen(0xfffffff0, 0, 0)
	if err == nil {
		_ = unix.Close(fd)
		t.Fatal("perfOpen should fail for an unknown PMU type")
	}
	if fd != -1 {
		t.Errorf("fd = %d on failure, want -1", fd)
	}
}

func TestPMUAvailableWithOpenEvents(t *testing.T) {
	fd := pmuPipe(t, nil)
	p := newTestPMU()
	p.events = []*pmuEvent{{pmu: "i915", name: "interrupts", fd: fd}}
	if p.Name() != "pmu" {
		t.Errorf("Name = %q", p.Name())
	}
	if !p.Available(nil) {
		t.Error("Available should be true when events are already open")
	}
	if err := p.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if p.events != nil {
		t.Errorf("events = %v after Close, want nil", p.events)
	}
	if err := p.Close(); err != nil {
		t.Errorf("second Close: %v", err)
	}
}
