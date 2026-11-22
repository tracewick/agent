# Tracewick Agent

[![CI](https://github.com/tracewick/agent/actions/workflows/ci.yml/badge.svg?branch=main&event=push)](https://github.com/tracewick/agent/actions/workflows/ci.yml?query=branch%3Amain+event%3Apush)
[![E2E](https://github.com/tracewick/agent/actions/workflows/e2e.yml/badge.svg?branch=main&event=push)](https://github.com/tracewick/agent/actions/workflows/e2e.yml?query=branch%3Amain+event%3Apush)
[![Coverage](https://codecov.io/gh/tracewick/agent/branch/main/graph/badge.svg)](https://codecov.io/gh/tracewick/agent)
[![Release](https://img.shields.io/github/v/release/tracewick/agent?include_prereleases)](https://github.com/tracewick/agent/releases)
[![GHCR](https://img.shields.io/badge/ghcr.io-tracewick%2Fagent-blue)](https://github.com/tracewick/agent/pkgs/container/agent)
[![License](https://img.shields.io/badge/license-Apache--2.0-blue.svg)](LICENSE)

Long-running Go sidecar that delivers Tracewick profiles from a local spool to
[Pyroscope](https://pyroscope.io/).

PHP applications write **spool v2** files; this agent claims them, aggregates,
encodes **pprof**, and pushes via the Pyroscope Push API.

> [!WARNING]
> **Release candidate (`1.0.0-rc.1`).** Pin the candidate explicitly and read the
> changelog before upgrading from 0.2. Flags, defaults and operational behaviour
> may still need compatibility fixes during RC validation; the stable contract
> starts with final 1.0.0. Capacity evidence lives in
> [`tracewick/benchmarks`](https://github.com/tracewick/benchmarks) and describes
> its recorded versions and workloads.

## Status

| In this candidate | Later |
| --- | --- |
| Spool claim → aggregate → pprof → Push → ack (`run` / `--once`) | |
| Same claim protocol as PHP `tracewick-drain` (at-least-once) | Unix socket hand-off (v1.1+) |
| Stand + Toxiproxy + live Querier + Go↔PHP-drain E2E | |
| Operational CLI/env settings (new since 0.2.1) | |
| linux amd64/arm64 binaries + `ghcr.io/tracewick/agent` | |
| Codecov / mutation / fuzz / `go tool pprof` CI gates | |
| Soak / RPS / chaos / spool-pressure goldens (agent reader) | |

The release workflow generates archive SBOMs, signs their checksum manifest with
keyless Cosign, and signs SBOM-bearing GHCR image indexes. Existing 0.2.x releases
remain **unsigned**; the first signed publication still needs a successful release
run. See [verification and recovery](deploy/verify-release.md).

### Ops assumptions

- **One agent process per spool directory**, same UID/GID as the PHP writer.
  A second agent on the same directory is claim-safe (rename races) but each
  process has its own cardinality gate and may re-deliver after `StaleClaim`.
- **Aggregation window** (default 15s) groups files already claimed in the
  current pass. It does **not** wait for the window to close before push; the
  same logical bucket split across two passes becomes two Push requests
  (at-least-once).
- **Decode / merge limits** default to the PHP `BoundedProfileBuilder` defaults
  (`4096` stacks / `16384` frames / `32768` strings), plus one wire overflow
  sentinel. If PHP raises those caps, raise the agent with `--max-stacks` /
  `--max-frames` / `--max-strings` — otherwise capped-but-valid profiles are
  rejected as over budget.
- **Memory budgets** (`--max-held-decoded-bytes` as a conservative
  `Profile.EstimatedBytes` sum, `--max-in-flight-bytes` as the claimed-spool
  sum and encoded push cap) bound agent allocations; they are not a hard
  process RSS ceiling.

### Capacity evidence

Load / soak / capacity harness lives in
[`tracewick/benchmarks`](https://github.com/tracewick/benchmarks) — not here.
Canonical cloud goldens use depth-48 stacks on an 8 vCPU Timeweb VPS with
**go-agent `0.1.0`** as `READER=agent`.

| Scenario | What the agent showed |
| --- | --- |
| Soak 6h @ ~10 RPS | FPM / Swoole / RoadRunner: app + agent RSS **stable**, k6 **0%** fail |
| High-RPS hold ~700 | HTTP path matches PHP drain; spool stayed **~222 peak / ~109 last** vs drain soft-admission **~600** |
| Pyroscope +3s latency | App green; agent backlog **~172** files vs drain hitting soft **500** |
| Spool pressure catch-up | Same 80-file soft ceiling cleared in **~2 s** vs drain **~40 s** |

Full tables and pins:

- [72-hour RoadRunner + agent soak](https://github.com/tracewick/benchmarks/blob/main/reports/2026-11-22-soak-rr-agent-72h.md)
  records agent `a15f0d3`; the [current-development campaign](https://github.com/tracewick/benchmarks/pull/11)
  covers this candidate's runtime source `d464fac`, including recovery and real Symfony delivery.
  The latter is a separate campaign, not another 72-hour run.
- [soak FPM](https://github.com/tracewick/benchmarks/blob/main/reports/2026-11-07-soak-fpm-agent-depth48-cloud.md) ·
  [Swoole](https://github.com/tracewick/benchmarks/blob/main/reports/2026-11-07-soak-swoole-agent-depth48-cloud.md) ·
  [RoadRunner](https://github.com/tracewick/benchmarks/blob/main/reports/2026-11-08-soak-rr-agent-depth48-cloud.md)
- [RPS pipeline](https://github.com/tracewick/benchmarks/blob/main/reports/2026-11-06-rps-pipeline-agent-depth48-cloud.md)
- [chaos](https://github.com/tracewick/benchmarks/blob/main/reports/2026-11-06-chaos-pyroscope-agent-depth48-cloud.md)
- [spool pressure](https://github.com/tracewick/benchmarks/blob/main/reports/2026-11-06-spool-pressure-agent-depth48-cloud.md)

Library-side overhead (app vs off) and method limits:
[METHOD.md](https://github.com/tracewick/benchmarks/blob/main/METHOD.md) /
[LIMITATIONS.md](https://github.com/tracewick/benchmarks/blob/main/LIMITATIONS.md).

### Migrating from `tracewick-drain`

The library binary `vendor/bin/tracewick-drain` remains supported. Prefer the
agent when you want a long-running process with HTTP connection reuse.

| Topic | Notes |
| --- | --- |
| Spool contract | Same directory layout and claim rename ([spool format](https://github.com/tracewick/tracewick/blob/main/docs/spool-format.md)) |
| Delivery | **At-least-once**; duplicates possible after crash / lost 2xx |
| UID | Agent must share UID/GID with the PHP writer (`0700`/`0600`); see [deploy/](deploy/) |
| Auth / 404 | Agent **pauses** and keeps spool files (does not reject as trash on 401/403/404) — intentional difference from PHP drain |
| Coexistence | Drain and agent may share one spool; claim rename elects one winner per file |
| Stand | Local Pyroscope + Toxiproxy: `make stand-up` (see [CONTRIBUTING.md](CONTRIBUTING.md)) |

## Canon (main repository)

Wire format and delivery decisions live in
[`tracewick/tracewick`](https://github.com/tracewick/tracewick):

- [Spool format](https://github.com/tracewick/tracewick/blob/main/docs/spool-format.md)
- [ADR 4 — pprof](https://github.com/tracewick/tracewick/blob/main/docs/adr/0004-pprof-as-primary-format.md)
- [ADR 5 — fail-open](https://github.com/tracewick/tracewick/blob/main/docs/adr/0005-fail-open-policy.md)
- [ADR 7 — language-neutral spool](https://github.com/tracewick/tracewick/blob/main/docs/adr/0007-language-neutral-spool-format.md)
- [ADR 8 — own HTTP transport](https://github.com/tracewick/tracewick/blob/main/docs/adr/0008-own-http-transport.md)
- [ADR 9 — agent hand-off](https://github.com/tracewick/tracewick/blob/main/docs/adr/0009-agent-hand-off.md)
- [ADR 12 — aggregate before delivery](https://github.com/tracewick/tracewick/blob/main/docs/adr/0012-aggregate-before-delivery.md)
- [ADR 13 — compact-stacks](https://github.com/tracewick/tracewick/blob/main/docs/adr/0013-spool-v2-compact-payload.md)

## Install

Pin a release tag (see [Releases](https://github.com/tracewick/agent/releases)).
Existing 0.2.x artifacts are **unsigned** — verify `checksums.txt` (and prefer an image digest
for containers).
For this candidate, first [verify the signed checksums and matching archive/SBOM](deploy/verify-release.md).
The commands below become usable after the candidate's release workflow publishes its assets.

### Binary (linux amd64 / arm64)

Archives ship the binary as `tracewick-agent`:

```bash
VER=1.0.0-rc.1
ARCH=amd64   # or arm64
ARCHIVE="tracewick-agent_${VER}_linux_${ARCH}.tar.gz"
curl -fsSL -o "$ARCHIVE" \
  "https://github.com/tracewick/agent/releases/download/v${VER}/${ARCHIVE}" || exit 1
curl -fsSL -o checksums.txt \
  "https://github.com/tracewick/agent/releases/download/v${VER}/checksums.txt" || exit 1
# Authenticate checksums.txt with Cosign as linked above before continuing.
awk -v name="$ARCHIVE" '$2 == name {print; count++} END {if (count != 1) exit 1}' \
  checksums.txt > selected-checksum.txt || exit 1
sha256sum --check selected-checksum.txt || exit 1
tar -xzf "$ARCHIVE" tracewick-agent
sudo install -m 0755 tracewick-agent /usr/local/bin/tracewick-agent
tracewick-agent version
```

### Container (GHCR)

```bash
docker pull ghcr.io/tracewick/agent:1.0.0-rc.1
# pin by digest in production when possible:
# docker pull ghcr.io/tracewick/agent@sha256:…
```

Compose / Kubernetes examples (UID-compatible): [`deploy/`](deploy/).
RC images have versioned tags (`1.0.0-rc.1` and `v1.0.0-rc.1`); they do not update `latest`.

## Configure and run

Required: a spool directory writable by the **same UID/GID** as the PHP writer,
and a Pyroscope base URL.

| Setting | Flag | Environment |
| --- | --- | --- |
| Spool directory | `--spool-dir` | `TRACEWICK_SPOOL_DIR` |
| Pyroscope URL | `--pyroscope-url` | `TRACEWICK_PYROSCOPE_URL` |
| Basic auth user | `--pyroscope-user` | `TRACEWICK_PYROSCOPE_USER` |
| Basic auth password | *(env only)* | `TRACEWICK_PYROSCOPE_PASSWORD` |
| Tenant (`X-Scope-OrgID`) | `--pyroscope-tenant` | `TRACEWICK_PYROSCOPE_TENANT` |
| Prometheus `/metrics` | `--metrics-listen` | `TRACEWICK_METRICS_LISTEN` |
| Extra TLS CA PEM | `--tls-ca` | `TRACEWICK_TLS_CA` |
| Reader identity | `--reader-id` | `TRACEWICK_READER_ID` |
| Merge window (integer seconds, 10–15) | `--aggregation-window` | `TRACEWICK_AGGREGATION_WINDOW` |
| Idle delay (duration, e.g. `250ms`, `5s`) | `--sleep` | `TRACEWICK_SLEEP` |
| Shutdown grace (duration, e.g. `30s`) | `--shutdown-grace` | `TRACEWICK_SHUTDOWN_GRACE` |
| Rejected-file retention (count) | `--max-rejected-files` | `TRACEWICK_MAX_REJECTED_FILES` |
| Stack cap (count) | `--max-stacks` | `TRACEWICK_MAX_STACKS` |
| Frame cap (count) | `--max-frames` | `TRACEWICK_MAX_FRAMES` |
| String cap (count) | `--max-strings` | `TRACEWICK_MAX_STRINGS` |
| Held decoded budget (decimal bytes) | `--max-held-decoded-bytes` | `TRACEWICK_MAX_HELD_DECODED_BYTES` |
| In-flight budget (decimal bytes) | `--max-in-flight-bytes` | `TRACEWICK_MAX_IN_FLIGHT_BYTES` |

An explicitly supplied CLI option wins over env **including an empty value**. Empty optional
strings clear the setting (for example `--metrics-listen=` disables metrics); `--reader-id=`
resets the automatic host/pid identifier. Empty required strings or numeric/duration options
fail validation, without falling back to env. A valid CLI override ignores an invalid env value.
Empty environment variables mean unset and use defaults. `--once` and `--help` remain CLI-only.

Formats and bounds match CLI. Zero sleep/grace and rejected retention are valid; zero decode/memory
caps keep existing defaults. Byte budgets use decimal byte counts, not `64M` suffixes. PHP drain
uses integer seconds in `TRACEWICK_SLEEP_SECONDS`; this agent uses duration strings in
`TRACEWICK_SLEEP`. Defaults are unchanged (see `tracewick-agent run --help`).

The agent reads the process environment (`os.Getenv`); it does not load `.env` files. Docker
Compose's `.env` is an interpolation source: pass values through `environment` or `env_file`
to put them in the container environment. systemd can supply them with `EnvironmentFile`.
Password remains environment-only and must not appear on argv or in URL userinfo; clearing an
auth user while a password is present is an error.

### Long-running sidecar (recommended)

```bash
export TRACEWICK_SPOOL_DIR=/var/spool/tracewick
export TRACEWICK_PYROSCOPE_URL=https://pyroscope.example
export TRACEWICK_PYROSCOPE_USER=…          # if needed
export TRACEWICK_PYROSCOPE_PASSWORD=…      # if needed

tracewick-agent run --reader-id="$(hostname)-1"
```

`--once` delivers a single pass and exits (cron / one-shot drain). Omit it for
the normal loop (idle poll default `--sleep=5s`).

### systemd

Run as the PHP pool user (example: `www-data`). Adjust paths and unit name.

```ini
# /etc/systemd/system/tracewick-agent.service
[Unit]
Description=Tracewick agent (spool → Pyroscope)
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=www-data
Group=www-data
Environment=TRACEWICK_SPOOL_DIR=/var/spool/tracewick
Environment=TRACEWICK_PYROSCOPE_URL=https://pyroscope.example
EnvironmentFile=-/etc/tracewick/agent.env
ExecStart=/usr/local/bin/tracewick-agent run --reader-id=%H-1
Restart=on-failure
RestartSec=5

[Install]
WantedBy=multi-user.target
```

Put secrets in `/etc/tracewick/agent.env` (`TRACEWICK_PYROSCOPE_PASSWORD=…`,
mode `0600`, owned by root). Then:

```bash
sudo systemctl daemon-reload
sudo systemctl enable --now tracewick-agent
sudo systemctl status tracewick-agent
```

### Docker

Match the host/PHP UID so the agent can open `0700`/`0600` spool files:

```bash
docker run --rm --user "$(id -u):$(id -g)" \
  -v /var/spool/tracewick:/var/spool/tracewick \
  -e TRACEWICK_SPOOL_DIR=/var/spool/tracewick \
  -e TRACEWICK_PYROSCOPE_URL=https://pyroscope.example \
  -e TRACEWICK_PYROSCOPE_PASSWORD \
  ghcr.io/tracewick/agent:1.0.0-rc.1 run --reader-id=sidecar-1
```

For a one-shot pass, append `--once`. Compose and Kubernetes manifests:
[`deploy/`](deploy/).

### Optional metrics

```bash
tracewick-agent run \
  --spool-dir=/var/spool/tracewick \
  --pyroscope-url=https://pyroscope.example \
  --metrics-listen=127.0.0.1:9100
```

Scrape `http://127.0.0.1:9100/metrics` (empty `--metrics-listen` disables the
listener).

## Develop

```bash
git clone https://github.com/tracewick/agent.git
cd agent
make ci      # fmt, lint, govulncheck, race tests, coverage
make e2e     # cross-process E2E (separate workflow; build tag e2e)
make build
./bin/tracewick-agent version
./bin/tracewick-agent run --spool-dir=/var/spool/tracewick --pyroscope-url=http://localhost:4040 --once
```

See [CONTRIBUTING.md](CONTRIBUTING.md) and [AGENTS.md](AGENTS.md).

## Related

- Library: [tracewick/tracewick](https://github.com/tracewick/tracewick)
- Drain docs: [docs/drain.md](https://github.com/tracewick/tracewick/blob/main/docs/drain.md)
- Benchmarks: [tracewick/benchmarks](https://github.com/tracewick/benchmarks)

## License

[Apache License 2.0](LICENSE)
