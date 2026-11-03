# Security Policy

## Supported versions

Tracewick Agent has no stable release yet. Until 1.0.0, only the latest release
receives security fixes.

## Reporting a vulnerability

Please **do not** report security vulnerabilities in public issues, discussions,
or pull requests.

Report them privately through
[GitHub Security Advisories](https://github.com/tracewick/agent/security/advisories/new).
Include a description of the issue, the affected versions, and steps to
reproduce it.

We aim to acknowledge a report within 3 business days and to agree on a
disclosure timeline with you. Once a fix is released, we publish an advisory and
credit you unless you prefer otherwise.

## Scope

The agent runs as a sidecar with access to local spool files and (optionally)
credentials for Pyroscope. We treat the following as security issues:

- leaking secrets (for example Basic auth passwords) into logs or metrics labels;
- reading or writing files outside the configured spool / rejected paths;
- remote code execution via crafted spool payloads or HTTP responses;
- disabling TLS verification by default or documenting insecure defaults as safe.
