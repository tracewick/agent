# syntax=docker/dockerfile:1
# Early-dev image. Multi-arch publish / SBOM / cosign land with M5 (GoReleaser).
# Do not bake a fixed USER here: set runAsUser / compose `user:` to match PHP-FPM
# (spool dirs are 0700 / files 0600 — a mismatched UID cannot read the spool).

ARG GO_VERSION=1.27.3

FROM golang:${GO_VERSION}-bookworm AS build
WORKDIR /src
COPY go.mod go.sum* ./
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/tracewick-agent ./cmd/tracewick-agent

FROM gcr.io/distroless/static-debian12
COPY --from=build /out/tracewick-agent /usr/local/bin/tracewick-agent
ENTRYPOINT ["/usr/local/bin/tracewick-agent"]
