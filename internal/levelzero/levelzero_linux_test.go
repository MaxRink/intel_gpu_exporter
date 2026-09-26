//go:build linux

package levelzero

import (
	"errors"
	"reflect"
	"sync"
	"testing"
	"unsafe"
)

const zeErrorUnknown uint32 = 0x7ffffffe

func withFns(t *testing.T, f fnTable) {
	t.Helper()
	saved := fns
	fns = f
	t.Cleanup(func() { fns = saved })
}

func handles(n int) []unsafe.Pointer {
	out := make([]unsafe.Pointer, n)
	for i := range out {
		v := new(int)
		*v = i
		out[i] = unsafe.Pointer(v)
	}
	return out
}

func idx(h unsafe.Pointer) int { return *(*int)(h) }

func enumOf(hs []unsafe.Pointer) func(unsafe.Pointer, *uint32, *unsafe.Pointer) uint32 {
	return func(_ unsafe.Pointer, count *uint32, out *unsafe.Pointer) uint32 {
		if out == nil {
			*count = uint32(len(hs))
			return zeResultSuccess
		}
		copy(unsafe.Slice(out, *count), hs)
		return zeResultSuccess
	}
}

func enumFail(unsafe.Pointer, *uint32, *unsafe.Pointer) uint32 { return zeErrorUnknown }

func TestList(t *testing.T) {
	t.Run("count error", func(t *testing.T) {
		_, err := list(func(*uint32, *int) uint32 { return zeErrorUnknown })
		if err == nil {
			t.Error("expected an error")
		}
	})
	t.Run("empty", func(t *testing.T) {
		calls := 0
		got, err := list(func(c *uint32, _ *int) uint32 {
			calls++
			*c = 0
			return zeResultSuccess
		})
		if err != nil || got != nil || calls != 1 {
			t.Errorf("got (%v, %v) after %d calls", got, err, calls)
		}
	})
	t.Run("fill error", func(t *testing.T) {
		_, err := list(func(c *uint32, o *int) uint32 {
			if o == nil {
				*c = 2
				return zeResultSuccess
			}
			return zeErrorUnknown
		})
		if err == nil {
			t.Error("expected an error")
		}
	})
	t.Run("fill shrinks count", func(t *testing.T) {
		got, err := list(func(c *uint32, o *int) uint32 {
			if o == nil {
				*c = 3
				return zeResultSuccess
			}
			s := unsafe.Slice(o, *c)
			s[0], s[1] = 10, 20
			*c = 2
			return zeResultSuccess
		})
		if err != nil {
			t.Fatalf("list: %v", err)
		}
		if len(got) != 2 || got[0] != 10 || got[1] != 20 {
			t.Errorf("got %v", got)
		}
	})
}

func TestRasCategoryName(t *testing.T) {
	cases := map[int]string{
		-1: "unknown",
		0:  "reset",
		1:  "programming",
		4:  "non_compute",
		6:  "display",
		7:  "unknown",
	}
	for i, want := range cases {
		if got := RasCategoryName(i); got != want {
			t.Errorf("RasCategoryName(%d) = %q, want %q", i, got, want)
		}
	}
}

func TestRasTypeName(t *testing.T) {
	cases := map[uint32]string{0: "correctable", 1: "uncorrectable", 2: "unknown"}
	for in, want := range cases {
		if got := rasTypeName(in); got != want {
			t.Errorf("rasTypeName(%d) = %q, want %q", in, got, want)
		}
	}
}

func TestEngineTypeName(t *testing.T) {
	cases := map[uint32]string{
		0:          "all",
		1:          "compute_all",
		2:          "media_all",
		3:          "copy_all",
		4:          "compute_single",
		5:          "render_single",
		6:          "media_decode_single",
		7:          "media_encode_single",
		8:          "copy_single",
		9:          "media_enhancement_single",
		10:         "3d_single",
		11:         "3d_render_compute_all",
		12:         "render_all",
		13:         "3d_all",
		14:         "media_codec_single",
		15:         "unknown",
		0x7fffffff: "unknown",
	}
	for in, want := range cases {
		if got := engineTypeName(in); got != want {
			t.Errorf("engineTypeName(%d) = %q, want %q", in, got, want)
		}
	}
}

func TestRasGetStateClearIsZeBool(t *testing.T) {
	f, ok := reflect.TypeOf(fnTable{}).FieldByName("zesRasGetState")
	if !ok {
		t.Fatal("fnTable has no zesRasGetState field")
	}
	if got := f.Type.NumIn(); got != 3 {
		t.Fatalf("zesRasGetState takes %d args, want 3", got)
	}
	arg := f.Type.In(1)
	if arg.Kind() != reflect.Uint8 || arg.Size() != 1 {
		t.Errorf("zesRasGetState clear is %s (%d bytes), want ze_bool_t uint8 (1 byte)", arg, arg.Size())
	}
}

type layoutCase struct {
	name      string
	got, want uintptr
}

func checkLayout(t *testing.T, cases []layoutCase) {
	t.Helper()
	for _, tc := range cases {
		if tc.got != tc.want {
			t.Errorf("%s = %d, want %d", tc.name, tc.got, tc.want)
		}
	}
}

func TestStructLayout(t *testing.T) {
	var (
		pa  pciAddress
		ps  pciSpeed
		pci pciProperties
		pec powerEnergyCounter
		pp  powerProperties
		tp  tempProperties
		fs  freqState
		ftt freqThrottleTime
		fp  freqProperties
		es  engineStats
		ep  engineProperties
		rs  rasState
		rp  rasProperties
		ms  memState
		mb  memBandwidth
		mp  memProperties
	)
	checkLayout(t, []layoutCase{
		{"sizeof zes_pci_address_t", unsafe.Sizeof(pa), 16},
		{"zes_pci_address_t.domain", unsafe.Offsetof(pa.Domain), 0},
		{"zes_pci_address_t.bus", unsafe.Offsetof(pa.Bus), 4},
		{"zes_pci_address_t.device", unsafe.Offsetof(pa.Device), 8},
		{"zes_pci_address_t.function", unsafe.Offsetof(pa.Function), 12},

		{"sizeof zes_pci_speed_t", unsafe.Sizeof(ps), 16},
		{"zes_pci_speed_t.gen", unsafe.Offsetof(ps.Gen), 0},
		{"zes_pci_speed_t.width", unsafe.Offsetof(ps.Width), 4},
		{"zes_pci_speed_t.maxBandwidth", unsafe.Offsetof(ps.MaxBandwidth), 8},

		{"sizeof zes_pci_properties_t", unsafe.Sizeof(pci), 56},
		{"zes_pci_properties_t.stype", unsafe.Offsetof(pci.Stype), 0},
		{"zes_pci_properties_t.pNext", unsafe.Offsetof(pci.PNext), 8},
		{"zes_pci_properties_t.address", unsafe.Offsetof(pci.Address), 16},
		{"zes_pci_properties_t.maxSpeed", unsafe.Offsetof(pci.MaxSpeed), 32},
		{"zes_pci_properties_t.haveBandwidthCounters", unsafe.Offsetof(pci.HaveBandwidthCounters), 48},
		{"zes_pci_properties_t.havePacketCounters", unsafe.Offsetof(pci.HavePacketCounters), 49},
		{"zes_pci_properties_t.haveReplayCounters", unsafe.Offsetof(pci.HaveReplayCounters), 50},

		{"sizeof zes_power_energy_counter_t", unsafe.Sizeof(pec), 16},
		{"zes_power_energy_counter_t.energy", unsafe.Offsetof(pec.Energy), 0},
		{"zes_power_energy_counter_t.timestamp", unsafe.Offsetof(pec.Timestamp), 8},

		{"sizeof zes_power_properties_t", unsafe.Sizeof(pp), 40},
		{"zes_power_properties_t.stype", unsafe.Offsetof(pp.Stype), 0},
		{"zes_power_properties_t.pNext", unsafe.Offsetof(pp.PNext), 8},
		{"zes_power_properties_t.onSubdevice", unsafe.Offsetof(pp.OnSubdevice), 16},
		{"zes_power_properties_t.subdeviceId", unsafe.Offsetof(pp.SubdeviceID), 20},
		{"zes_power_properties_t.canControl", unsafe.Offsetof(pp.CanControl), 24},
		{"zes_power_properties_t.isEnergyThresholdSupported", unsafe.Offsetof(pp.IsEnergyThresholdSupported), 25},
		{"zes_power_properties_t.defaultLimit", unsafe.Offsetof(pp.DefaultLimit), 28},
		{"zes_power_properties_t.minLimit", unsafe.Offsetof(pp.MinLimit), 32},
		{"zes_power_properties_t.maxLimit", unsafe.Offsetof(pp.MaxLimit), 36},

		{"sizeof zes_temp_properties_t", unsafe.Sizeof(tp), 48},
		{"zes_temp_properties_t.stype", unsafe.Offsetof(tp.Stype), 0},
		{"zes_temp_properties_t.pNext", unsafe.Offsetof(tp.PNext), 8},
		{"zes_temp_properties_t.type", unsafe.Offsetof(tp.SensorType), 16},
		{"zes_temp_properties_t.onSubdevice", unsafe.Offsetof(tp.OnSubdevice), 20},
		{"zes_temp_properties_t.subdeviceId", unsafe.Offsetof(tp.SubdeviceID), 24},
		{"zes_temp_properties_t.maxTemperature", unsafe.Offsetof(tp.MaxTemperature), 32},
		{"zes_temp_properties_t.isCriticalTempSupported", unsafe.Offsetof(tp.IsCriticalTempSupported), 40},
		{"zes_temp_properties_t.isThreshold1Supported", unsafe.Offsetof(tp.IsThreshold1Supported), 41},
		{"zes_temp_properties_t.isThreshold2Supported", unsafe.Offsetof(tp.IsThreshold2Supported), 42},

		{"sizeof zes_freq_state_t", unsafe.Sizeof(fs), 64},
		{"zes_freq_state_t.stype", unsafe.Offsetof(fs.Stype), 0},
		{"zes_freq_state_t.pNext", unsafe.Offsetof(fs.PNext), 8},
		{"zes_freq_state_t.currentVoltage", unsafe.Offsetof(fs.CurrentVoltage), 16},
		{"zes_freq_state_t.request", unsafe.Offsetof(fs.Request), 24},
		{"zes_freq_state_t.tdp", unsafe.Offsetof(fs.TDP), 32},
		{"zes_freq_state_t.efficient", unsafe.Offsetof(fs.Efficient), 40},
		{"zes_freq_state_t.actual", unsafe.Offsetof(fs.Actual), 48},
		{"zes_freq_state_t.throttleReasons", unsafe.Offsetof(fs.ThrottleReasons), 56},

		{"sizeof zes_freq_throttle_time_t", unsafe.Sizeof(ftt), 16},
		{"zes_freq_throttle_time_t.throttleTime", unsafe.Offsetof(ftt.ThrottleTime), 0},
		{"zes_freq_throttle_time_t.timestamp", unsafe.Offsetof(ftt.Timestamp), 8},

		{"sizeof zes_freq_properties_t", unsafe.Sizeof(fp), 48},
		{"zes_freq_properties_t.stype", unsafe.Offsetof(fp.Stype), 0},
		{"zes_freq_properties_t.pNext", unsafe.Offsetof(fp.PNext), 8},
		{"zes_freq_properties_t.type", unsafe.Offsetof(fp.Type), 16},
		{"zes_freq_properties_t.onSubdevice", unsafe.Offsetof(fp.OnSubdevice), 20},
		{"zes_freq_properties_t.subdeviceId", unsafe.Offsetof(fp.SubdeviceID), 24},
		{"zes_freq_properties_t.canControl", unsafe.Offsetof(fp.CanControl), 28},
		{"zes_freq_properties_t.isThrottleEventSupported", unsafe.Offsetof(fp.IsThrottleEvent), 29},
		{"zes_freq_properties_t.min", unsafe.Offsetof(fp.Min), 32},
		{"zes_freq_properties_t.max", unsafe.Offsetof(fp.Max), 40},

		{"sizeof zes_engine_stats_t", unsafe.Sizeof(es), 16},
		{"zes_engine_stats_t.activeTime", unsafe.Offsetof(es.ActiveTime), 0},
		{"zes_engine_stats_t.timestamp", unsafe.Offsetof(es.Timestamp), 8},

		{"sizeof zes_engine_properties_t", unsafe.Sizeof(ep), 32},
		{"zes_engine_properties_t.stype", unsafe.Offsetof(ep.Stype), 0},
		{"zes_engine_properties_t.pNext", unsafe.Offsetof(ep.PNext), 8},
		{"zes_engine_properties_t.type", unsafe.Offsetof(ep.Type), 16},
		{"zes_engine_properties_t.onSubdevice", unsafe.Offsetof(ep.OnSubdevice), 20},
		{"zes_engine_properties_t.subdeviceId", unsafe.Offsetof(ep.SubdeviceID), 24},

		{"sizeof zes_ras_state_t", unsafe.Sizeof(rs), 72},
		{"zes_ras_state_t.stype", unsafe.Offsetof(rs.Stype), 0},
		{"zes_ras_state_t.pNext", unsafe.Offsetof(rs.PNext), 8},
		{"zes_ras_state_t.category", unsafe.Offsetof(rs.Category), 16},

		{"sizeof zes_ras_properties_t", unsafe.Sizeof(rp), 32},
		{"zes_ras_properties_t.stype", unsafe.Offsetof(rp.Stype), 0},
		{"zes_ras_properties_t.pNext", unsafe.Offsetof(rp.PNext), 8},
		{"zes_ras_properties_t.type", unsafe.Offsetof(rp.Type), 16},
		{"zes_ras_properties_t.onSubdevice", unsafe.Offsetof(rp.OnSubdevice), 20},
		{"zes_ras_properties_t.subdeviceId", unsafe.Offsetof(rp.SubdeviceID), 24},

		{"sizeof zes_mem_state_t", unsafe.Sizeof(ms), 40},
		{"zes_mem_state_t.stype", unsafe.Offsetof(ms.Stype), 0},
		{"zes_mem_state_t.pNext", unsafe.Offsetof(ms.PNext), 8},
		{"zes_mem_state_t.health", unsafe.Offsetof(ms.Health), 16},
		{"zes_mem_state_t.free", unsafe.Offsetof(ms.Free), 24},
		{"zes_mem_state_t.size", unsafe.Offsetof(ms.Size), 32},

		{"sizeof zes_mem_bandwidth_t", unsafe.Sizeof(mb), 32},
		{"zes_mem_bandwidth_t.readCounter", unsafe.Offsetof(mb.ReadCounter), 0},
		{"zes_mem_bandwidth_t.writeCounter", unsafe.Offsetof(mb.WriteCounter), 8},
		{"zes_mem_bandwidth_t.maxBandwidth", unsafe.Offsetof(mb.MaxBandwidth), 16},
		{"zes_mem_bandwidth_t.timestamp", unsafe.Offsetof(mb.Timestamp), 24},

		{"sizeof zes_mem_properties_t", unsafe.Sizeof(mp), 48},
		{"zes_mem_properties_t.stype", unsafe.Offsetof(mp.Stype), 0},
		{"zes_mem_properties_t.pNext", unsafe.Offsetof(mp.PNext), 8},
		{"zes_mem_properties_t.type", unsafe.Offsetof(mp.Type), 16},
		{"zes_mem_properties_t.onSubdevice", unsafe.Offsetof(mp.OnSubdevice), 20},
		{"zes_mem_properties_t.subdeviceId", unsafe.Offsetof(mp.SubdeviceID), 24},
		{"zes_mem_properties_t.location", unsafe.Offsetof(mp.Location), 28},
		{"zes_mem_properties_t.physicalSize", unsafe.Offsetof(mp.PhysicalSize), 32},
		{"zes_mem_properties_t.busWidth", unsafe.Offsetof(mp.BusWidth), 40},
		{"zes_mem_properties_t.numChannels", unsafe.Offsetof(mp.NumChannels), 44},
	})
}

func TestStructureTypes(t *testing.T) {
	cases := map[string][2]uint32{
		"ZES_STRUCTURE_TYPE_PCI_PROPERTIES":    {stypePciProperties, 0x2},
		"ZES_STRUCTURE_TYPE_ENGINE_PROPERTIES": {stypeEngineProperties, 0x5},
		"ZES_STRUCTURE_TYPE_FREQ_PROPERTIES":   {stypeFreqProperties, 0x9},
		"ZES_STRUCTURE_TYPE_MEM_PROPERTIES":    {stypeMemProperties, 0xb},
		"ZES_STRUCTURE_TYPE_POWER_PROPERTIES":  {stypePowerProperties, 0xd},
		"ZES_STRUCTURE_TYPE_RAS_PROPERTIES":    {stypeRasProperties, 0xf},
		"ZES_STRUCTURE_TYPE_TEMP_PROPERTIES":   {stypeTempProperties, 0x14},
		"ZES_STRUCTURE_TYPE_FREQ_STATE":        {stypeFreqState, 0x1b},
		"ZES_STRUCTURE_TYPE_MEM_STATE":         {stypeMemState, 0x1e},
		"ZES_STRUCTURE_TYPE_RAS_STATE":         {stypeRasState, 0x22},
	}
	for name, c := range cases {
		if c[0] != c[1] {
			t.Errorf("%s = %#x, want %#x", name, c[0], c[1])
		}
	}
}

func TestEnumerateDevice(t *testing.T) {
	dev := handles(1)[0]
	withFns(t, fnTable{
		zesDevicePciGetProperties: func(h DeviceHandle, p *pciProperties) uint32 {
			if h != dev || p.Stype != stypePciProperties {
				return zeErrorUnknown
			}
			p.Address = pciAddress{Domain: 0x1, Bus: 0x2a, Device: 0x1f, Function: 0x7}
			return zeResultSuccess
		},
		zesDeviceEnumPowerDomains: enumOf(handles(2)),
		zesPowerGetProperties: func(h PwrHandle, p *powerProperties) uint32 {
			if idx(h) != 0 {
				return zeErrorUnknown
			}
			p.OnSubdevice, p.SubdeviceID, p.MinLimit, p.MaxLimit = 1, 1, 1000, 2000
			return zeResultSuccess
		},
		zesDeviceEnumTemperatureSensors: enumOf(handles(3)),
		zesTemperatureGetProperties: func(h TempHandle, p *tempProperties) uint32 {
			switch idx(h) {
			case 0:
				p.SensorType = 1
			case 1:
				p.SensorType = 99
				p.OnSubdevice, p.SubdeviceID = 1, 2
			default:
				return zeErrorUnknown
			}
			return zeResultSuccess
		},
		zesDeviceEnumFrequencyDomains: enumOf(handles(2)),
		zesFrequencyGetProperties: func(h FreqHandle, p *freqProperties) uint32 {
			if idx(h) != 0 {
				return zeErrorUnknown
			}
			p.Type, p.OnSubdevice, p.SubdeviceID = 1, 1, 3
			return zeResultSuccess
		},
		zesDeviceEnumEngineGroups: enumOf(handles(3)),
		zesEngineGetProperties: func(h EngineHandle, p *engineProperties) uint32 {
			switch idx(h) {
			case 0:
				p.Type = 1
			case 1:
				p.Type = 0
			default:
				return zeErrorUnknown
			}
			return zeResultSuccess
		},
		zesDeviceEnumRasErrorSets: enumOf(handles(3)),
		zesRasGetProperties: func(h RasHandle, p *rasProperties) uint32 {
			switch idx(h) {
			case 0:
				p.Type = rasTypeCorrectable
			case 1:
				p.Type = rasTypeUncorrectable
				p.OnSubdevice, p.SubdeviceID = 1, 1
			default:
				return zeErrorUnknown
			}
			return zeResultSuccess
		},
		zesDeviceEnumMemoryModules: enumOf(handles(3)),
		zesMemoryGetProperties: func(h MemHandle, p *memProperties) uint32 {
			switch idx(h) {
			case 0:
				p.Type, p.PhysicalSize = 17, 16<<30
			case 1:
				p.Type = 99
				p.OnSubdevice, p.SubdeviceID = 1, 1
			default:
				return zeErrorUnknown
			}
			return zeResultSuccess
		},
	})

	d := enumerateDevice(dev)

	if d.Handle != dev || d.PCIBus != "0001:2a:1f.7" {
		t.Errorf("handle/pci = %v/%q", d.Handle, d.PCIBus)
	}

	if len(d.Power) != 2 {
		t.Fatalf("power = %+v", d.Power)
	}
	if p := d.Power[0]; !p.OnSubdevice || p.SubdeviceID != 1 || p.MinLimitMW != 1000 || p.MaxLimitMW != 2000 {
		t.Errorf("power[0] = %+v", p)
	}
	if p := d.Power[1]; p.Handle == nil || p.OnSubdevice || p.MaxLimitMW != 0 {
		t.Errorf("power[1] = %+v", p)
	}

	wantTemp := []Temperature{
		{Sensor: "gpu"},
		{Sensor: "unknown", OnSubdevice: true, SubdeviceID: 2},
		{Sensor: "unknown"},
	}
	if len(d.Temp) != len(wantTemp) {
		t.Fatalf("temp = %+v", d.Temp)
	}
	for i, w := range wantTemp {
		g := d.Temp[i]
		if g.Sensor != w.Sensor || g.OnSubdevice != w.OnSubdevice || g.SubdeviceID != w.SubdeviceID {
			t.Errorf("temp[%d] = %+v, want %+v", i, g, w)
		}
	}

	if len(d.Freq) != 2 {
		t.Fatalf("freq = %+v", d.Freq)
	}
	if f := d.Freq[0]; f.Type != 1 || !f.OnSubdevice || f.SubdeviceID != 3 {
		t.Errorf("freq[0] = %+v", f)
	}
	if f := d.Freq[1]; f.Type != 0 || f.OnSubdevice {
		t.Errorf("freq[1] = %+v", f)
	}

	wantEngine := []string{"compute_all", "all", "unknown"}
	if len(d.Engine) != len(wantEngine) {
		t.Fatalf("engine = %+v", d.Engine)
	}
	for i, w := range wantEngine {
		if d.Engine[i].Type != w {
			t.Errorf("engine[%d].Type = %q, want %q", i, d.Engine[i].Type, w)
		}
	}

	wantRAS := []RAS{
		{ErrorType: "correctable"},
		{ErrorType: "uncorrectable", OnSubdevice: true, SubdeviceID: 1},
		{ErrorType: "unknown"},
	}
	if len(d.RAS) != len(wantRAS) {
		t.Fatalf("ras = %+v", d.RAS)
	}
	for i, w := range wantRAS {
		g := d.RAS[i]
		if g.ErrorType != w.ErrorType || g.OnSubdevice != w.OnSubdevice || g.SubdeviceID != w.SubdeviceID {
			t.Errorf("ras[%d] = %+v, want %+v", i, g, w)
		}
	}

	wantMem := []Memory{
		{Type: "gddr6", PhysicalSize: 16 << 30},
		{Type: "unknown", OnSubdevice: true, SubdeviceID: 1},
		{Type: "unknown"},
	}
	if len(d.Mem) != len(wantMem) {
		t.Fatalf("mem = %+v", d.Mem)
	}
	for i, w := range wantMem {
		g := d.Mem[i]
		if g.Type != w.Type || g.OnSubdevice != w.OnSubdevice || g.SubdeviceID != w.SubdeviceID || g.PhysicalSize != w.PhysicalSize {
			t.Errorf("mem[%d] = %+v, want %+v", i, g, w)
		}
	}
}

func TestEnumerateDeviceAllFailing(t *testing.T) {
	withFns(t, fnTable{
		zesDevicePciGetProperties:       func(DeviceHandle, *pciProperties) uint32 { return zeErrorUnknown },
		zesDeviceEnumPowerDomains:       enumFail,
		zesDeviceEnumTemperatureSensors: enumFail,
		zesDeviceEnumFrequencyDomains:   enumFail,
		zesDeviceEnumEngineGroups:       enumFail,
		zesDeviceEnumRasErrorSets:       enumFail,
		zesDeviceEnumMemoryModules:      enumFail,
	})
	dev := handles(1)[0]
	d := enumerateDevice(dev)
	if d.Handle != dev || d.PCIBus != "" {
		t.Errorf("handle/pci = %v/%q", d.Handle, d.PCIBus)
	}
	if d.Power != nil || d.Temp != nil || d.Freq != nil || d.Engine != nil || d.RAS != nil || d.Mem != nil {
		t.Errorf("expected no sub-resources, got %+v", d)
	}
}

func TestClientReadsSuccess(t *testing.T) {
	withFns(t, fnTable{
		zesPowerGetEnergyCounter: func(_ PwrHandle, c *powerEnergyCounter) uint32 {
			c.Energy, c.Timestamp = 5000, 77
			return zeResultSuccess
		},
		zesTemperatureGetState: func(_ TempHandle, v *float64) uint32 {
			*v = 61.5
			return zeResultSuccess
		},
		zesFrequencyGetState: func(_ FreqHandle, s *freqState) uint32 {
			if s.Stype != stypeFreqState {
				return zeErrorUnknown
			}
			s.Actual, s.Request, s.TDP = 1200, 1500, 2100
			return zeResultSuccess
		},
		zesFrequencyGetThrottleTime: func(_ FreqHandle, tt *freqThrottleTime) uint32 {
			tt.ThrottleTime = 900
			return zeResultSuccess
		},
		zesEngineGetActivity: func(_ EngineHandle, s *engineStats) uint32 {
			s.ActiveTime, s.Timestamp = 333, 444
			return zeResultSuccess
		},
		zesRasGetState: func(_ RasHandle, clear uint8, s *rasState) uint32 {
			if clear != 0 {
				return zeErrorUnknown
			}
			s.Category[0], s.Category[6] = 2, 9
			return zeResultSuccess
		},
		zesMemoryGetState: func(_ MemHandle, s *memState) uint32 {
			s.Size, s.Free = 1000, 250
			return zeResultSuccess
		},
		zesMemoryGetBandwidth: func(_ MemHandle, b *memBandwidth) uint32 {
			b.ReadCounter, b.WriteCounter, b.MaxBandwidth, b.Timestamp = 1, 2, 3, 4
			return zeResultSuccess
		},
	})
	c := &Client{}
	h := handles(1)[0]

	if e, ts, ok := c.Energy(Power{Handle: h}); !ok || e != 5000 || ts != 77 {
		t.Errorf("Energy = (%d, %d, %v)", e, ts, ok)
	}
	if v, ok := c.Temperature(Temperature{Handle: h}); !ok || v != 61.5 {
		t.Errorf("Temperature = (%v, %v)", v, ok)
	}
	st, throttle, ok := c.FrequencyState(Frequency{Handle: h})
	if !ok || throttle != 900 || st.ActualMHz() != 1200 || st.RequestMHz() != 1500 || st.TDPMHz() != 2100 {
		t.Errorf("FrequencyState = (%+v, %d, %v)", st, throttle, ok)
	}
	if a, ts, ok := c.EngineActivity(Engine{Handle: h}); !ok || a != 333 || ts != 444 {
		t.Errorf("EngineActivity = (%d, %d, %v)", a, ts, ok)
	}
	cats, ok := c.RASCategories(RAS{Handle: h})
	if !ok || cats[0] != 2 || cats[6] != 9 || cats[3] != 0 {
		t.Errorf("RASCategories = (%v, %v)", cats, ok)
	}
	if size, free, ok := c.MemoryState(Memory{Handle: h}); !ok || size != 1000 || free != 250 {
		t.Errorf("MemoryState = (%d, %d, %v)", size, free, ok)
	}
	if r, w, m, ts, ok := c.MemoryBandwidth(Memory{Handle: h}); !ok || r != 1 || w != 2 || m != 3 || ts != 4 {
		t.Errorf("MemoryBandwidth = (%d, %d, %d, %d, %v)", r, w, m, ts, ok)
	}
}

func TestClientReadsFailure(t *testing.T) {
	withFns(t, fnTable{
		zesPowerGetEnergyCounter: func(PwrHandle, *powerEnergyCounter) uint32 { return zeErrorUnknown },
		zesTemperatureGetState:   func(TempHandle, *float64) uint32 { return zeErrorUnknown },
		zesFrequencyGetState:     func(FreqHandle, *freqState) uint32 { return zeErrorUnknown },
		zesEngineGetActivity:     func(EngineHandle, *engineStats) uint32 { return zeErrorUnknown },
		zesRasGetState:           func(RasHandle, uint8, *rasState) uint32 { return zeErrorUnknown },
		zesMemoryGetState:        func(MemHandle, *memState) uint32 { return zeErrorUnknown },
		zesMemoryGetBandwidth:    func(MemHandle, *memBandwidth) uint32 { return zeErrorUnknown },
	})
	c := &Client{}
	h := handles(1)[0]

	if _, _, ok := c.Energy(Power{Handle: h}); ok {
		t.Error("Energy should fail")
	}
	if _, ok := c.Temperature(Temperature{Handle: h}); ok {
		t.Error("Temperature should fail")
	}
	if _, _, ok := c.FrequencyState(Frequency{Handle: h}); ok {
		t.Error("FrequencyState should fail")
	}
	if _, _, ok := c.EngineActivity(Engine{Handle: h}); ok {
		t.Error("EngineActivity should fail")
	}
	if cats, ok := c.RASCategories(RAS{Handle: h}); ok || cats != [maxRasCategoryCount]uint64{} {
		t.Errorf("RASCategories = (%v, %v)", cats, ok)
	}
	if _, _, ok := c.MemoryState(Memory{Handle: h}); ok {
		t.Error("MemoryState should fail")
	}
	if _, _, _, _, ok := c.MemoryBandwidth(Memory{Handle: h}); ok {
		t.Error("MemoryBandwidth should fail")
	}
}

func TestFrequencyStateThrottleUnavailable(t *testing.T) {
	withFns(t, fnTable{
		zesFrequencyGetState: func(_ FreqHandle, s *freqState) uint32 {
			s.Actual = 800
			return zeResultSuccess
		},
		zesFrequencyGetThrottleTime: func(FreqHandle, *freqThrottleTime) uint32 { return zeErrorUnknown },
	})
	st, throttle, ok := (&Client{}).FrequencyState(Frequency{Handle: handles(1)[0]})
	if !ok || throttle != 0 || st.ActualMHz() != 800 {
		t.Errorf("FrequencyState = (%+v, %d, %v)", st, throttle, ok)
	}
}

func TestClientDevices(t *testing.T) {
	c := &Client{devices: []Device{{PCIBus: "0000:03:00.0"}}}
	if got := c.Devices(); len(got) != 1 || got[0].PCIBus != "0000:03:00.0" {
		t.Errorf("Devices = %+v", got)
	}
}

func TestOpenWithoutLoader(t *testing.T) {
	if Available() {
		t.Skip("level zero loader is installed on this host")
	}
	c, err := Open()
	if c != nil || !errors.Is(err, errUnavailable) {
		t.Errorf("Open = (%v, %v), want errUnavailable", c, err)
	}
}

func withLoaded(t *testing.T) {
	t.Helper()
	savedErr := loadErr
	loadOnce.Do(func() {})
	loadErr = nil
	t.Cleanup(func() {
		loadOnce = sync.Once{}
		loadErr = savedErr
	})
}

func deviceEnumerators(pci func(DeviceHandle, *pciProperties) uint32) fnTable {
	return fnTable{
		zesDevicePciGetProperties:       pci,
		zesDeviceEnumPowerDomains:       enumOf(nil),
		zesDeviceEnumTemperatureSensors: enumOf(nil),
		zesDeviceEnumFrequencyDomains:   enumOf(nil),
		zesDeviceEnumEngineGroups:       enumOf(nil),
		zesDeviceEnumRasErrorSets:       enumOf(nil),
		zesDeviceEnumMemoryModules:      enumOf(nil),
	}
}

func TestOpenEnumeratesDevicesAcrossDrivers(t *testing.T) {
	if Available() {
		t.Skip("level zero loader is installed on this host")
	}
	withLoaded(t)
	drivers := handles(3)
	devs := handles(3)
	f := deviceEnumerators(func(h DeviceHandle, p *pciProperties) uint32 {
		p.Address = pciAddress{Bus: uint32(idx(h) + 3)}
		return zeResultSuccess
	})
	f.zesInit = func(flags uint32) uint32 {
		if flags != 0 {
			return zeErrorUnknown
		}
		return zeResultSuccess
	}
	f.zesDriverGet = func(count *uint32, out *DriverHandle) uint32 {
		return enumOf(drivers)(nil, count, out)
	}
	f.zesDeviceGet = func(drv DriverHandle, count *uint32, out *DeviceHandle) uint32 {
		switch idx(drv) {
		case 0:
			return enumOf(devs[:2])(drv, count, out)
		case 1:
			return enumFail(drv, count, out)
		default:
			return enumOf(devs[2:])(drv, count, out)
		}
	}
	withFns(t, f)

	c, err := Open()
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	var got []string
	for _, d := range c.Devices() {
		got = append(got, d.PCIBus)
	}
	want := []string{"0000:03:00.0", "0000:04:00.0", "0000:05:00.0"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("devices = %v, want %v", got, want)
	}
}

func TestOpenErrors(t *testing.T) {
	if Available() {
		t.Skip("level zero loader is installed on this host")
	}
	cases := map[string]fnTable{
		"init fails": {
			zesInit: func(uint32) uint32 { return zeErrorUnknown },
		},
		"driver enumeration fails": {
			zesInit:      func(uint32) uint32 { return zeResultSuccess },
			zesDriverGet: func(*uint32, *DriverHandle) uint32 { return zeErrorUnknown },
		},
		"no drivers": {
			zesInit:      func(uint32) uint32 { return zeResultSuccess },
			zesDriverGet: func(count *uint32, _ *DriverHandle) uint32 { *count = 0; return zeResultSuccess },
		},
	}
	for name, f := range cases {
		t.Run(name, func(t *testing.T) {
			withLoaded(t)
			withFns(t, f)
			c, err := Open()
			if err == nil || c != nil {
				t.Errorf("Open = (%v, %v), want an error", c, err)
			}
		})
	}
}
