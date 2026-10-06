package collector

import (
	"fmt"
	"runtime"
	"unsafe"

	"golang.org/x/sys/unix"
)

const drmIoctlI915Query = 0xC0106479 // _IOWR('d', 0x40+0x39, struct drm_i915_query)

type i915QueryItem struct {
	QueryID uint64
	Length  int32
	Flags   uint32
	DataPtr uint64
}

type i915Query struct {
	NumItems uint32
	Flags    uint32
	ItemsPtr uint64
}

func i915QueryIoctl(fd int, item *i915QueryItem) error {
	q := i915Query{NumItems: 1, ItemsPtr: uint64(uintptr(unsafe.Pointer(item)))}
	_, _, errno := unix.Syscall(unix.SYS_IOCTL, uintptr(fd), drmIoctlI915Query, uintptr(unsafe.Pointer(&q)))
	runtime.KeepAlive(item)
	if errno != 0 {
		return errno
	}
	if item.Length < 0 {
		return fmt.Errorf("i915 query failed: %d", item.Length)
	}
	return nil
}

// queryI915MemRegions opens a DRM node and asks i915 for its memory regions.
// Render nodes allow this ioctl; no DRM master is needed.
func queryI915MemRegions(node string) ([]memRegion, error) {
	fd, err := unix.Open(node, unix.O_RDWR|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	defer func() { _ = unix.Close(fd) }()
	item := i915QueryItem{QueryID: drmI915QueryMemRegions}
	if err := i915QueryIoctl(fd, &item); err != nil {
		return nil, err
	}
	buf := make([]byte, item.Length)
	item.DataPtr = uint64(uintptr(unsafe.Pointer(&buf[0])))
	err = i915QueryIoctl(fd, &item)
	runtime.KeepAlive(buf)
	if err != nil {
		return nil, err
	}
	return parseI915MemRegions(buf)
}
