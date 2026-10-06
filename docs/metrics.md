# Metrics

All metric names start with `intel_gpu_`. Internal scrape metrics: `intel_gpu_scrape_duration_seconds{source}`, `intel_gpu_scrape_success{source}`.

## Per-device gauges

Common labels: `card`, `pci`, `device`, `driver`.

| Metric                                              | Source                                |
|-----------------------------------------------------|---------------------------------------|
| `intel_gpu_info{…}` (constant 1)                    | static metadata: subsystem, revision, NUMA, modalias, tiles |
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
| `intel_gpu_engine_{heartbeat_interval_ms, preempt_timeout_ms, stop_timeout_ms, timeslice_duration_ms, max_busywait_duration_ns}` | same |

## Per-process gauges (DRM fdinfo)

Labels: `pci`, `driver`, `pid`, `comm`, `engine` or `region`. Capped at `--collector.fdinfo.top-n` busiest processes.

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
