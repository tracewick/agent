# Tracewick Agent

Long-running Go sidecar that delivers Tracewick profiles from a local spool to
[Pyroscope](https://pyroscope.io/).

PHP applications write **spool v2** files; this agent claims them, aggregates,
encodes **pprof**, and pushes via the Pyroscope Push API.

> [!WARNING]
> **Early development.** This repository is a work in progress. APIs, flags, and
> behaviour may change without notice. Do not use in production.

## Status

Early development: git flow, strict CI, lint, and coverage gates are in place.
Spool claim → aggregate → pprof → Push → ack loop works with `--once` and the
long-running `run` command; the binary is not production-ready yet.

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
  sentinel. If PHP raises those caps, raise the matching agent delivery limits
  too — otherwise capped-but-valid profiles are rejected as over budget.
- **Memory budgets** (`MaxDecodedBytes`, `MaxHeldDecodedBytes`,
  `MaxInFlightBytes`, encode string/protobuf caps) bound agent allocations;
  they are not a hard process RSS ceiling (encode still builds intermediate
  buffers before the final blob).

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

Password for Pyroscope basic auth: `TRACEWICK_PYROSCOPE_PASSWORD` in the environment (never
argv; same rule as `tracewick-drain`). Do not put credentials in `--pyroscope-url`.
Optional Prometheus scrape: `--metrics-listen` / `TRACEWICK_METRICS_LISTEN` serves `GET /metrics`.
Custom HTTPS CA: `--tls-ca` / `TRACEWICK_TLS_CA` (PEM); verification stays on.

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
- Benchmarks: [tracewick/benchmarks](https://github.com/tracewick/benchmarks)

## License

[Apache License 2.0](LICENSE)
