package collector

import (
	"encoding/binary"
	"testing"

	"github.com/xsaveopt/intel_gpu_exporter/internal/discovery"
)

func TestMemoryUpdateDiscreteLmem(t *testing.T) {
	g := i915GPU(t, map[string]string{"lmem_total_bytes": "17179869184\n"}, nil)
	assertSamples(t, NewMemory([]discovery.GPU{g}), []string{
		`intel_gpu_memory_lmem_total_bytes{` + i915Labels + `} 1.7179869184e+10`,
	})
}

func TestMemoryUpdateXeTiles(t *testing.T) {
	g := xeGPU(t, 2, 1, nil, map[string]string{
		"tile0/physical_vram_size_bytes": "8589934592\n",
		"tile1/physical_vram_size_bytes": "8589934592\n",
	})
	assertSamples(t, NewMemory([]discovery.GPU{g}), []string{
		`intel_gpu_memory_vram_total_bytes{` + xeLabels + `,tile="0"} 8.589934592e+09`,
		`intel_gpu_memory_vram_total_bytes{` + xeLabels + `,tile="1"} 8.589934592e+09`,
	})
}

func TestMemoryUpdateSkipsSyntheticTile(t *testing.T) {
	g := i915GPU(t, nil, map[string]string{"physical_vram_size_bytes": "1234\n"})
	if got := samples(t, NewMemory([]discovery.GPU{g})); len(got) != 0 {
		t.Errorf("got %v, want no samples for a tile aliased to the device path", got)
	}
}

func TestMemoryUpdateIntegratedGPU(t *testing.T) {
	g := i915GPU(t, nil, nil)
	if got := samples(t, NewMemory([]discovery.GPU{g})); len(got) != 0 {
		t.Errorf("got %v, want no samples on an integrated GPU", got)
	}
}

func TestMemoryAvailable(t *testing.T) {
	c := NewMemory(nil)
	if c.Name() != "memory" {
		t.Errorf("Name = %q", c.Name())
	}
	if c.Available(nil) {
		t.Error("Available should be false without GPUs")
	}
	if !c.Available([]discovery.GPU{{Card: "card0"}}) {
		t.Error("Available should be true with a GPU present")
	}
}

func TestParseI915MemRegions(t *testing.T) {
	b := make([]byte, i915QueryHeaderSize+2*i915MemoryRegionInfoSize)
	binary.LittleEndian.PutUint32(b[0:4], 2)
	r0 := b[i915QueryHeaderSize:]
	binary.LittleEndian.PutUint64(r0[8:16], 64<<30)
	binary.LittleEndian.PutUint64(r0[16:24], 32<<30)
	r1 := b[i915QueryHeaderSize+i915MemoryRegionInfoSize:]
	binary.LittleEndian.PutUint16(r1[0:2], 1)
	binary.LittleEndian.PutUint64(r1[8:16], 6<<30)
	binary.LittleEndian.PutUint64(r1[16:24], 5<<30)
	got, err := parseI915MemRegions(b)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].name() != "system0" || got[1].name() != "local0" ||
		got[1].Probed != 6<<30 || got[1].Unallocated != 5<<30 {
		t.Errorf("got %+v", got)
	}
	if _, err := parseI915MemRegions(b[:20]); err == nil {
		t.Error("truncated buffer should fail")
	}
}

func TestMemoryUpdateI915Regions(t *testing.T) {
	g := i915GPU(t, nil, map[string]string{"drm/renderD128/dev": "226:128\n"})
	c := NewMemory([]discovery.GPU{g})
	c.DevRoot = "/fake"
	c.queryRegions = func(node string) ([]memRegion, error) {
		if node != "/fake/dri/renderD128" {
			t.Errorf("node = %q", node)
		}
		return []memRegion{{Class: 1, Probed: 6 << 30, Unallocated: 4 << 30}}, nil
	}
	assertSamples(t, c, []string{
		`intel_gpu_memory_region_free_bytes{` + i915Labels + `,region="local0"} 4.294967296e+09`,
		`intel_gpu_memory_region_total_bytes{` + i915Labels + `,region="local0"} 6.442450944e+09`,
	})
}
