# Cross-process E2E (M4+)

These tests exercise real `tracewick-agent` processes against a shared spool
directory and an HTTP stub for Pyroscope. They are **not** part of `make ci` /
the `Checks` job (`go test -race` does not replace this class).

```bash
make e2e
# or
go test -tags=e2e -count=1 -timeout=5m ./test/e2e/
```

CI: `.github/workflows/e2e.yml`.

Later M4 items (PHP-drain coexistence, chaos, live Pyroscope, soak) grow this
package; keep build tag `e2e` so unit CI stays fast.
