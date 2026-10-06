package config

import (
	"errors"
	"flag"
	"os"
	"testing"
	"time"
)

func quietStderr(t *testing.T) {
	t.Helper()
	devnull, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		t.Fatalf("open %s: %v", os.DevNull, err)
	}
	saved := os.Stderr
	os.Stderr = devnull
	t.Cleanup(func() {
		os.Stderr = saved
		_ = devnull.Close()
	})
}

func TestParseArgsDefaults(t *testing.T) {
	c, err := ParseArgs("intel_gpu_exporter", nil)
	if err != nil {
		t.Fatalf("ParseArgs: %v", err)
	}
	want := Config{
		ListenAddr:      ":9404",
		MetricsPath:     "/metrics",
		SysfsRoot:       "/sys",
		ProcRoot:        "/proc",
		HwmonRoot:       "/sys/class/hwmon",
		DevRoot:         "/dev",
		ScrapeTimeout:   5 * time.Second,
		EnableFdinfo:    true,
		FdinfoTopN:      32,
		EnableGpuTop:    true,
		EnablePMU:       true,
		IntelGpuTopPath: "intel_gpu_top",
		LogLevel:        "info",
	}
	if *c != want {
		t.Errorf("got %+v, want %+v", *c, want)
	}
}

func TestParseArgsOverrides(t *testing.T) {
	c, err := ParseArgs("intel_gpu_exporter", []string{
		"-web.listen-address=127.0.0.1:9999",
		"-web.telemetry-path=/m",
		"-path.sysfs=/fake/sys",
		"-path.procfs=/fake/proc",
		"-path.hwmon=/fake/sys/class/hwmon",
		"-path.dev=/fake/dev",
		"-scrape.timeout=250ms",
		"-collector.fdinfo=false",
		"-collector.fdinfo.top-n=0",
		"-collector.intel-gpu-top=false",
		"-collector.pmu=false",
		"-collector.intel-gpu-top.path=/usr/bin/intel_gpu_top",
		"-log.level=debug",
	})
	if err != nil {
		t.Fatalf("ParseArgs: %v", err)
	}
	want := Config{
		ListenAddr:      "127.0.0.1:9999",
		MetricsPath:     "/m",
		SysfsRoot:       "/fake/sys",
		ProcRoot:        "/fake/proc",
		HwmonRoot:       "/fake/sys/class/hwmon",
		DevRoot:         "/fake/dev",
		ScrapeTimeout:   250 * time.Millisecond,
		EnableFdinfo:    false,
		FdinfoTopN:      0,
		EnableGpuTop:    false,
		EnablePMU:       false,
		IntelGpuTopPath: "/usr/bin/intel_gpu_top",
		LogLevel:        "debug",
	}
	if *c != want {
		t.Errorf("got %+v, want %+v", *c, want)
	}
}

func TestParseArgsDoubleDashAccepted(t *testing.T) {
	c, err := ParseArgs("intel_gpu_exporter", []string{"--log.level", "warn", "--collector.fdinfo.top-n", "8"})
	if err != nil {
		t.Fatalf("ParseArgs: %v", err)
	}
	if c.LogLevel != "warn" {
		t.Errorf("LogLevel = %q, want %q", c.LogLevel, "warn")
	}
	if c.FdinfoTopN != 8 {
		t.Errorf("FdinfoTopN = %d, want 8", c.FdinfoTopN)
	}
}

func TestParseArgsErrors(t *testing.T) {
	cases := []struct {
		name string
		args []string
	}{
		{"unknown flag", []string{"-nope"}},
		{"bad duration", []string{"-scrape.timeout=soon"}},
		{"bad int", []string{"-collector.fdinfo.top-n=many"}},
		{"bad bool", []string{"-collector.pmu=maybe"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			quietStderr(t)
			if _, err := ParseArgs("intel_gpu_exporter", tc.args); err == nil {
				t.Fatal("expected an error")
			}
		})
	}
}

func TestParseArgsHelp(t *testing.T) {
	quietStderr(t)
	_, err := ParseArgs("intel_gpu_exporter", []string{"-h"})
	if !errors.Is(err, flag.ErrHelp) {
		t.Fatalf("got %v, want flag.ErrHelp", err)
	}
}

func TestParseArgsIsIndependentOfGlobalFlagSet(t *testing.T) {
	for range 2 {
		if _, err := ParseArgs("intel_gpu_exporter", []string{"-log.level=error"}); err != nil {
			t.Fatalf("ParseArgs: %v", err)
		}
	}
	if flag.CommandLine.Lookup("log.level") != nil {
		t.Error("ParseArgs registered flags on the global flag set")
	}
}
