# Exporter on distroless base (glibc: purego needs the dynamic loader to
# dlopen Level Zero, even with CGO_ENABLED=0). Run as root with every capability dropped
# except PERFMON (i915 PMU, memory-region free size) and, for per-client fdinfo
# with --pid host, SYS_PTRACE; read-only rootfs, only the render node.
FROM golang:1.27.4-trixie@sha256:0f82c58710c405cc012da2fbd2b04bde9ced20392a89f66062822d711d7d6b4b AS build
ARG VERSION=dev
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" -o /intel_gpu_exporter ./cmd/intel_gpu_exporter

FROM gcr.io/distroless/base-nossl-debian13@sha256:f21fdfe4ede467d9ea8a6caaec1f8381ea4502243a3e8bb1c43df80c6fbaaeae
COPY --from=build /intel_gpu_exporter /intel_gpu_exporter
EXPOSE 9404
ENTRYPOINT ["/intel_gpu_exporter"]
