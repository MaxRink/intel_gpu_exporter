package discovery

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/xsaveopt/intel_gpu_exporter/internal/sysutil"
)

const intelVendor = "0x8086"

type Driver string

const (
	DriverI915    Driver = "i915"
	DriverXe      Driver = "xe"
	DriverUnknown Driver = "unknown"
)

type GPU struct {
	Card       string
	DRMPath    string
	DevicePath string
	PCIAddr    string
	DeviceID   string
	Driver     Driver
	HwmonPaths []string
	Tiles      []Tile
}

type Tile struct {
	Index int
	GTs   []GT
	Path  string
}

type GT struct {
	Index int
	Path  string
}

func Discover(sysfsRoot string) ([]GPU, error) {
	drmRoot := filepath.Join(sysfsRoot, "class", "drm")
	entries, err := os.ReadDir(drmRoot)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", drmRoot, err)
	}

	var gpus []GPU
	for _, e := range entries {
		name := e.Name()
		if !strings.HasPrefix(name, "card") || strings.ContainsRune(name, '-') {
			continue
		}
		drmPath := filepath.Join(drmRoot, name)
		devPath, err := filepath.EvalSymlinks(filepath.Join(drmPath, "device"))
		if err != nil {
			continue
		}
		vendor, _ := sysutil.ReadString(filepath.Join(devPath, "vendor"))
		if vendor != intelVendor {
			continue
		}
		deviceID, _ := sysutil.ReadString(filepath.Join(devPath, "device"))
		driver := detectDriver(devPath)
		gpu := GPU{
			Card:       name,
			DRMPath:    drmPath,
			DevicePath: devPath,
			PCIAddr:    filepath.Base(devPath),
			DeviceID:   deviceID,
			Driver:     driver,
		}
		gpu.HwmonPaths = findHwmon(devPath)
		gpu.Tiles = discoverTiles(driver, devPath)
		gpus = append(gpus, gpu)
	}
	sort.Slice(gpus, func(i, j int) bool { return gpus[i].Card < gpus[j].Card })
	if len(gpus) == 0 {
		return nil, errors.New("no Intel GPU found in sysfs")
	}
	return gpus, nil
}

func detectDriver(devPath string) Driver {
	link, err := os.Readlink(filepath.Join(devPath, "driver"))
	if err != nil {
		return DriverUnknown
	}
	switch filepath.Base(link) {
	case "i915":
		return DriverI915
	case "xe":
		return DriverXe
	default:
		return DriverUnknown
	}
}

func findHwmon(devPath string) []string {
	matches, _ := filepath.Glob(filepath.Join(devPath, "hwmon", "hwmon*"))
	return matches
}

func discoverTiles(driver Driver, devPath string) []Tile {
	switch driver {
	case DriverXe:
		var tiles []Tile
		tileDirs, _ := filepath.Glob(filepath.Join(devPath, "tile*"))
		sort.Slice(tileDirs, func(i, j int) bool {
			a, errA := strconv.Atoi(strings.TrimPrefix(filepath.Base(tileDirs[i]), "tile"))
			b, errB := strconv.Atoi(strings.TrimPrefix(filepath.Base(tileDirs[j]), "tile"))
			if errA != nil || errB != nil {
				return tileDirs[i] < tileDirs[j]
			}
			return a < b
		})
		for ti, td := range tileDirs {
			tile := Tile{Index: ti, Path: td}
			gtDirs, _ := filepath.Glob(filepath.Join(td, "gt*"))
			sort.Strings(gtDirs)
			for gi, gd := range gtDirs {
				tile.GTs = append(tile.GTs, GT{Index: gi, Path: gd})
			}
			tiles = append(tiles, tile)
		}
		return tiles
	case DriverI915:

		return []Tile{{Index: 0, GTs: []GT{{Index: 0, Path: devPath}}}}
	default:
		return []Tile{{Index: 0, GTs: []GT{{Index: 0, Path: devPath}}}}
	}
}

func I915GTs(g GPU) []GT {
	if g.Driver != DriverI915 {
		return nil
	}
	matches, _ := filepath.Glob(filepath.Join(g.DRMPath, "gt", "gt*"))
	sort.Strings(matches)
	gts := make([]GT, 0, len(matches))
	for i, p := range matches {
		gts = append(gts, GT{Index: i, Path: p})
	}
	return gts
}
