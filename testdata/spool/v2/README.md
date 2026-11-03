# Spool format fixtures (version 2)

The only supported on-disk spool format: `version = 2`, `format = compact-stacks`.
Regenerate valid files with `TRACEWICK_UPDATE_GOLDEN=1 composer test:unit` (see `SpoolSinkTest`)
and invalid/corruption cases via `SpoolFixturesTest`.

| File                     | Contents                                                                 |
|--------------------------|--------------------------------------------------------------------------|
| `web-request.spool`      | Valid wall-time profile, label `service=blog`                            |
| `edge-cases.spool`       | Valid CPU profile, empty labels                                          |
| `unknown-header-key.spool` | Valid file plus ignored header key `x_future`                          |
| `*.spool` (others)       | Invalid / corrupt; readers MUST reject and MUST NOT deliver             |

A reader must deliver the three valid fixtures and reject the rest.
