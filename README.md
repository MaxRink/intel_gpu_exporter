# intel_gpu_exporter

A Prometheus exporter for Intel GPUs on Linux, reading telemetry from every kernel interface the i915 and xe drivers offer.
At startup it walks /sys/class/drm, finds each Intel device and the driver bound to it, and enables whichever collectors that device can answer for.
Frequencies and RC6 come from sysfs, power and temperature from hwmon, per-process engine usage from DRM fdinfo, and high-resolution counters from the i915 and xe perf PMUs.
On i915 hosts that have intel_gpu_top installed it also runs intel_gpu_top -J as an additional source, and when the Level Zero loader is present it adds sysman data such as RAS error counts, memory bandwidth and engine group activity for Data Center GPUs like Flex and Max.
The startup log includes a kernel feature matrix listing which of these interfaces the running kernel exposes, so a missing metric comes with a reason.

Background on each source is in docs/kernel-feature-matrix.md, docs/pmu.md and docs/levelzero.md.

## Installation

Each release carries static Linux binaries for amd64 and arm64 on the [releases page](https://github.com/xsaveopt/intel_gpu_exporter/releases/latest).

```sh
sudo curl -fL -o /usr/local/bin/intel_gpu_exporter \
  https://github.com/xsaveopt/intel_gpu_exporter/releases/latest/download/intel_gpu_exporter_linux_amd64
sudo chmod +x /usr/local/bin/intel_gpu_exporter
```

To run it as a service, docs/systemd.md has a reference unit along with the service user it expects.
Building from source with make build puts the binary in bin/.

### Container image (MaxRink fork)

Tags pushed as `v*` build `ghcr.io/maxrink/intel_gpu_exporter:<tag>` (distroless static, linux/amd64 and arm64) via `.github/workflows/image.yml`.
Least-privilege example: run as root with `--cap-drop ALL --cap-add PERFMON --read-only --device /dev/dri/renderD128`, and add `--pid host --cap-add SYS_PTRACE` only for the per-process fdinfo collector.

## Configuration

Everything is set with command-line flags, and intel_gpu_exporter -h lists each one with its default.
Under systemd the flags go on the ExecStart= line of the unit.

The Level Zero collector turns itself on when libze_loader.so.1 is on the standard loader path.
On Debian and Ubuntu that library comes from the intel-level-zero-gpu and libze1 packages, and on Fedora and RHEL from intel-level-zero, after which the exporter picks it up on its next start.

A starter Grafana dashboard ships as docs/grafana/intel-gpu.json, and docs/grafana.md walks through its panels.

## Metrics

Every metric name starts with intel_gpu_.
Per-device series carry the card, pci, device and driver labels, while per-process fdinfo series carry pci, driver, pid, comm and engine.
PMU counters are labelled with pmu, event, family, engine and kind, and Level Zero series with pci, subdevice and an axis specific to the metric, such as sensor, domain, engine or module.
The full list of series and the kernel source behind each one is in docs/metrics.md.

Any Intel GPU bound to i915 or xe is discovered automatically, from integrated graphics through the DG1, Arc, Flex and Max discrete cards, and docs/hardware-coverage.md shows what each generation exposes.

## Permissions

| Collector       | Requires                                                   |
| --------------- | ---------------------------------------------------------- |
| sysfs (i915/xe) | read access to `/sys/class/drm`, world-readable by default |
| hwmon           | read access to `/sys/class/hwmon`                          |
| fdinfo          | `CAP_SYS_PTRACE` to read other users' file descriptors     |
| `intel_gpu_top` | `CAP_PERFMON` or root, depending on the kernel             |
| PMU             | `CAP_PERFMON` and `perf_event_paranoid` of 1 or lower      |
| Level Zero      | access to `/dev/dri/renderD*`, usually the `render` group  |

The reference unit in docs/systemd.md runs as a dedicated intel-gpu-exporter user with the render and video groups added for that service only, grants CAP_PERFMON for the PMU collector, and applies systemd hardening with /sys and /proc mounted read-only.

## License

GPL-2.0, see LICENSE.
