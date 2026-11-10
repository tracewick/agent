# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [0.2.0](https://github.com/tracewick/agent/compare/v0.1.0...v0.2.0) (2026-11-09)


### Fixed

* **release:** GHCR via GITHUB_TOKEN + republish dispatch ([a98a135](https://github.com/tracewick/agent/commit/a98a135aefac4a12deed8d7a8ed41efc9c7673d2))
* **release:** push GHCR with GITHUB_TOKEN, not the App ([2672bbc](https://github.com/tracewick/agent/commit/2672bbccd6ba14a81f2b7d0c30b4f3f13ba2f198))

## 0.1.0 (2026-11-05)


### Added

* **agent:** Drainer one-pass and long-running Agent loop ([#7](https://github.com/tracewick/agent/issues/7)) ([4cc99fb](https://github.com/tracewick/agent/commit/4cc99fbd6b084f6977c8fbfc3dab07489f337eea))
* **aggregate:** ADR-12 key, window, cardinality, GroupBy ([#4](https://github.com/tracewick/agent/issues/4)) ([5f33041](https://github.com/tracewick/agent/commit/5f3304118429ba9acdb28ba8f4eac6f890cc2518))
* **delivery:** HTTP classify, ownership budget, limits, shutdown ([#6](https://github.com/tracewick/agent/issues/6)) ([1827c05](https://github.com/tracewick/agent/commit/1827c05da564e9f27e914623d24fd644013029f3))
* **deploy:** UID-compatible Docker/Compose/K8s spool examples ([#10](https://github.com/tracewick/agent/issues/10)) ([c0b4f32](https://github.com/tracewick/agent/commit/c0b4f32ab89d002f2f26636f30cdc52eda8dcc08))
* **e2e:** two Go agents on one spool directory ([#11](https://github.com/tracewick/agent/issues/11)) ([5a32bda](https://github.com/tracewick/agent/commit/5a32bda1fb1f4b8532afca1e2794efe8eba8eca8))
* **metrics:** §3.4 registry and optional Prometheus /metrics ([#8](https://github.com/tracewick/agent/issues/8)) ([a38bafa](https://github.com/tracewick/agent/commit/a38bafab3d1b86f5f2db99777e428d90f38ca69f))
* **pprof:** compact-stacks decode/merge and pprof encode ([#5](https://github.com/tracewick/agent/issues/5)) ([d6af78a](https://github.com/tracewick/agent/commit/d6af78a3d4597b78defbcaf5f357199e8a8d29a8))
* **pyroscope:** Push API encode + httptest client ([#3](https://github.com/tracewick/agent/issues/3)) ([88bcc7b](https://github.com/tracewick/agent/commit/88bcc7bcc7d44b0d85b50117ba796902fcaa8d04))
* **release:** GoReleaser binaries + multi-arch GHCR ([#27](https://github.com/tracewick/agent/issues/27)) ([2837fb0](https://github.com/tracewick/agent/commit/2837fb066e6b6e313bd18b6b4b699eabfd561700))
* **spool:** parse SpoolFile v2 and directory claim protocol ([#2](https://github.com/tracewick/agent/issues/2)) ([40d5966](https://github.com/tracewick/agent/commit/40d59669ef4dc9d3439fa57840447c4104b9fe72))
* **stand:** Pyroscope+Toxiproxy stand and CI pprof/fuzz hygiene ([#12](https://github.com/tracewick/agent/issues/12)) ([92dd31d](https://github.com/tracewick/agent/commit/92dd31d7caab616732355a1eec3a54ffe74243f0))
* **tls:** custom CA roots and slog secret redact ([#9](https://github.com/tracewick/agent/issues/9)) ([d4fc4b3](https://github.com/tracewick/agent/commit/d4fc4b32b1b8b73f33a727124316bf0ab1c337c8))


### Fixed

* **agent:** cardinality store TTL, decoded budgets, int64 samples ([#24](https://github.com/tracewick/agent/issues/24)) ([83bcadd](https://github.com/tracewick/agent/commit/83bcadd3f6dab3c31fc2bfbf7453c18ea2523f6c))
* **agent:** encode reuse + pre-tag review seams (weight, budgets, ack) ([#23](https://github.com/tracewick/agent/issues/23)) ([df2fe3a](https://github.com/tracewick/agent/commit/df2fe3affcd4a4de11a2e9292dfabbdba636f5f5))
* **agent:** pre-alpha correctness seams from review ([#20](https://github.com/tracewick/agent/issues/20)) ([aa32d4b](https://github.com/tracewick/agent/commit/aa32d4be8857aa739654d2b66093e227febcf138))
* **pprof:** PHP overflow wire budget and decode memory caps ([#21](https://github.com/tracewick/agent/issues/21)) ([92768a3](https://github.com/tracewick/agent/commit/92768a36bdf87bd12666136fdb14a5245b5b7c2f))
* **pprof:** wire overflow frames/strings and encode memory budgets ([#22](https://github.com/tracewick/agent/issues/22)) ([ff6cb9d](https://github.com/tracewick/agent/commit/ff6cb9d2b9a69714b4f021030414ecdf94f69bfb))

## [Unreleased]

Bootstrap only — no delivery features yet.
