# Exporter on distroless base (glibc: purego needs the dynamic loader to
# dlopen Level Zero, even with CGO_ENABLED=0). Run as root with every capability dropped
# except PERFMON (i915 PMU, memory-region free size) and, for per-client fdinfo
# with --pid host, SYS_PTRACE; read-only rootfs, only the render node.
FROM golang:1.27.4-trixie@sha256:430985542100b871e108ccc00e8dcc5eef08ca14ee5825e98ebf563d8ffec4ee AS build
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
