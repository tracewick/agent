# Contributing

Thank you for helping with Tracewick Agent. This repository follows the same
engineering and release discipline as
[`tracewick/tracewick`](https://github.com/tracewick/tracewick), adapted for Go.

## Project

`tracewick-agent` is a long-running Go sidecar that reads Tracewick **spool v2**
files, aggregates them, encodes **pprof**, and pushes to Pyroscope. The PHP
library and `tracewick-drain` remain the spool writers / reference drain.

> **Early development.** Do not claim production or highload readiness until a
> tagged release and the maintainers say so.

## Commands

```bash
make ci          # fmt-check, golangci-lint, govulncheck, test -race, coverage
make test-race
make lint
make build
```

Before every push, run what CI would catch on your change. Prefer `make ci`.
Do not push blind and fix lint on GitHub.

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
[GoReleaser](https://goreleaser.com/) (wired fully by M5):

1. Branch `release/<version>` from `develop`, stabilize, merge into `main` with a
   **merge commit** (not squash).
2. The `Release` workflow opens or updates a `chore: release <version>` pull
   request into `main`.
3. Merging it tags `v<version>` and publishes the GitHub Release (binaries /
   checksums / images via GoReleaser when enabled).
4. The workflow opens a pull request merging `main` back into `develop`.

Tags `v*` are protected by a ruleset. The workflow should act as a dedicated
GitHub App on the bypass list (same pattern as `tracewick/tracewick`). Until the
App is configured, do not force-push tags by hand.

## License

By contributing, you agree that your contributions are licensed under the
[Apache License 2.0](LICENSE).
