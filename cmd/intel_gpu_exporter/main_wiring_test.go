package main

import (
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

const helperEnv = "INTEL_GPU_EXPORTER_TEST_MAIN"

func TestHelperMain(t *testing.T) {
	if os.Getenv(helperEnv) != "1" {
		t.Skip("helper process for the wiring tests")
	}
	args := os.Args
	for i, a := range args {
		if a == "--" {
			args = args[i+1:]
			break
		}
	}
	os.Args = append([]string{"intel_gpu_exporter"}, args...)
	main()
	os.Exit(0)
}

func fakeSysfs(t *testing.T) (root, drmPath string) {
	t.Helper()
	root = t.TempDir()
	devPath := filepath.Join(root, "devices", "pci0000:00", "0000:00:02.0")
	driverPath := filepath.Join(root, "bus", "pci", "drivers", "i915")
	drmPath = filepath.Join(root, "class", "drm", "card0")
	for _, d := range []string{devPath, driverPath, drmPath} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
	}
	for name, content := range map[string]string{"vendor": "0x8086\n", "device": "0x9a49\n"} {
		if err := os.WriteFile(filepath.Join(devPath, name), []byte(content), 0o644); err != nil {
			t.Fatalf("write: %v", err)
		}
	}
	if err := os.Symlink(driverPath, filepath.Join(devPath, "driver")); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	if err := os.Symlink(devPath, filepath.Join(drmPath, "device")); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	return root, drmPath
}

func freeAddr(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := l.Addr().String()
	_ = l.Close()
	return addr
}

func startExporter(t *testing.T, args ...string) (*exec.Cmd, <-chan error) {
	t.Helper()
	cmd := exec.Command(os.Args[0], append([]string{"-test.run=^TestHelperMain$", "--"}, args...)...)
	cmd.Env = append(os.Environ(), helperEnv+"=1")
	if err := cmd.Start(); err != nil {
		t.Fatalf("start exporter: %v", err)
	}
	exited := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
		close(exited)
	}()
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		<-exited
	})
	return cmd, done
}

func get(t *testing.T, url string) (int, string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		resp, err := http.Get(url)
		if err == nil {
			b, _ := io.ReadAll(resp.Body)
			_ = resp.Body.Close()
			return resp.StatusCode, string(b)
		}
		if time.Now().After(deadline) {
			t.Fatalf("GET %s: %v", url, err)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func TestMainServesMetricsHealthAndShutsDown(t *testing.T) {
	sysfs, drmPath := fakeSysfs(t)
	addr := freeAddr(t)
	cmd, done := startExporter(t,
		"-web.listen-address="+addr,
		"-web.telemetry-path=/custom-metrics",
		"-path.sysfs="+sysfs,
		"-path.procfs="+t.TempDir(),
		"-collector.pmu=false",
		"-collector.intel-gpu-top=false",
		"-log.level=error",
	)
	base := "http://" + addr

	code, body := get(t, base+"/health")
	if code != http.StatusOK || body != "up" {
		t.Errorf("/health = %d %q, want 200 %q", code, body, "up")
	}

	code, body = get(t, base+"/")
	if code != http.StatusOK || !strings.Contains(body, `href="/custom-metrics"`) {
		t.Errorf("/ = %d %q, want a link to the metrics path", code, body)
	}

	code, body = get(t, base+"/custom-metrics")
	if code != http.StatusOK {
		t.Fatalf("/custom-metrics status = %d", code)
	}
	for _, want := range []string{
		`intel_gpu_info{`,
		`card="card0"`,
		`intel_gpu_scrape_success{source="info"} 1`,
		`intel_gpu_scrape_success{source="fdinfo"} 1`,
		`go_goroutines `,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("metrics missing %q", want)
		}
	}
	for _, absent := range []string{`source="pmu"`, `source="intel_gpu_top"`} {
		if strings.Contains(body, absent) {
			t.Errorf("metrics contain %q although that collector was disabled", absent)
		}
	}

	if err := os.RemoveAll(drmPath); err != nil {
		t.Fatalf("remove drm path: %v", err)
	}
	code, body = get(t, base+"/health")
	if code != http.StatusServiceUnavailable || body != "degraded" {
		t.Errorf("/health after the GPU vanished = %d %q, want 503 %q", code, body, "degraded")
	}

	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatalf("signal: %v", err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("exporter exited with %v after SIGTERM, want a clean exit", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("exporter did not shut down after SIGTERM")
	}
}

func TestMainExitsWhenNoGPUFound(t *testing.T) {
	_, done := startExporter(t, "-path.sysfs="+t.TempDir(), "-log.level=error")
	select {
	case err := <-done:
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) || exitErr.ExitCode() != 1 {
			t.Errorf("exit = %v, want exit status 1", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("exporter kept running without any GPU")
	}
}
