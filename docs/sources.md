# Sources

A source turns container metadata into desired [endpoints](architecture.md#the-endpoint).
Enable one or more with `MUNPAE_SOURCES` (comma-separated); they are merged.

```yaml
MUNPAE_SOURCES: "docker,traefik"
```

All label keys use the `MUNPAE_LABEL_PREFIX` namespace (default `munpae`), so
the examples below assume `munpae.dns.*`.

> [!NOTE]
> The separator between `dns` and the field is a dot (`munpae.dns.hostname`). The
> older slash form (`munpae.dns/hostname`) is still read for backwards
> compatibility but is **deprecated** — it logs a warning and increments
> `munpae_deprecated_label_total`, and will be removed in a future minor release.

Any container can opt out of every source with:

```yaml
labels:
  munpae.dns.exclude: "true"
```

## Grace & restarts

When a container restarts — `docker restart`, or a `docker compose up` that
recreates it after an image update — the container is briefly absent from the
running set. Without a safeguard, munpae's `sync` policy would delete the
record at that moment (it's in the owned set but not in the desired set) and
re-create it when the container comes back: a DNS flap.

`MUNPAE_GRACE_PERIOD` (default `5m`) prevents this. A record whose container
vanishes is kept — and still published — for the grace window, and only
deleted once the container has been absent for longer than that. A restart or
recreate that finishes within the window therefore never causes a deletion.
Set it to `0` to disable (immediate deletion, the pre-grace behaviour).

The grace window also covers a munpae restart that happens *during* a
container's recreate: on startup munpae seeds the grace set from the records it
already owns, so those records get a full grace window before any deletion is
considered, rather than being dropped the moment munpae comes back up.

Separately, `MUNPAE_INCLUDE_STOPPED` (default `false`) treats containers that
still exist but are stopped (`docker stop`, which emits a `die` event without a
`destroy`) as still desired. When enabled, a stopped container keeps its record
until the container is actually removed (`docker rm`). When disabled (default),
a stopped container is treated like a vanished one and its record is deleted
after the grace window, unless it comes back.

## `docker-label`

Explicit by nature — you declare the record directly. Useful for anything
without a Traefik route: a bare TCP service, a manual A/CNAME, etc.

| Label | Required | Purpose |
|---|---|---|
| `munpae.dns.hostname` | yes | Record name(s). Comma-separated for several. |
| `munpae.dns.target` | no | RDATA. Omitted → `MUNPAE_DEFAULT_TARGET`. |
| `munpae.dns.ttl` | no | TTL in seconds. |
| `munpae.dns.cloudflare-proxied` | no | Per-record Cloudflare proxied override (`true`/`false`). See [cloudflare](providers.md#cloudflare). |
| `munpae.dns.exclude` | no | `true` skips the container. |

```yaml
services:
  db:
    image: postgres
    labels:
      munpae.dns.hostname: "db.example.com,postgres.example.com"
      munpae.dns.target: 192.0.2.2
      munpae.dns.ttl: "300"
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
  munpae.dns.traefik.entrypoint.internal-secure.target:   internal.example.com
  munpae.dns.traefik.entrypoint.internal-secure.priority: "100"   # optional, default 0
  munpae.dns.traefik.entrypoint.external-secure.target:   external.example.com
  munpae.dns.traefik.entrypoint.external-secure.priority: "50"
```

A router on `internal-secure` resolves to `internal.example.com`, one on
`external-secure` to `external.example.com`.

When a single hostname maps to **several** entrypoints — one router listing
both, or two routers with the same `Host(...)` (e.g. a public-path router plus
an admin catchall) — an instance that publishes both must pick one target. The
highest **priority** wins (`.priority`, default 0): the internal (bind) instance
typically gives its internal entrypoint a higher priority, so the LAN horizon
resolves to the internal anchor while the external instance still publishes the
external one. Ties are handled under [Target precedence](#target-precedence).

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

1. `munpae.dns.target` on the routed container (per-app override),
2. among the entrypoints this instance publishes, the entrypoint→target anchor
   with the **highest `.priority`** (default 0),
3. `MUNPAE_DEFAULT_TARGET` (core fallback).

If a hostname still resolves to **more than one distinct target** at the top of
this order (e.g. two entrypoints at the same priority with different anchors),
it is a **conflict**: munpae does not guess — it skips the hostname, logs a
warning, and increments `munpae_source_conflicts_total{host}`. Resolve it by
giving one entrypoint a higher `.priority`, setting a per-app `munpae.dns.target`,
or excluding a router (below).

If none yields a target, the hostname is skipped. Record type is inferred from
the resolved target.

### Excluding a router

A single router can be kept out of DNS without excluding the whole container:

```yaml
labels:
  munpae.dns.traefik.router.<name>.exclude: "true"
```

Useful when one container has several routers and only some should produce a
record — e.g. publish the internal admin router but let a separate DNS instance
own the public one.
