# Tracewick Agent

Long-running Go sidecar that delivers Tracewick profiles from a local spool to
[Pyroscope](https://pyroscope.io/).

PHP applications write **spool v2** files; this agent claims them, aggregates,
encodes **pprof**, and pushes via the Pyroscope Push API.

> [!WARNING]
> **Early development.** This repository is a work in progress. APIs, flags, and
> behaviour may change without notice. Do not use in production.

## Status

Bootstrap / enterprise floor is being established (git flow, strict CI, lint,
coverage gates). Delivery pipeline work starts after that (Docmost `go-agent` M1).

## Develop

```bash
git clone https://github.com/tracewick/agent.git
cd agent
make ci      # fmt, lint, govulncheck, race tests, coverage
make build
./bin/tracewick-agent version
```

See [CONTRIBUTING.md](CONTRIBUTING.md) and [AGENTS.md](AGENTS.md).

## Related

- Library: [tracewick/tracewick](https://github.com/tracewick/tracewick)
- Benchmarks: [tracewick/benchmarks](https://github.com/tracewick/benchmarks)

## License

[Apache License 2.0](LICENSE)
