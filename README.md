# Tracewick Agent

Long-running Go sidecar that delivers Tracewick profiles from a local spool to [Pyroscope](https://pyroscope.io/).

PHP applications write **spool v2** files; this agent claims them, aggregates, encodes **pprof**, and pushes via the Pyroscope Push API.

> Status: scaffolding. Not production-ready yet.

## Related

- Library: [tracewick/tracewick](https://github.com/tracewick/tracewick)
- Benchmarks: [tracewick/benchmarks](https://github.com/tracewick/benchmarks)

## License

Apache-2.0 (planned; LICENSE to be added with the first code drop).
