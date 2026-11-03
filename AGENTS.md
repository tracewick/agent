# AGENTS.md

Instructions for AI coding agents working on this repository. Humans should read
[CONTRIBUTING.md](CONTRIBUTING.md); everything there applies to agents too.

## Project

Tracewick Agent (`github.com/tracewick/agent`, binary `tracewick-agent`) delivers
profiles from a local Tracewick spool to Pyroscope. Canon: Docmost page
`go-agent` and the PHP library docs (`spool-format.md`, ADRs 5, 7, 9, 12, 13).

## Commands

```bash
make ci          # required local gate before push
make test-race
make lint
make build
```

**Before every commit**, run at least `make fmt-check`, `make lint`, and
`make test-race` for touched packages. Prefer `make ci`.

## Layout

- `cmd/tracewick-agent/` — binary entrypoint
- `internal/spool/` — spool file parse (SpoolFile) + directory claim/ack/release
- `internal/aggregate/`, `internal/pprof/`, `internal/pyroscope/`, `internal/config/`,
  `internal/metrics/` — added as milestones progress
- `testdata/spool/v2/` — fixtures compatible with the PHP library
- `test/e2e/` — cross-process E2E (M4+)

## Rules

- Spool-first; unix socket is v1.1+ with length-prefix framing only.
- Claim rename: `X.spool` → `.X.spool.claimed-<readerId>-<epoch>`.
- Ack only after Pyroscope HTTP 2xx; at-least-once; duplicates possible.
- Do not invent softer HTTP/auth behaviour than Docmost §2.8.
- Never claim “highload-ready” or production readiness ahead of milestones.
- English only. Conventional Commits. Do not hand-edit `CHANGELOG.md`.
- Git flow: branch `feature/*` from `develop`, open PRs into `develop`.
- After opening or pushing a PR, babysit checks (`gh pr checks`, Codecov) until
  green; fix failures yourself. Report only when review-ready or blocked on a
  secret/decision.

## Releasing

See [CONTRIBUTING.md](CONTRIBUTING.md) § Releasing. Do not invent tags outside
release-please. Do not push or merge to `main` unless the maintainer asks.
