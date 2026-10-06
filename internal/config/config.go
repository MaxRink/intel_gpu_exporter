package config

import (
	"errors"
	"flag"
	"os"
	"time"
)

type Config struct {
	ListenAddr      string
	MetricsPath     string
	SysfsRoot       string
	ProcRoot        string
	HwmonRoot       string
	DevRoot         string
	ScrapeTimeout   time.Duration
	EnableFdinfo    bool
	FdinfoTopN      int
	FdinfoRescan    time.Duration
	EnableGpuTop    bool
	EnablePMU       bool
	IntelGpuTopPath string
	LogLevel        string
}

func Parse() *Config {
	c, err := ParseArgs(os.Args[0], os.Args[1:])
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			os.Exit(0)
		}
		os.Exit(2)
	}
	return c
}

func ParseArgs(name string, args []string) (*Config, error) {
	c := &Config{}
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.StringVar(&c.ListenAddr, "web.listen-address", ":9404", "address to listen on for HTTP requests")
	fs.StringVar(&c.MetricsPath, "web.telemetry-path", "/metrics", "path under which to expose metrics")
	fs.StringVar(&c.SysfsRoot, "path.sysfs", "/sys", "sysfs mountpoint")
	fs.StringVar(&c.ProcRoot, "path.procfs", "/proc", "procfs mountpoint")
	fs.StringVar(&c.DevRoot, "path.dev", "/dev", "devfs mountpoint (i915 render nodes for memory region queries)")
	fs.StringVar(&c.HwmonRoot, "path.hwmon", "/sys/class/hwmon", "hwmon root")
	fs.DurationVar(&c.ScrapeTimeout, "scrape.timeout", 5*time.Second, "maximum time a single scrape may take")
	fs.BoolVar(&c.EnableFdinfo, "collector.fdinfo", true, "enable per-process DRM fdinfo collector")
	fs.IntVar(&c.FdinfoTopN, "collector.fdinfo.top-n", 32, "fdinfo: cap per-process series at the top N processes by aggregate engine time (0 = unlimited)")
	fs.DurationVar(&c.FdinfoRescan, "collector.fdinfo.rescan-interval", 0, "fdinfo: walk all of /proc at most this often and re-read only known GPU clients in between (0 = every scrape)")
	fs.BoolVar(&c.EnableGpuTop, "collector.intel-gpu-top", true, "enable intel_gpu_top fallback collector when PMU is unavailable")
	fs.BoolVar(&c.EnablePMU, "collector.pmu", true, "enable i915/xe PMU collector via perf_event_open")
	fs.StringVar(&c.IntelGpuTopPath, "collector.intel-gpu-top.path", "intel_gpu_top", "path to intel_gpu_top binary")
	fs.StringVar(&c.LogLevel, "log.level", "info", "log level (debug|info|warn|error)")
	if err := fs.Parse(args); err != nil {
		return nil, err
	}
	return c, nil
}
