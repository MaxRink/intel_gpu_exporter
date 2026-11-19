# Exporter on distroless base (glibc: purego needs the dynamic loader to
# dlopen Level Zero, even with CGO_ENABLED=0). Run as root with every capability dropped
# except PERFMON (i915 PMU, memory-region free size) and, for per-client fdinfo
# with --pid host, SYS_PTRACE; read-only rootfs, only the render node.
FROM golang:1.27.4-trixie@sha256:f18c3d73a184fb0582828510722e723b63a93f7188d11dd223d9be6f6880e65d AS build
ARG VERSION=dev
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" -o /intel_gpu_exporter ./cmd/intel_gpu_exporter

FROM gcr.io/distroless/base-nossl-debian13@sha256:af5cb8dd589b8520b8c06bebb9efb73d7e16406cab58e85c51761fff49d370a0
COPY --from=build /intel_gpu_exporter /intel_gpu_exporter
EXPOSE 9404
ENTRYPOINT ["/intel_gpu_exporter"]
