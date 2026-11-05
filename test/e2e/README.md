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

README marks **alpha (`0.1.x`)**, not production readiness. Keep advancing
release cuts (GoReleaser), honest unsigned notes until cosign, and soak via
[`tracewick/benchmarks`](https://github.com/tracewick/benchmarks). E2E here
covers stand, Toxiproxy chaos, Querier live, and Go + PHP-drain coexistence.

## Canon (parity with `tracewick/tracewick`)

| Concern | Approach |
| --- | --- |
| Claim coexistence | Real processes, one spool dir (two Go agents; Go + `tracewick-drain`) |
| Pyroscope faults | **Toxiproxy** (`timeout` / `latency` / `reset_peer` / …) via `PYROSCOPE_TOXIC_URL` |
| Healthy push | `PYROSCOPE_URL`; Querier asserts still to land |
| UID mismatch | Docker named volumes (no host binds): spool `33:33` `0700`/`0600`; agent `1000` cannot claim, `33` delivers. Needs working `docker run` (CI ubuntu; local skip if Desktop wedged) |
| Missing stand | Skip when stand env unset |

`httptest` is fine for **fast claim/race smoke** only.

## Status

- Done: two Go agents (httptest); stand compose + smoke;
  Toxiproxy suite (healthy / latency / timeout / reset_peer / limit_data) +
  process kill -9 → stale reclaim; Go + PHP `tracewick-drain` coexistence;
  live Querier at check-profiles depth (`LabelValues`, `ProfileTypes`
  wall:nanoseconds, `SelectMergeStacktraces` + fixture frames, no raw URLs);
  401 pause keeps spool; fixture reject+deliver; long-run SIGTERM after drain.
  `make e2e` uses `-v` so lines prefixed `e2e:` show progress in CI.
- Done: UID-mismatch (Docker volume, wrong UID vs matching `33`) —
  `TestUIDMismatchCannotClaimMatchingCan` (skips without docker).
- Next: benchmarks soak. Mutation MSI gate is `make mutation` (Checks step, not
  e2e). FakeHttp-class faults live under `internal/testhttp` +
  `internal/pyroscope` (unit).

## PHP drain for local E2E

```bash
make php-drain          # composer → .cache/php-drain/vendor/bin/tracewick-drain
make e2e                # exports TRACEWICK_DRAIN_BIN when that binary exists
```

Or point at a main-repo checkout: `export TRACEWICK_DRAIN_BIN=../tracewick/bin/tracewick-drain`.
Without a drain binary the Go+PHP test **skips**; other e2e cases still run.
