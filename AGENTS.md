# AGENTS.md

Instructions for AI coding agents working on this repository. Humans should read
[CONTRIBUTING.md](CONTRIBUTING.md); everything there applies to agents too.

## Project

Tracewick Agent (`github.com/tracewick/agent`, binary `tracewick-agent`) delivers
profiles from a local Tracewick spool to Pyroscope. Canon: the main repo
[`tracewick/tracewick`](https://github.com/tracewick/tracewick) — do not fork
wire semantics here. Humans: [CONTRIBUTING.md](CONTRIBUTING.md) and [README.md](README.md).

| Doc | URL |
| --- | --- |
| Spool format | https://github.com/tracewick/tracewick/blob/main/docs/spool-format.md |
| ADR 4 pprof | https://github.com/tracewick/tracewick/blob/main/docs/adr/0004-pprof-as-primary-format.md |
| ADR 5 fail-open | https://github.com/tracewick/tracewick/blob/main/docs/adr/0005-fail-open-policy.md |
| ADR 7 spool | https://github.com/tracewick/tracewick/blob/main/docs/adr/0007-language-neutral-spool-format.md |
| ADR 8 HTTP | https://github.com/tracewick/tracewick/blob/main/docs/adr/0008-own-http-transport.md |
| ADR 9 hand-off | https://github.com/tracewick/tracewick/blob/main/docs/adr/0009-agent-hand-off.md |
| ADR 12 aggregate | https://github.com/tracewick/tracewick/blob/main/docs/adr/0012-aggregate-before-delivery.md |
| ADR 13 compact-stacks | https://github.com/tracewick/tracewick/blob/main/docs/adr/0013-spool-v2-compact-payload.md |

**Mindset:** README **beta** is not production readiness. Keep the alpha floor
green (stand / Toxiproxy / Querier / mutation+fuzz / release cuts); soak via
`tracewick/benchmarks`. Mirror main-repo harnesses; do not replace Toxiproxy with
httptest “backend down”.

## Commands

```bash
make ci          # required local gate before push (includes pprof-check)
make test-race
make lint
make build
make stand-up    # Pyroscope + Toxiproxy
make e2e         # cross-process E2E (not part of make ci; needs stand for toxic tests)
make mutation    # Gremlins MSI on core (Checks step; .gremlins.yaml)
make stand-down
```

**Before every commit**, run at least `make fmt-check`, `make lint`, and
`make test-race` for touched packages. Prefer `make ci`.

## Layout

- `cmd/tracewick-agent/` — binary entrypoint
- `internal/spool/` — spool file parse (SpoolFile) + directory claim/ack/release
- `internal/agent/` — `Drainer` (one pass) + `Agent` (long-running loop)
- `internal/delivery/`, `internal/aggregate/`, `internal/pprof/`, `internal/pyroscope/`,
  `internal/config/`, `internal/metrics/`
- `testdata/spool/v2/` — fixtures compatible with the PHP library
- `testdata/pprof/` — golden pprof (`make pprof-check` / CI)
- `compose.yaml`, `docker/` — development stand (Pyroscope + Toxiproxy)
- `deploy/` — UID-compatible Compose/Kubernetes examples + Dockerfile
- `test/e2e/` — cross-process E2E (`make e2e`, build tag `e2e`; CI `e2e.yml`);
  network chaos via Toxiproxy/stand like `tracewick/tracewick`, not httptest alone
  (see `test/e2e/README.md`)

## Rules

- Spool-first; unix socket is v1.1+ with length-prefix framing only.
- One agent process per spool directory (same UID/GID as PHP writer). Claim
  rename is race-safe across readers, but each process has its own cardinality
  gate and may re-deliver after `StaleClaim`.
- Aggregation window groups files claimed in the current pass only; it does not
  wait for the window to close. Same logical bucket across two passes → two Push
  (at-least-once).
- Decode/merge caps default to PHP `BoundedProfileBuilder` (4096/16384/32768);
  if PHP raises those, raise matching agent flags (`--max-stacks` /
  `--max-frames` / `--max-strings`) or valid profiles are rejected as over budget.
- Claim rename: `X.spool` → `.X.spool.claimed-<readerId>-<epoch>`.
- Ack only after Pyroscope HTTP 2xx; at-least-once; duplicates possible. Do not
  count delivered when Acknowledge/Remove fails — leave claim held for release.
- HTTP delivery: ack only on 2xx; 408/429/5xx → release+retry; 401/403/404 →
  keep spool (pause, do not reject as trash); 413 → shrink batch (or reject a
  single oversized profile); 400 → reject. Do not invent softer auth behaviour.
- Never claim “highload-ready” or production readiness ahead of milestones.
- Do not defer stand/Toxiproxy/mutation/release work with “beta is enough”.
- English only. Conventional Commits. Do not hand-edit `CHANGELOG.md`.
- Do not invent agent-local ADR numbers that shadow main-repo ADRs; link them.
- Git flow: branch `feature/*` from `develop`, open PRs into `develop`.
- After opening or pushing a PR, babysit checks (`gh pr checks`, Codecov) until
  green; fix failures yourself. Report only when review-ready or blocked on a
  secret/decision.

## Releasing

See [CONTRIBUTING.md](CONTRIBUTING.md) § Releasing. Do not invent tags outside
release-please. Do not push or merge to `main` unless the maintainer asks.
