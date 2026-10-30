package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/xsaveopt/intel_gpu_exporter/internal/collector"
	"github.com/xsaveopt/intel_gpu_exporter/internal/config"
	"github.com/xsaveopt/intel_gpu_exporter/internal/discovery"
	"github.com/xsaveopt/intel_gpu_exporter/internal/kernelinfo"
)

var version = "dev"

func main() {
	cfg := config.Parse()
	log := newLogger(cfg.LogLevel)
	log.Info("intel_gpu_exporter starting", "version", version)

	gpus, err := discovery.Discover(cfg.SysfsRoot)
	if err != nil {
		log.Error("GPU discovery failed", "err", err)
		os.Exit(1)
	}
	drivers := map[string]bool{}
	for _, g := range gpus {
		log.Info("discovered intel GPU",
			"card", g.Card, "pci", g.PCIAddr, "device", g.DeviceID,
			"driver", g.Driver, "tiles", len(g.Tiles), "hwmon", len(g.HwmonPaths))
		if g.Driver == discovery.DriverUnknown {
			log.Warn("device has no recognised driver bound; some metrics will be unavailable",
				"card", g.Card)
		}
		drivers[string(g.Driver)] = true
	}

	kver := kernelinfo.Detect()
	log.Info("running kernel detected", "version", kver)
	if level, desc, err := kernelinfo.PerfParanoid(); err == nil {
		evt := log.Info
		if level >= 2 {
			evt = log.Warn
		}
		evt("perf_event_paranoid", "level", level, "meaning", desc,
			"pmu_usable", level <= 1)
	}
	statuses := kernelinfo.Evaluate(kver, drivers)
	var avail, missing int
	for _, s := range statuses {
		if s.Available {
			log.Info("kernel feature available",
				"id", s.Feature.ID, "since", s.Feature.Since,
				"summary", s.Feature.Summary, "notes", s.Feature.Notes)
			avail++
		} else {
			log.Info("kernel feature NOT available",
				"id", s.Feature.ID, "since", s.Feature.Since,
				"reason", s.Reason, "summary", s.Feature.Summary)
			missing++
		}
	}
	log.Info("kernel feature summary", "available", avail, "unavailable", missing, "kernel", kver)

	mem := collector.NewMemory(gpus)
	mem.DevRoot = cfg.DevRoot
	sources := []collector.Source{
		collector.NewInfo(gpus),
		collector.NewI915Sysfs(gpus),
		collector.NewXeSysfs(gpus),
		collector.NewHwmon(gpus),
		collector.NewPCIe(gpus),
		mem,
		collector.NewEngines(gpus),
		collector.NewLevelZero(log),
	}
	if cfg.EnableFdinfo {
		sources = append(sources, collector.NewFdinfo(cfg.ProcRoot, cfg.FdinfoTopN))
	}
	var pmu *collector.PMU
	if cfg.EnablePMU {
		pmu = collector.NewPMU(log)
		sources = append(sources, pmu)
	}

	var gpuTop *collector.IntelGPUTop
	if cfg.EnableGpuTop {
		gpuTop = collector.NewIntelGPUTop(cfg.IntelGpuTopPath, log)
		sources = append(sources, gpuTop)
	}

	reg := collector.NewRegistry(log, gpus, cfg.ScrapeTimeout, sources...)

	promReg := prometheus.NewRegistry()
	promReg.MustRegister(collectors.NewGoCollector())
	promReg.MustRegister(collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))
	promReg.MustRegister(reg)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if gpuTop != nil && gpuTop.Available(gpus) {
		if err := gpuTop.Start(ctx); err != nil {
			log.Warn("intel_gpu_top failed to start, fallback disabled", "err", err)
		}
	}

	mux := http.NewServeMux()
	mux.Handle(cfg.MetricsPath, promhttp.HandlerFor(promReg, promhttp.HandlerOpts{
		ErrorLog: slogErrorLog{log},
		Registry: promReg,
	}))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`<html><head><title>intel_gpu_exporter</title></head>
<body><h1>intel_gpu_exporter</h1>
<p><a href="` + cfg.MetricsPath + `">metrics</a></p></body></html>`))
	})
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		if !reg.Healthy() {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte("degraded"))
			return
		}
		_, _ = w.Write([]byte("up"))
	})

	srv := &http.Server{
		Addr:              cfg.ListenAddr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		log.Info("listening", "addr", cfg.ListenAddr, "path", cfg.MetricsPath)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error("http server failed", "err", err)
			stop()
		}
	}()

	<-ctx.Done()
	log.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = srv.Shutdown(shutdownCtx)
	if gpuTop != nil {
		gpuTop.Stop()
	}
	if pmu != nil {
		_ = pmu.Close()
	}
}

func newLogger(level string) *slog.Logger {
	lvl := slog.LevelInfo
	switch level {
	case "debug":
		lvl = slog.LevelDebug
	case "warn":
		lvl = slog.LevelWarn
	case "error":
		lvl = slog.LevelError
	}
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: lvl}))
}

type slogErrorLog struct{ l *slog.Logger }

func (s slogErrorLog) Println(v ...any) { s.l.Error("promhttp", "msg", v) }
