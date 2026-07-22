# Changelog

All notable changes to this project are documented here. The format is based on
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and the project
adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## 0.1.0 (2026-07-22)


### Features

* publish DNS for Docker workloads from labels and Traefik ([ba886cf](https://github.com/davidborzek/munpae/commit/ba886cf925859a1f9139939e8b8851514332a1b5))


### Miscellaneous Chores

* cut the first release as 0.1.0 ([94d53c2](https://github.com/davidborzek/munpae/commit/94d53c285b94b0c09bf24b9e797fa31a32c6db39))

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
