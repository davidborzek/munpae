<div align="center">

# munpae

**An `external-dns` for plain Docker / Compose — no Kubernetes.**

[![ci](https://github.com/davidborzek/munpae/actions/workflows/ci.yaml/badge.svg)](https://github.com/davidborzek/munpae/actions/workflows/ci.yaml)
[![license](https://img.shields.io/github/license/davidborzek/munpae)](LICENSE)
[![release](https://img.shields.io/github/v/release/davidborzek/munpae)](https://github.com/davidborzek/munpae/releases)

</div>

munpae watches a single Docker host, derives the desired DNS records from
container metadata (its own labels + Traefik router rules), and reconciles them
into a DNS backend (bind via RFC2136, Cloudflare, or any external-dns webhook
provider) — safely, only touching records it owns.

*munpae* (문패) is Korean for the nameplate on a gate: the small plate that says
who lives at an address. A DNS record is exactly that — a name pointing at an
address.

> [!WARNING]
> **Early-stage software.** munpae is fresh and pre-1.0 — expect rough edges,
> and review the plan (`--dry-run`) before you let it write to your DNS. The
> configuration and label surface is still settling and may change, though large
> shifts are unlikely since it mirrors the established external-dns model.

## Features

- **Docker-native sources** — publish records from explicit container labels or
  from Traefik `Host(...)` router rules.
- **Multiple providers** — `rfc2136` (bind + TSIG), `cloudflare`, and a
  `webhook` client that speaks the [external-dns webhook protocol](docs/webhook-provider.md)
  (so any external-dns webhook provider works as a backend).
- **Ownership-safe** — a TXT registry marks records munpae created; foreign
  records are never modified or deleted.
- **Event-driven** — reacts to Docker start/die/destroy events with debouncing,
  plus a periodic resync safety net.
- **Observable** — `/metrics` (Prometheus) and `/healthz`.
- **`--dry-run`** — log the plan, change nothing.

## Quick start

Run against a bind server, seeing what it *would* do first:

```yaml
# compose.yaml
services:
  munpae:
    image: munpae:local           # or build from this repo (see docs/installation.md)
    environment:
      MUNPAE_DRY_RUN: "true"
      MUNPAE_SOURCES: "docker"
      MUNPAE_PROVIDER: "rfc2136"
      MUNPAE_DOMAIN_FILTER: "example.com"
      MUNPAE_RFC2136_HOST: "192.0.2.53"
      MUNPAE_RFC2136_ZONE: "example.com"
      MUNPAE_RFC2136_TSIG_KEYNAME: "munpae"
      MUNPAE_RFC2136_TSIG_SECRET: "base64-secret=="
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock:ro
```

Then declare a record on any container:

```yaml
services:
  db:
    image: postgres
    labels:
      munpae.dns/hostname: db.example.com
      munpae.dns/target: 192.0.2.2
```

Drop `MUNPAE_DRY_RUN` to apply for real. See [Usage](docs/usage.md) for the full
flow.

## Documentation

- [Installation](docs/installation.md) — image, Compose, building from source.
- [Usage](docs/usage.md) — configuring munpae, examples, multi-instance setups.
- [Configuration reference](docs/configuration.md) — every `MUNPAE_*` variable.
- [Architecture](docs/architecture.md) — the reconcile loop and ownership model.
- [Sources](docs/sources.md) — `docker-label` and `traefik`.
- [Providers](docs/providers.md) — `rfc2136`, `cloudflare`, `webhook`, registry.
- [Webhook provider](docs/webhook-provider.md) — the external-dns webhook protocol.

## Scope

munpae targets a **single Docker host** and is **single-provider per instance**
by design. To publish to more than one backend (e.g. bind *and* Cloudflare), run
one instance per provider — see [Usage → Multiple instances](docs/usage.md#multiple-instances).
It programs existing authoritative DNS; it is not itself a DNS server, and does
no health-checking or failover of targets.

## Credits

munpae's design is modelled on
[external-dns](https://github.com/kubernetes-sigs/external-dns): the Endpoint /
Source / Provider / Registry / Plan architecture, the TXT ownership model, and
the [webhook provider protocol](docs/webhook-provider.md) are all mirrored from
it — adapted from Kubernetes to plain Docker. Thanks to its maintainers and
community.
