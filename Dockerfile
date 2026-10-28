# Exporter on distroless base (glibc: purego needs the dynamic loader to
# dlopen Level Zero, even with CGO_ENABLED=0). Run as root with every capability dropped
# except PERFMON (i915 PMU, memory-region free size) and, for per-client fdinfo
# with --pid host, SYS_PTRACE; read-only rootfs, only the render node.
FROM golang:1.27.3-trixie@sha256:c65ecfe613b539ac74c62a4c3b2c27e8f8a6076384c8f27e8a3c0f3be17cc385 AS build
ARG VERSION=dev
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" -o /intel_gpu_exporter ./cmd/intel_gpu_exporter

FROM gcr.io/distroless/base-nossl-debian13@sha256:ca1c7e6a875e59345ae6ff182a1e0930f0ca1ffef23a9b830f8823ab348ad5dd
COPY --from=build /intel_gpu_exporter /intel_gpu_exporter
EXPOSE 9404
ENTRYPOINT ["/intel_gpu_exporter"]
