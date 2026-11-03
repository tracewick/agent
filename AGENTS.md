# AGENTS.md

Instructions for AI coding agents working on this repository. Humans should read
[CONTRIBUTING.md](CONTRIBUTING.md); everything there applies to agents too.

## Project

Tracewick Agent (`github.com/tracewick/agent`, binary `tracewick-agent`) delivers
profiles from a local Tracewick spool to Pyroscope. Canon: the main repo
[`tracewick/tracewick`](https://github.com/tracewick/tracewick) docs
(`docs/spool-format.md`, ADRs 5, 7, 8, 9, 12, 13) and this repository’s CONTRIBUTING.

**Mindset:** README “early development” is a pre-tag label, not an excuse to skip
lifecycle. Path to **alpha (`v0.x`)** = stand + Toxiproxy + Querier live +
Go↔PHP-drain + fuzz/`go tool pprof`/mutation CI + real release cuts; soak via
`tracewick/benchmarks`. Mirror main-repo harnesses; do not replace Toxiproxy with
httptest “backend down”. Every PR should advance that table (see CONTRIBUTING
§ Path to alpha and `test/e2e/README.md`).

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
- Claim rename: `X.spool` → `.X.spool.claimed-<readerId>-<epoch>`.
- Ack only after Pyroscope HTTP 2xx; at-least-once; duplicates possible.
- HTTP delivery: ack only on 2xx; 408/429/5xx → release+retry; 401/403/404 →
  keep spool (pause, do not reject as trash); 413 → shrink batch (or reject a
  single oversized profile); 400 → reject. Do not invent softer auth behaviour.
- Never claim “highload-ready” or production readiness ahead of milestones.
- Do not defer stand/Toxiproxy/mutation/release work with “early-dev is enough”.
- English only. Conventional Commits. Do not hand-edit `CHANGELOG.md`.
- Git flow: branch `feature/*` from `develop`, open PRs into `develop`.
- After opening or pushing a PR, babysit checks (`gh pr checks`, Codecov) until
  green; fix failures yourself. Report only when review-ready or blocked on a
  secret/decision.

## Releasing

See [CONTRIBUTING.md](CONTRIBUTING.md) § Releasing. Do not invent tags outside
release-please. Do not push or merge to `main` unless the maintainer asks.
