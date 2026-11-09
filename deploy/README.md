# Deploy examples (UID-compatible spool)

PHP writers create the spool directory mode `0700` and files mode `0600`. The
agent must run as the **same UID (and typically GID)** as the PHP-FPM / worker
process that writes the spool. A different UID cannot open the files; sharing
only an `emptyDir` volume is not enough.

Supported v1 pattern (this directory):

1. Pick one numeric UID/GID (examples use `33` / `www-data`).
2. Run PHP and `tracewick-agent` as that user.
3. Mount one writable spool path into both.

Alternatives (not shipped here): ACL / group-readable spool would require a
change in `tracewick/tracewick` writers — open an issue in the main repo first.

## Docker Compose

```bash
# from repository root
docker compose -f deploy/docker-compose.yml up --build
```

See [`docker-compose.yml`](docker-compose.yml). Override `SPOOL_UID` /
`SPOOL_GID` to match your PHP image.

## Kubernetes

See [`kubernetes/agent.yaml`](kubernetes/agent.yaml). Set `runAsUser`,
`runAsGroup`, and `fsGroup` to the same IDs as the application container that
writes `/var/spool/tracewick`.

## Images

| Source | Image |
| --- | --- |
| Local | `make docker-build` → `tracewick-agent:local` |
| Release | `ghcr.io/tracewick/agent:<version>` (multi-arch amd64/arm64; e.g. `0.2.0`) |

```bash
make docker-build
docker run --rm --user "$(id -u):$(id -g)" \
  -v /var/spool/tracewick:/var/spool/tracewick \
  -e TRACEWICK_SPOOL_DIR=/var/spool/tracewick \
  -e TRACEWICK_PYROSCOPE_URL=http://host.docker.internal:4040 \
  tracewick-agent:local run --once
```

Published tags are **unsigned** until cosign lands; pin by digest when possible.
