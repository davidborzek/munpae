# Changelog

All notable changes to this project are documented here. The format is based on
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and the project
adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- Event-driven reconcile loop over the Docker API with debouncing and a periodic
  resync.
- Sources: `docker-label` and `traefik` (Host/HostSNI rule parsing, entrypoint
  target anchors).
- Providers: `rfc2136` (bind, dynamic UPDATE + AXFR with TSIG), `cloudflare`
  (paginated, per-record proxied override), and `webhook` (external-dns webhook
  protocol client).
- `txt` ownership registry (and a `noop` registry) so munpae only touches
  records it created.
- `upsert-only` and `sync` reconcile policies.
- Prometheus `/metrics` and `/healthz` endpoints.
- CLI with `--dry-run`, `--version`, and `--help`.
- Multi-arch container image (`linux/amd64`, `linux/arm64`).
