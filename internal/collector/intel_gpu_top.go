package collector

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os/exec"
	"sync"
	"sync/atomic"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/xsaveopt/intel_gpu_exporter/internal/discovery"
)

type IntelGPUTop struct {
	binPath string
	log     *slog.Logger

	mu     sync.Mutex
	latest *gpuTopSample
	last   atomic.Int64
	exited atomic.Bool
	cancel context.CancelFunc

	freqReq *prometheus.Desc
	freqAct *prometheus.Desc
	rc6     *prometheus.Desc
	power   *prometheus.Desc
	engine  *prometheus.Desc
	imcR    *prometheus.Desc
	imcW    *prometheus.Desc
	irqs    *prometheus.Desc
}

type gpuTopSample struct {
	Period struct {
		Duration float64 `json:"duration"`
		Unit     string  `json:"unit"`
	} `json:"period"`
	Frequency struct {
		Requested float64 `json:"requested"`
		Actual    float64 `json:"actual"`
	} `json:"frequency"`
	RC6 struct {
		Value float64 `json:"value"`
	} `json:"rc6"`
	Power map[string]any `json:"power"`
	IMCBW struct {
		Reads  float64 `json:"reads"`
		Writes float64 `json:"writes"`
	} `json:"imc-bandwidth"`
	Interrupts struct {
		Count float64 `json:"count"`
	} `json:"interrupts"`
	Engines map[string]struct {
		Busy float64 `json:"busy"`
		Sema float64 `json:"sema"`
		Wait float64 `json:"wait"`
	} `json:"engines"`
}

func NewIntelGPUTop(binPath string, log *slog.Logger) *IntelGPUTop {
	lbls := []string{"source"}
	d := func(name, help string, extra ...string) *prometheus.Desc {
		return prometheus.NewDesc(prometheus.BuildFQName(Namespace, "gputop", name), help, append(lbls, extra...), nil)
	}
	return &IntelGPUTop{
		binPath: binPath,
		log:     log,
		freqReq: d("frequency_requested_mhz", "Requested GT frequency (intel_gpu_top)."),
		freqAct: d("frequency_actual_mhz", "Actual GT frequency (intel_gpu_top)."),
		rc6:     d("rc6_ratio", "RC6 residency ratio (0..100)."),
		power:   d("power_watts", "Power draw reported by intel_gpu_top.", "rail"),
		engine:  d("engine_busy_ratio", "Per-engine busy percentage.", "engine", "metric"),
		imcR:    d("imc_bandwidth_read_mibps", "IMC read bandwidth."),
		imcW:    d("imc_bandwidth_write_mibps", "IMC write bandwidth."),
		irqs:    d("interrupts_per_second", "Interrupts per sample period."),
	}
}

func (c *IntelGPUTop) Name() string { return "intel_gpu_top" }

func (c *IntelGPUTop) Available(gpus []discovery.GPU) bool {
	if _, err := exec.LookPath(c.binPath); err != nil {
		return false
	}
	for _, g := range gpus {
		if g.Driver == discovery.DriverI915 {
			return true
		}
	}
	return false
}

func (c *IntelGPUTop) Start(parent context.Context) error {
	ctx, cancel := context.WithCancel(parent)
	c.cancel = cancel
	cmd := exec.CommandContext(ctx, c.binPath, "-J", "-s", "1000")
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		cancel()
		return err
	}
	if err := cmd.Start(); err != nil {
		cancel()
		return err
	}
	c.log.Info("intel_gpu_top started", "pid", cmd.Process.Pid, "bin", c.binPath)
	var readers sync.WaitGroup
	readers.Add(2)
	go func() {
		defer readers.Done()
		c.consume(stdout)
		_, _ = io.Copy(io.Discard, stdout)
	}()
	go func() {
		defer readers.Done()
		c.drainStderr(stderr)
	}()
	go func() {
		readers.Wait()
		err := cmd.Wait()
		c.exited.Store(true)
		samples := c.last.Load() != 0
		c.log.Warn("intel_gpu_top exited",
			"err", err, "got_any_sample", samples,
			"hint", "if got_any_sample=false, check stderr above; common causes: CAP_PERFMON missing, perf_event_paranoid too high, or systemd SystemCallFilter blocking perf_event_open")
	}()
	return nil
}

func (c *IntelGPUTop) drainStderr(r io.Reader) {
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		c.log.Warn("intel_gpu_top stderr", "line", sc.Text())
	}
}

func (c *IntelGPUTop) Stop() {
	if c.cancel != nil {
		c.cancel()
	}
}

func (c *IntelGPUTop) consume(r io.Reader) {

	br := bufio.NewReader(r)
	dec := json.NewDecoder(br)
	if t, err := dec.Token(); err != nil {
		c.log.Warn("intel_gpu_top read opening token", "err", err)
		return
	} else if d, ok := t.(json.Delim); !ok || d != '[' {
		c.log.Warn("intel_gpu_top unexpected opening token", "token", t)
		return
	}
	for dec.More() {
		var s gpuTopSample
		if err := dec.Decode(&s); err != nil {
			if err == io.EOF {
				return
			}
			c.log.Warn("intel_gpu_top decode", "err", err)
			time.Sleep(500 * time.Millisecond)
			continue
		}
		c.mu.Lock()
		c.latest = &s
		c.mu.Unlock()
		c.last.Store(time.Now().UnixNano())
	}
}

func (c *IntelGPUTop) Update(ctx context.Context, ch chan<- prometheus.Metric) error {
	c.mu.Lock()
	s := c.latest
	c.mu.Unlock()
	if s == nil {
		return fmt.Errorf("no intel_gpu_top sample yet")
	}
	if c.exited.Load() {
		return fmt.Errorf("intel_gpu_top exited")
	}
	const src = "intel_gpu_top"
	ch <- prometheus.MustNewConstMetric(c.freqReq, prometheus.GaugeValue, s.Frequency.Requested, src)
	ch <- prometheus.MustNewConstMetric(c.freqAct, prometheus.GaugeValue, s.Frequency.Actual, src)
	ch <- prometheus.MustNewConstMetric(c.rc6, prometheus.GaugeValue, s.RC6.Value, src)
	ch <- prometheus.MustNewConstMetric(c.imcR, prometheus.GaugeValue, s.IMCBW.Reads, src)
	ch <- prometheus.MustNewConstMetric(c.imcW, prometheus.GaugeValue, s.IMCBW.Writes, src)
	ch <- prometheus.MustNewConstMetric(c.irqs, prometheus.GaugeValue, s.Interrupts.Count, src)
	for rail, v := range s.Power {
		f, ok := v.(float64)
		if !ok {
			continue
		}
		ch <- prometheus.MustNewConstMetric(c.power, prometheus.GaugeValue, f, src, rail)
	}
	for name, e := range s.Engines {
		ch <- prometheus.MustNewConstMetric(c.engine, prometheus.GaugeValue, e.Busy, src, name, "busy")
		ch <- prometheus.MustNewConstMetric(c.engine, prometheus.GaugeValue, e.Sema, src, name, "sema")
		ch <- prometheus.MustNewConstMetric(c.engine, prometheus.GaugeValue, e.Wait, src, name, "wait")
	}
	return nil
}
