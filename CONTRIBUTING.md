# Contributing

Thank you for helping with Tracewick Agent. This repository follows the same
engineering and release discipline as
[`tracewick/tracewick`](https://github.com/tracewick/tracewick), adapted for Go.

## Project

`tracewick-agent` is a long-running Go sidecar that reads Tracewick **spool v2**
files, aggregates them, encodes **pprof**, and pushes to Pyroscope. The PHP
library and `tracewick-drain` remain the spool writers / reference drain.

> **Beta (`0.2.x`).** Capacity evidence (agent as reader) lives in
> [`tracewick/benchmarks`](https://github.com/tracewick/benchmarks). Do not claim
> production / highload readiness. Cosign/SBOM, broader env parity, and socket
> hand-off remain RC / later.

## Path to release

Alpha cut is done (`v0.1.0`). Keep the floor green while advancing RC work:

| Contour | Expectation |
| --- | --- |
| Delivery proof | Stand + Toxiproxy chaos + Querier live + Go ↔ PHP-drain (see `test/e2e/`) |
| Test proof | Coverage gates **and** fuzz / `go tool pprof` / mutation (Gremlins) on core |
| Release machine | release-please + GoReleaser: binaries + `ghcr.io/tracewick/agent`; unsigned until cosign |
| Load truth | Soak/load via [`tracewick/benchmarks`](https://github.com/tracewick/benchmarks), not a weak mini-soak here |

httptest is fine for unit and claim-smoke only — not a substitute for Toxiproxy chaos.

## Commands

```bash
make ci          # fmt-check, golangci-lint, govulncheck, test -race, coverage
make test-race
make lint
make build
make e2e         # cross-process E2E (separate from make ci; CI workflow e2e.yml)
make mutation    # Gremlins MSI gate on core packages (also a Checks step, like Infection)
```

Before every push, run what CI would catch on your change. Prefer `make ci`.
Do not push blind and fix lint on GitHub. Touching spool claim or the agent
loop: also run `make e2e`. Changing delivery / aggregate / spool logic: also
run `make mutation` (thresholds in `.gremlins.yaml`; never lower them to green).

Cross-process E2E details and **Toxiproxy / stand parity** with
[`tracewick/tracewick`](https://github.com/tracewick/tracewick) (not httptest for
network chaos): see [`test/e2e/README.md`](test/e2e/README.md).

## Development stand

Same idea as the main repo stand, slimmed to what the agent needs:

```bash
make stand-up      # Pyroscope + Toxiproxy on 127.0.0.1
make stand-smoke   # /ready + proxy wiring
make e2e           # claim-smoke + stand tests (stand tests skip without env)
make stand-down
```

| From the host | Purpose |
| --- | --- |
| http://127.0.0.1:4040 | Pyroscope (`PYROSCOPE_URL`) |
| http://127.0.0.1:14040 | Pyroscope via Toxiproxy (`PYROSCOPE_TOXIC_URL`) |
| http://127.0.0.1:8474 | Toxiproxy API (`TOXIPROXY_URL`) |

Export those three when running stand-backed E2E locally (CI `e2e.yml` sets them).
UID-compatible deploy examples remain under [`deploy/`](deploy/) — that is ops
shape, not the development stand.

## Rules

- **English** for code, comments, commits, issues, and documentation.
- **Conventional Commits** for commit messages and PR titles.
- Do not skip, disable, or delete a test to make CI green.
- `//nolint` requires a reason on the same line.
- Keep runtime dependencies minimal; discuss new modules in an issue first.
- Fail-open for the **PHP application** is a library concern; this agent must
  still be bounded (memory, claims, HTTP) and never corrupt the spool protocol.

## Branching model (git flow)

- `main` holds released code only; every commit on it is a tagged release.
- `develop` is the integration branch. Feature pull requests target `develop`.
- `feature/<short-name>` branches start from `develop` and merge back into it.
- `release/<version>` branches start from `develop`, receive only stabilization
  fixes, and merge into both `main` (tagged) and `develop`.
- `hotfix/<version>` branches start from `main` and merge into both `main` and
  `develop`.

Never push directly to `main` or `develop`.

## Commits and pull requests

- Keep pull requests focused: one logical change per PR.
- Do not edit `CHANGELOG.md` by hand; release-please generates it.
- Pull requests must keep Codecov project and patch gates green (see
  `.github/codecov.yml`).

## Releasing

Releases follow git flow and are automated with
[release-please](https://github.com/googleapis/release-please) plus
[GoReleaser](https://goreleaser.com/):

1. Branch `release/<version>` from `develop`, stabilize, merge into `main` with a
   **merge commit** (not squash).
2. The `Release` workflow opens or updates a `chore: release <version>` pull
   request into `main`.
3. Merging it tags `v<version>` and runs GoReleaser: linux amd64/arm64
   `tar.gz` + `checksums.txt`, multi-arch `ghcr.io/tracewick/agent`, and an
   **unsigned** footer on the GitHub Release body (no cosign/SBOM yet).
4. The workflow opens a pull request merging `main` back into `develop`.

Tags `v*` are protected by a ruleset. The workflow uses a dedicated GitHub App
(`RELEASE_APP_ID` / `RELEASE_APP_PRIVATE_KEY`, on the tag ruleset bypass list —
same pattern as `tracewick/tracewick`). Do not force-push tags by hand.

Releases stay **unsigned** until cosign/SBOM land. Pin the next tag with
`release-as` in `release-please-config.json` when forcing a version (then remove
it after the release PR merges). Do not invent tags outside that flow.

## License

By contributing, you agree that your contributions are licensed under the
[Apache License 2.0](LICENSE).
