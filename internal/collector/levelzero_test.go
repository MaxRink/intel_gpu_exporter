package collector

import (
	"context"
	"log/slog"
	"testing"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/xsaveopt/intel_gpu_exporter/internal/levelzero"
)

func TestSubdevLabel(t *testing.T) {
	cases := []struct {
		on   bool
		id   uint32
		want string
	}{
		{false, 0, "root"},
		{false, 3, "root"},
		{true, 0, "0"},
		{true, 1, "1"},
		{true, 4294967295, "4294967295"},
	}
	for _, tc := range cases {
		if got := subdevLabel(tc.on, tc.id); got != tc.want {
			t.Errorf("subdevLabel(%v, %d) = %q, want %q", tc.on, tc.id, got, tc.want)
		}
	}
}

func TestLevelZeroName(t *testing.T) {
	if got := NewLevelZero(slog.New(slog.DiscardHandler)).Name(); got != "levelzero" {
		t.Errorf("Name = %q", got)
	}
}

func TestLevelZeroUpdateWithoutClient(t *testing.T) {
	c := NewLevelZero(slog.New(slog.DiscardHandler))
	ch := make(chan prometheus.Metric, 1)
	if err := c.Update(context.Background(), ch); err == nil {
		t.Error("expected an error without an initialised client")
	}
	if len(ch) != 0 {
		t.Errorf("got %d metrics, want 0", len(ch))
	}
}

func TestLevelZeroUpdateNoDevices(t *testing.T) {
	c := NewLevelZero(slog.New(slog.DiscardHandler))
	c.client = &levelzero.Client{}
	if got := samples(t, c); len(got) != 0 {
		t.Errorf("got %v, want no samples", got)
	}
}

func TestLevelZeroAvailableWithoutLoader(t *testing.T) {
	if levelzero.Available() {
		t.Skip("level zero loader is installed on this host")
	}
	c := NewLevelZero(slog.New(slog.DiscardHandler))
	if c.Available(nil) {
		t.Error("Available should be false without the level zero loader")
	}
	if c.client != nil {
		t.Error("Available should not keep a client when the loader is missing")
	}
}
