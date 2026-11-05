# Tracewick Agent

Long-running Go sidecar that delivers Tracewick profiles from a local spool to
[Pyroscope](https://pyroscope.io/).

PHP applications write **spool v2** files; this agent claims them, aggregates,
encodes **pprof**, and pushes via the Pyroscope Push API.

> [!WARNING]
> **Alpha (`0.1.x`).** Suitable for experimentation and early adoption. Flags,
> defaults, and ops behaviour may still change before 1.0.0. Pin a release and
> read the changelog before upgrading. Not a production / highload readiness
> claim — soak evidence lives in
> [`tracewick/benchmarks`](https://github.com/tracewick/benchmarks).

## Status

| In alpha | Not yet |
| --- | --- |
| Spool claim → aggregate → pprof → Push → ack (`run` / `--once`) | Signed releases (cosign) / SBOM |
| Same claim protocol as PHP `tracewick-drain` (at-least-once) | Unix socket hand-off (v1.1+) |
| Stand + Toxiproxy + live Querier + Go↔PHP-drain E2E | Stable CLI/env contract (beta+) |
| linux amd64/arm64 binaries + `ghcr.io/tracewick/agent` | |
| Codecov / mutation / fuzz / `go tool pprof` CI gates | |

GitHub Releases for `v0.1.x` are **unsigned** until cosign is wired. Prefer
`checksums.txt` (SHA-256) and pinned GHCR digests.

### Ops assumptions (alpha)

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

## Develop

```bash
git clone https://github.com/tracewick/agent.git
cd agent
make ci      # fmt, lint, govulncheck, race tests, coverage
make e2e     # cross-process E2E (separate workflow; build tag e2e)
make build
./bin/tracewick-agent version
./bin/tracewick-agent run --spool-dir=/var/spool/tracewick --pyroscope-url=http://localhost:4040 --once
./bin/tracewick-agent run --spool-dir=/var/spool/tracewick --pyroscope-url=http://localhost:4040 \
  --metrics-listen=127.0.0.1:9090
```

Password for Pyroscope basic auth: `TRACEWICK_PYROSCOPE_PASSWORD` in the
environment (never argv; same rule as `tracewick-drain`). Do not put credentials
in `--pyroscope-url`. Optional Prometheus scrape: `--metrics-listen` /
`TRACEWICK_METRICS_LISTEN`. Custom HTTPS CA: `--tls-ca` / `TRACEWICK_TLS_CA`.

### Spool permissions (UID)

PHP writes spool dirs as `0700` and files as `0600`. Run the agent as the **same
UID/GID** as PHP-FPM (or set `runAsUser` / `fsGroup` together). Examples:

- Compose: [`deploy/docker-compose.yml`](deploy/docker-compose.yml)
- Kubernetes: [`deploy/kubernetes/agent.yaml`](deploy/kubernetes/agent.yaml)
- Notes: [`deploy/README.md`](deploy/README.md)

```bash
make docker-build
docker compose -f deploy/docker-compose.yml up
```

See [CONTRIBUTING.md](CONTRIBUTING.md) and [AGENTS.md](AGENTS.md).

## Related

- Library: [tracewick/tracewick](https://github.com/tracewick/tracewick)
- Drain docs: [docs/drain.md](https://github.com/tracewick/tracewick/blob/main/docs/drain.md)
- Benchmarks: [tracewick/benchmarks](https://github.com/tracewick/benchmarks)

## License

[Apache License 2.0](LICENSE)
