# Tracewick Agent

Long-running Go sidecar that delivers Tracewick profiles from a local spool to [Pyroscope](https://pyroscope.io/).

PHP applications write **spool v2** files; this agent claims them, aggregates, encodes **pprof**, and pushes via the Pyroscope Push API.

> [!WARNING]
> **Early development.** This repository is a work in progress. APIs, flags, and behaviour may change without notice. Do not use in production.

## Related

- Library: [tracewick/tracewick](https://github.com/tracewick/tracewick)
- Benchmarks: [tracewick/benchmarks](https://github.com/tracewick/benchmarks)

## License

Apache-2.0 (planned; LICENSE to be added with the first code drop).
