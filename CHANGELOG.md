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

### Changed

- Container label keys now use a dot between `dns` and the field
  (`munpae.dns.hostname`), conforming to the shared label convention
  (`LABEL-SPEC.md`).

### Deprecated

- The slash label form (`munpae.dns/<field>`, including
  `munpae.dns/traefik.entrypoint.<ep>.target`) in favour of the dot form. It is
  still honoured — the dot form wins when both are set — but logs a warning and
  increments `munpae_deprecated_label_total{label}`. It will be removed in a
  future minor release.

### Fixed

- Cloudflare: TXT record content is now unquoted on read. Cloudflare stores TXT
  content wrapped in double quotes (splitting long values into 255-byte quoted
  chunks); reading it back verbatim made every diff see `"x"` != `x`.
- Ownership TXT records are reconciled against live provider state instead of
  being mirrored blindly onto every create/delete. A managed record whose
  ownership TXT still exists (e.g. the record was deleted out from under munpae)
  is now recreated without re-creating the surviving TXT, fixing a reconcile
  loop that repeatedly failed with `An identical record already exists (81058)`.
