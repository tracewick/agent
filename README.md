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

## Develop

```bash
git clone https://github.com/tracewick/agent.git
cd agent
make ci      # fmt, lint, govulncheck, race tests, coverage
make build
./bin/tracewick-agent version
./bin/tracewick-agent run --spool-dir=/var/spool/tracewick --pyroscope-url=http://localhost:4040 --once
./bin/tracewick-agent run --spool-dir=/var/spool/tracewick --pyroscope-url=http://localhost:4040 \
  --metrics-listen=127.0.0.1:9090
```

Password for Pyroscope basic auth: `TRACEWICK_PYROSCOPE_PASSWORD` in the environment (never argv).
Optional Prometheus scrape: `--metrics-listen` / `TRACEWICK_METRICS_LISTEN` serves `GET /metrics`.

See [CONTRIBUTING.md](CONTRIBUTING.md) and [AGENTS.md](AGENTS.md).

## Related

- Library: [tracewick/tracewick](https://github.com/tracewick/tracewick)
- Benchmarks: [tracewick/benchmarks](https://github.com/tracewick/benchmarks)

## License

[Apache License 2.0](LICENSE)
