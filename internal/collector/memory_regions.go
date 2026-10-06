package collector

import (
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// i915 memory region as reported by DRM_IOCTL_I915_QUERY / DRM_I915_QUERY_MEMORY_REGIONS.
type memRegion struct {
	Class, Instance uint16
	Probed          uint64 // total bytes
	Unallocated     uint64 // free bytes; equals Probed unless the caller has CAP_PERFMON
}

func (r memRegion) name() string {
	switch r.Class {
	case 0:
		return fmt.Sprintf("system%d", r.Instance)
	case 1:
		return fmt.Sprintf("local%d", r.Instance)
	}
	return fmt.Sprintf("class%d_%d", r.Class, r.Instance)
}

const (
	drmI915QueryMemRegions   = 4
	i915QueryHeaderSize      = 16 // num_regions + rsvd[3]
	i915MemoryRegionInfoSize = 88 // class, instance, rsvd0, probed, unallocated, rsvd1[8]
)

// parseI915MemRegions decodes a drm_i915_query_memory_regions buffer (little endian).
func parseI915MemRegions(b []byte) ([]memRegion, error) {
	if len(b) < i915QueryHeaderSize {
		return nil, errors.New("short memory regions buffer")
	}
	n := int(binary.LittleEndian.Uint32(b[0:4]))
	if len(b) < i915QueryHeaderSize+n*i915MemoryRegionInfoSize {
		return nil, fmt.Errorf("memory regions buffer too short for %d regions", n)
	}
	out := make([]memRegion, 0, n)
	for i := 0; i < n; i++ {
		r := b[i915QueryHeaderSize+i*i915MemoryRegionInfoSize:]
		out = append(out, memRegion{
			Class:       binary.LittleEndian.Uint16(r[0:2]),
			Instance:    binary.LittleEndian.Uint16(r[2:4]),
			Probed:      binary.LittleEndian.Uint64(r[8:16]),
			Unallocated: binary.LittleEndian.Uint64(r[16:24]),
		})
	}
	return out, nil
}

// renderNode returns /dev/dri/renderD* for a PCI device directory.
func renderNode(devRoot, devicePath string) (string, error) {
	m, _ := filepath.Glob(filepath.Join(devicePath, "drm", "renderD*"))
	if len(m) == 0 {
		return "", os.ErrNotExist
	}
	return filepath.Join(devRoot, "dri", filepath.Base(m[0])), nil
}
