# Cross-process E2E (M4+ → alpha)

These tests exercise real `tracewick-agent` processes against a shared spool
directory. They are **not** part of `make ci` / the `Checks` job
(`go test -race` does not replace this class).

```bash
make stand-up
export PYROSCOPE_URL=http://127.0.0.1:4040
export PYROSCOPE_TOXIC_URL=http://127.0.0.1:14040
export TOXIPROXY_URL=http://127.0.0.1:8474
make e2e
make stand-down
```

Without the three env vars, stand-backed tests **skip**; the two-agent claim-smoke
still runs (httptest). CI `e2e.yml` starts the stand and exports the env.

## Path to alpha

README may still say early development until the first `v0.x` tag. That label is
**not** permission to skip repository lifecycle. M4–M5 close **alpha
readiness**: stand, Toxiproxy chaos, Querier live round-trip, Go + PHP-drain
coexistence, CI hygiene (pprof goldens, fuzz, mutation), then a real release
cut. Soak/load belong with [`tracewick/benchmarks`](https://github.com/tracewick/benchmarks).

## Canon (parity with `tracewick/tracewick`)

| Concern | Approach |
| --- | --- |
| Claim coexistence | Real processes, one spool dir (two Go agents; later Go + `tracewick-drain`) |
| Pyroscope faults | **Toxiproxy** (`timeout` / `latency` / `reset_peer` / …) via `PYROSCOPE_TOXIC_URL` |
| Healthy push | `PYROSCOPE_URL`; Querier asserts still to land |
| Missing stand | Skip when stand env unset |

`httptest` is fine for **fast claim/race smoke** only.

## Status

- Done: two Go agents (httptest); stand compose + smoke; toxic timeout chaos smoke.
- Next: Querier live round-trip; Go + PHP-drain; FakeHttp-class HTTP faults; mutation gate.
