# Metrics

All metric names start with `intel_gpu_`. Internal scrape metrics: `intel_gpu_scrape_duration_seconds{source}`, `intel_gpu_scrape_success{source}`.

## Per-device gauges

Common labels: `card`, `pci`, `device`, `driver`.

| Metric                                              | Source                                |
|-----------------------------------------------------|---------------------------------------|
| `intel_gpu_info{…}` (constant 1)                    | static metadata: subsystem, revision, NUMA, modalias, tiles |
| `intel_gpu_runtime_suspended`                       | PCI `power/runtime_status`; while 1, the i915/xe sysfs, hwmon, PCIe and memory sources replay their last samples instead of waking the GPU |
| `intel_gpu_i915_frequency_{actual,requested,min,max,rp0,rpn,boost}_mhz` (label `gt`) | i915 sysfs (`gt_*_freq_mhz` + per-GT `gt/gtN/rps_*`) |
| `intel_gpu_i915_rc6_residency_ms`                   | `cardN/power/rc6_residency_ms`        |
| `intel_gpu_xe_frequency_{actual,requested,rp0,rpa,rpn}_mhz` (labels `tile`, `gt`) | xe sysfs `tile*/gt*/freq0/` |
| `intel_gpu_xe_throttle_reason` (label `reason`)     | xe sysfs `freq0/throttle/`            |
| `intel_gpu_hwmon_{power_max,power_rated_max,power_crit}_watts` (labels `hwmon`, `channel`) | hwmon |
| `intel_gpu_hwmon_energy_joules_total`               | hwmon `energy*_input`                 |
| `intel_gpu_hwmon_temperature_celsius`               | hwmon `temp*_input`                   |
| `intel_gpu_hwmon_fan_rpm`                           | hwmon `fan*_input`                    |
| `intel_gpu_hwmon_{current_amperes,voltage_volts}`   | hwmon `curr*` / `in*`                 |
| `intel_gpu_pcie_{current,max}_link_speed_gtps`      | `/sys/bus/pci/.../current_link_speed` |
| `intel_gpu_pcie_{current,max}_link_width`           | `current_link_width`                  |
| `intel_gpu_pcie_{current,max}_generation`           | derived from link speed (Gen1–6)      |
| `intel_gpu_memory_lmem_total_bytes`                 | i915 `lmem_total_bytes` (when published) |
| `intel_gpu_memory_vram_total_bytes` (label `tile`)  | xe `physical_vram_size_bytes`         |
| `intel_gpu_memory_region_{total,free}_bytes` (label `region`: `local0` = VRAM, `system0`) | i915 `DRM_I915_QUERY_MEMORY_REGIONS` on the render node; free needs `CAP_PERFMON` (otherwise equals total) |
| `intel_gpu_i915_throttle_reason` (labels `gt`, `reason`; `status` = any) | i915 sysfs `gt/gtN/throttle_reason_*` |
| `intel_gpu_engine_info{capabilities, known_capabilities}` (constant 1) | i915 `engine/<name>/` |
| `intel_gpu_i915_frequency_{rp1,punit_request}_mhz`, `intel_gpu_i915_media_frequency_{rp0,rpn}_mhz`, `intel_gpu_i915_media_frequency_factor_ratio` (label `gt`) | i915 sysfs `gt/gtN/rps_RP1_freq_mhz`, `punit_req_freq_mhz`, `media_*` |
| `intel_gpu_i915_rc6_enabled_bool`, `intel_gpu_i915_slpc_ignore_efficient_frequency_bool` (label `gt`) | `gt/gtN/rc6_enable`, `slpc_ignore_eff_freq` |
| `intel_gpu_i915_error_state_present`                | `cardN/error` (1 after a GPU hang/reset capture) |
| `intel_gpu_hwmon_power_max_interval_seconds`        | hwmon `power*_max_interval` (PL1 tau) |
| `intel_gpu_pcie_upstream_{current,max}_link_{speed_gtps,width}` (labels `hop`, `port`) | every upstream port up to the root port (discrete cards sit behind an internal switch; the slot link is a higher hop) |
| `intel_gpu_uc_firmware_info{gt,fw,status,version,file}` (constant 1), `intel_gpu_uc_running{gt,fw}` | DRM debugfs `gtN/uc/{guc,huc,gsc}_info`, only with `--collector.debugfs.path` |
| `intel_gpu_i915_frequency_rpe_mhz`                  | debugfs `i915_frequency_info` (efficient frequency) |
| `intel_gpu_engine_{heartbeat_interval_ms, preempt_timeout_ms, stop_timeout_ms, timeslice_duration_ms, max_busywait_duration_ns}` | same |

## Per-process gauges (DRM fdinfo)

Also `intel_gpu_client_memory_{purgeable,active}_bytes` and `intel_gpu_client_engine_capacity` (engines per class; divide busy time by it). Labels: `pci`, `driver`, `pid`, `comm`, `engine` or `region`. Capped at `--collector.fdinfo.top-n` busiest processes. `--collector.fdinfo.rescan-interval` (e.g. `60s`) limits the full `/proc` walk; in between only PIDs that held a DRM fd are re-read, so new clients appear within that interval. `--collector.fdinfo.containers-only` reads only processes whose `/proc/<pid>/cgroup` is a container cgroup (Docker, Podman, CRI-O, containerd CRI, Kubernetes). That file needs no ptrace access check, so host processes are never ptrace-checked: run in a container with `pid: host` and `SYS_PTRACE`, and AppArmor docker-default logs no denials for unconfined host peers. Whether a PID holds `/dev/dri` open cannot be read without the same ptrace check, so the cgroup is the prefilter. Better: `--collector.fdinfo.drm-clients=<dir>` with the host's `/sys/kernel/debug/dri` bind-mounted read-only at `<dir>` reads the DRM client tgids from debugfs (`<minor>/clients`, no ptrace check) and touches only those PIDs, with no `/proc` walk at all; combine with `containers-only`. A GPU client in an unconfined (for example privileged) container still cannot be read under docker-default AppArmor.

- `intel_gpu_client_engine_time_seconds_total`
- `intel_gpu_client_engine_{cycles,total_cycles}_total` from the xe drm-cycles and drm-total-cycles keys
- `intel_gpu_client_memory_{total,resident,shared}_bytes`
- `intel_gpu_client_dropped_processes` — processes dropped by the cap

## PMU counters

`intel_gpu_pmu_counter{pmu, event, family, engine, kind}` — see [pmu.md](pmu.md).

## `intel_gpu_top` fallback

`intel_gpu_gputop_{frequency_*, power_watts, engine_busy_ratio, rc6_ratio, imc_bandwidth_*, interrupts_per_second}` — only emitted when `intel_gpu_top -J` is running. Disable via `--collector.intel-gpu-top=false` once PMU is reachable.

## Level Zero (Data Center GPUs)

`intel_gpu_zes_*` — see [levelzero.md](levelzero.md).
