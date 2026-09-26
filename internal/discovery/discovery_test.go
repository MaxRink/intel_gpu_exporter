package discovery

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

type cardSpec struct {
	name     string
	pciAddr  string
	vendor   string
	deviceID string
	driver   string
	hwmons   []string
	tiles    int
	gtsPer   int
	i915GTs  int
}

func mkdir(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", path, err)
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	mkdir(t, filepath.Dir(path))
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func symlink(t *testing.T, target, link string) {
	t.Helper()
	mkdir(t, filepath.Dir(link))
	if err := os.Symlink(target, link); err != nil {
		t.Fatalf("symlink %s -> %s: %v", link, target, err)
	}
}

func buildSysfs(t *testing.T, cards ...cardSpec) string {
	t.Helper()
	root := t.TempDir()
	drmRoot := filepath.Join(root, "class", "drm")
	mkdir(t, drmRoot)

	for _, c := range cards {
		devPath := filepath.Join(root, "devices", "pci0000:00", c.pciAddr)
		mkdir(t, devPath)
		writeFile(t, filepath.Join(devPath, "vendor"), c.vendor+"\n")
		writeFile(t, filepath.Join(devPath, "device"), c.deviceID+"\n")

		if c.driver != "" {
			driverPath := filepath.Join(root, "bus", "pci", "drivers", c.driver)
			mkdir(t, driverPath)
			rel, err := filepath.Rel(devPath, driverPath)
			if err != nil {
				t.Fatalf("rel: %v", err)
			}
			symlink(t, rel, filepath.Join(devPath, "driver"))
		}

		for _, h := range c.hwmons {
			hwmonPath := filepath.Join(devPath, "hwmon", h)
			mkdir(t, hwmonPath)
			writeFile(t, filepath.Join(hwmonPath, "name"), "i915\n")
		}

		for ti := range c.tiles {
			tilePath := filepath.Join(devPath, "tile"+strconv.Itoa(ti))
			mkdir(t, tilePath)
			for gi := range c.gtsPer {
				mkdir(t, filepath.Join(tilePath, "gt"+strconv.Itoa(gi)))
			}
		}

		drmPath := filepath.Join(drmRoot, c.name)
		mkdir(t, drmPath)
		for gi := range c.i915GTs {
			mkdir(t, filepath.Join(drmPath, "gt", "gt"+strconv.Itoa(gi)))
		}
		rel, err := filepath.Rel(drmPath, devPath)
		if err != nil {
			t.Fatalf("rel: %v", err)
		}
		symlink(t, rel, filepath.Join(drmPath, "device"))

		mkdir(t, filepath.Join(drmRoot, c.name+"-HDMI-A-1"))
	}
	return root
}

func TestDiscoverI915(t *testing.T) {
	root := buildSysfs(t, cardSpec{
		name: "card0", pciAddr: "0000:00:02.0", vendor: "0x8086",
		deviceID: "0x9a49", driver: "i915", hwmons: []string{"hwmon3"}, i915GTs: 2,
	})
	gpus, err := Discover(root)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if len(gpus) != 1 {
		t.Fatalf("got %d GPUs, want 1", len(gpus))
	}
	g := gpus[0]
	if g.Card != "card0" {
		t.Errorf("Card = %q", g.Card)
	}
	if g.PCIAddr != "0000:00:02.0" {
		t.Errorf("PCIAddr = %q", g.PCIAddr)
	}
	if g.DeviceID != "0x9a49" {
		t.Errorf("DeviceID = %q", g.DeviceID)
	}
	if g.Driver != DriverI915 {
		t.Errorf("Driver = %q, want %q", g.Driver, DriverI915)
	}
	if g.DRMPath != filepath.Join(root, "class", "drm", "card0") {
		t.Errorf("DRMPath = %q", g.DRMPath)
	}
	if len(g.HwmonPaths) != 1 {
		t.Errorf("HwmonPaths = %v, want one entry", g.HwmonPaths)
	}
	if len(g.Tiles) != 1 || len(g.Tiles[0].GTs) != 1 {
		t.Fatalf("Tiles = %+v, want a single synthetic tile with one GT", g.Tiles)
	}
	if g.Tiles[0].GTs[0].Path != g.DevicePath {
		t.Errorf("synthetic GT path = %q, want the device path %q", g.Tiles[0].GTs[0].Path, g.DevicePath)
	}
}

func TestDiscoverXeTilesAndGTs(t *testing.T) {
	root := buildSysfs(t, cardSpec{
		name: "card1", pciAddr: "0000:03:00.0", vendor: "0x8086",
		deviceID: "0x56a0", driver: "xe", hwmons: []string{"hwmon5", "hwmon6"},
		tiles: 2, gtsPer: 2,
	})
	gpus, err := Discover(root)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	g := gpus[0]
	if g.Driver != DriverXe {
		t.Fatalf("Driver = %q, want %q", g.Driver, DriverXe)
	}
	if len(g.HwmonPaths) != 2 {
		t.Errorf("HwmonPaths = %v, want two entries", g.HwmonPaths)
	}
	if len(g.Tiles) != 2 {
		t.Fatalf("got %d tiles, want 2", len(g.Tiles))
	}
	for ti, tile := range g.Tiles {
		if tile.Index != ti {
			t.Errorf("tile %d has Index %d", ti, tile.Index)
		}
		if filepath.Base(tile.Path) != "tile"+strconv.Itoa(ti) {
			t.Errorf("tile %d path = %q", ti, tile.Path)
		}
		if len(tile.GTs) != 2 {
			t.Fatalf("tile %d has %d GTs, want 2", ti, len(tile.GTs))
		}
		for gi, gt := range tile.GTs {
			if gt.Index != gi {
				t.Errorf("tile %d gt %d has Index %d", ti, gi, gt.Index)
			}
			if filepath.Base(gt.Path) != "gt"+strconv.Itoa(gi) {
				t.Errorf("tile %d gt %d path = %q", ti, gi, gt.Path)
			}
		}
	}
}

func TestDiscoverXeTilesSortNumerically(t *testing.T) {
	root := buildSysfs(t, cardSpec{
		name: "card1", pciAddr: "0000:03:00.0", vendor: "0x8086",
		deviceID: "0x56a0", driver: "xe", tiles: 11, gtsPer: 1,
	})
	gpus, err := Discover(root)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	tiles := gpus[0].Tiles
	if len(tiles) != 11 {
		t.Fatalf("got %d tiles, want 11", len(tiles))
	}
	for ti, tile := range tiles {
		if filepath.Base(tile.Path) != "tile"+strconv.Itoa(ti) {
			t.Errorf("tile Index %d points at %q, want tile%d", tile.Index, filepath.Base(tile.Path), ti)
		}
	}
}

func TestDiscoverSkipsNonIntelAndConnectors(t *testing.T) {
	root := buildSysfs(t,
		cardSpec{name: "card0", pciAddr: "0000:01:00.0", vendor: "0x10de", deviceID: "0x2484", driver: "nvidia"},
		cardSpec{name: "card1", pciAddr: "0000:00:02.0", vendor: "0x8086", deviceID: "0x9a49", driver: "i915"},
	)
	gpus, err := Discover(root)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if len(gpus) != 1 || gpus[0].Card != "card1" {
		t.Fatalf("got %+v, want only card1", gpus)
	}
}

func TestDiscoverSortsByCard(t *testing.T) {
	root := buildSysfs(t,
		cardSpec{name: "card1", pciAddr: "0000:03:00.0", vendor: "0x8086", deviceID: "0x56a0", driver: "xe"},
		cardSpec{name: "card0", pciAddr: "0000:00:02.0", vendor: "0x8086", deviceID: "0x9a49", driver: "i915"},
	)
	gpus, err := Discover(root)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if len(gpus) != 2 {
		t.Fatalf("got %d GPUs, want 2", len(gpus))
	}
	if gpus[0].Card != "card0" || gpus[1].Card != "card1" {
		t.Errorf("order = %q, %q", gpus[0].Card, gpus[1].Card)
	}
}

func TestDiscoverUnknownDriver(t *testing.T) {
	root := buildSysfs(t, cardSpec{
		name: "card0", pciAddr: "0000:00:02.0", vendor: "0x8086", deviceID: "0x9a49",
	})
	gpus, err := Discover(root)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if gpus[0].Driver != DriverUnknown {
		t.Errorf("Driver = %q, want %q", gpus[0].Driver, DriverUnknown)
	}
	if len(gpus[0].Tiles) != 1 || len(gpus[0].Tiles[0].GTs) != 1 {
		t.Errorf("Tiles = %+v, want a single synthetic tile", gpus[0].Tiles)
	}
}

func TestDiscoverUnboundDriverName(t *testing.T) {
	root := buildSysfs(t, cardSpec{
		name: "card0", pciAddr: "0000:00:02.0", vendor: "0x8086", deviceID: "0x9a49", driver: "vfio-pci",
	})
	gpus, err := Discover(root)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if gpus[0].Driver != DriverUnknown {
		t.Errorf("Driver = %q, want %q", gpus[0].Driver, DriverUnknown)
	}
}

func TestDiscoverNoIntelGPU(t *testing.T) {
	root := buildSysfs(t, cardSpec{
		name: "card0", pciAddr: "0000:01:00.0", vendor: "0x1002", deviceID: "0x73ff", driver: "amdgpu",
	})
	if _, err := Discover(root); err == nil {
		t.Fatal("expected an error when no Intel GPU is present")
	}
}

func TestDiscoverMissingDRMRoot(t *testing.T) {
	_, err := Discover(filepath.Join(t.TempDir(), "absent"))
	if err == nil {
		t.Fatal("expected an error for a missing drm root")
	}
}

func TestDiscoverBrokenDeviceSymlink(t *testing.T) {
	root := t.TempDir()
	drmPath := filepath.Join(root, "class", "drm", "card0")
	mkdir(t, drmPath)
	symlink(t, "../../../devices/gone", filepath.Join(drmPath, "device"))
	if _, err := Discover(root); err == nil {
		t.Fatal("expected an error when the only card has a dangling device link")
	}
}

func TestI915GTs(t *testing.T) {
	root := buildSysfs(t, cardSpec{
		name: "card0", pciAddr: "0000:00:02.0", vendor: "0x8086",
		deviceID: "0x9a49", driver: "i915", i915GTs: 3,
	})
	gpus, err := Discover(root)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	gts := I915GTs(gpus[0])
	if len(gts) != 3 {
		t.Fatalf("got %d GTs, want 3", len(gts))
	}
	for i, gt := range gts {
		if gt.Index != i {
			t.Errorf("GT %d has Index %d", i, gt.Index)
		}
		if filepath.Base(gt.Path) != "gt"+strconv.Itoa(i) {
			t.Errorf("GT %d path = %q", i, gt.Path)
		}
	}
}

func TestI915GTsSkipsOtherDrivers(t *testing.T) {
	root := buildSysfs(t, cardSpec{
		name: "card0", pciAddr: "0000:03:00.0", vendor: "0x8086",
		deviceID: "0x56a0", driver: "xe", i915GTs: 2,
	})
	gpus, err := Discover(root)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if gts := I915GTs(gpus[0]); gts != nil {
		t.Errorf("got %+v, want nil for an xe device", gts)
	}
}

func TestI915GTsWithoutGTDirectory(t *testing.T) {
	root := buildSysfs(t, cardSpec{
		name: "card0", pciAddr: "0000:00:02.0", vendor: "0x8086",
		deviceID: "0x9a49", driver: "i915",
	})
	gpus, err := Discover(root)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if gts := I915GTs(gpus[0]); len(gts) != 0 {
		t.Errorf("got %+v, want none", gts)
	}
}
