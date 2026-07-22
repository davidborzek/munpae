# Sources

A source turns container metadata into desired [endpoints](architecture.md#the-endpoint).
Enable one or more with `MUNPAE_SOURCES` (comma-separated); they are merged.

```yaml
MUNPAE_SOURCES: "docker,traefik"
```

All label keys use the `MUNPAE_LABEL_PREFIX` namespace (default `munpae`), so
the examples below assume `munpae.dns/*`.

Any container can opt out of every source with:

```yaml
labels:
  munpae.dns/exclude: "true"
```

## `docker-label`

Explicit by nature — you declare the record directly. Useful for anything
without a Traefik route: a bare TCP service, a manual A/CNAME, etc.

| Label | Required | Purpose |
|---|---|---|
| `munpae.dns/hostname` | yes | Record name(s). Comma-separated for several. |
| `munpae.dns/target` | no | RDATA. Omitted → `MUNPAE_DEFAULT_TARGET`. |
| `munpae.dns/ttl` | no | TTL in seconds. |
| `munpae.dns/cloudflare-proxied` | no | Per-record Cloudflare proxied override (`true`/`false`). See [cloudflare](providers.md#cloudflare). |
| `munpae.dns/exclude` | no | `true` skips the container. |

```yaml
services:
  db:
    image: postgres
    labels:
      munpae.dns/hostname: "db.example.com,postgres.example.com"
      munpae.dns/target: 192.0.2.2
      munpae.dns/ttl: "300"
```

The record type is inferred from the target: an IP → `A`/`AAAA`, a hostname →
`CNAME`.

## `traefik`

Derives hostnames from a container's Traefik router labels — no per-app DNS
labels required. munpae reads the router rule and extracts literal hostnames
from `Host(...)` / `HostSNI(...)` matchers:

```
traefik.http.routers.<name>.rule = Host(`app.example.com`) [&& ...]
```

`HostRegexp` and non-host matchers are ignored (they yield no literal name).

This is effectively opt-in already: with Traefik's
`providers.docker.exposedByDefault=false`, only containers that set
`traefik.enable=true` are routed, so publishing DNS for routed hosts is
expected.

### Entrypoint → target anchors

A record needs a target. In the Traefik source, the target is chosen by the
router's **entrypoint**, and the entrypoint→target map is declared as labels on
the Traefik container itself (the component that owns that topology fact):

```yaml
# on the traefik container
labels:
  munpae.dns/traefik.entrypoint.internal-secure.target: internal.example.com
  munpae.dns/traefik.entrypoint.external-secure.target: external.example.com
```

A router on `internal-secure` then resolves to `internal.example.com`, one on
`external-secure` to `external.example.com`. A router listing both entrypoints
is published for both — split-horizon, automatically.

### Which entrypoints this instance publishes

`MUNPAE_TRAEFIK_ENTRYPOINTS` filters the entrypoints a given instance cares
about (unset = all). In a two-instance setup, the internal (bind) instance can
publish everything while the external (Cloudflare) instance sets
`MUNPAE_TRAEFIK_ENTRYPOINTS=external-secure` and so omits internal names:

```yaml
# external instance
MUNPAE_TRAEFIK_ENTRYPOINTS: "external-secure"
```

### Target precedence

For each derived hostname the target is chosen in this order:

1. `munpae.dns/target` on the routed container (per-app override),
2. the entrypoint→target anchor map,
3. `MUNPAE_DEFAULT_TARGET` (core fallback).

If none yields a target, the hostname is skipped. Record type is inferred from
the resolved target.
