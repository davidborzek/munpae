# Usage

munpae is configured through `MUNPAE_*` environment variables (see the
[configuration reference](configuration.md)). A running instance always has:

1. one or more **[sources](sources.md)** (`MUNPAE_SOURCES`) that derive desired
   records from container metadata, and
2. exactly one **[provider](providers.md)** (`MUNPAE_PROVIDER`) that writes them
   to a DNS backend.

A [registry](providers.md#ownership-registry) (default `txt`) sits between them
so munpae only ever touches records it created.

## Declaring records

The simplest source is `docker` — declare a record with labels on any
container:

```yaml
services:
  db:
    image: postgres
    labels:
      munpae.dns/hostname: db.example.com
      munpae.dns/target: 192.0.2.2      # omit to use MUNPAE_DEFAULT_TARGET
```

With the `traefik` source enabled, hostnames are instead derived from Traefik
router rules — no per-app DNS labels needed. See [Sources](sources.md) for both.

## The reconcile flow

On startup and then on every Docker event (debounced) plus a periodic resync,
munpae:

1. collects desired records from all sources,
2. reads the records it currently owns from the provider,
3. diffs them, and
4. applies the difference (creates/updates, and deletes if
   `MUNPAE_POLICY=sync`).

A provider error is logged and the previous state stands — the next resync
repairs any drift. See [Architecture](architecture.md) for details.

## Dry run

Set `MUNPAE_DRY_RUN=true` — or pass the `--dry-run` flag — to log the plan
without touching DNS. The output shows exactly what would change:

```
level=INFO msg=applying create=1 update=0 delete=0
level=INFO msg="dry-run CREATE" name=db.example.com type=A targets=[192.0.2.2] ttl=0
level=INFO msg="dry-run CREATE" name=munpae.a-db.example.com type=TXT targets="[heritage=munpae,munpae/owner=munpae]"
```

The second line is the [ownership TXT](architecture.md#ownership) munpae writes
alongside the record.

## Delete vs. keep: policy

- `upsert-only` (default) — create and update records, but **never delete**.
  Safe default: removing a container leaves its record in place.
- `sync` — additionally delete owned records whose container is gone.

Only records munpae owns are ever deleted; foreign records are untouched
regardless of policy.

## Scoping with a domain filter

`MUNPAE_DOMAIN_FILTER` restricts munpae to names under the given zones. Anything
outside is ignored on both read and write:

```yaml
MUNPAE_DOMAIN_FILTER: "example.com,internal.example.com"
```

## Multiple instances

munpae is **single-provider per instance**. To publish to more than one backend
— e.g. internal names to bind and public names to Cloudflare — run **one
instance per provider**. This mirrors external-dns' own model; nothing in munpae
is instance-aware, it is purely configuration.

| Instance | `MUNPAE_PROVIDER` | `MUNPAE_OWNER_ID` | `MUNPAE_DOMAIN_FILTER` | `MUNPAE_DEFAULT_TARGET` |
|---|---|---|---|---|
| internal | `rfc2136` | `munpae-bind` | internal zone(s) | LAN IP |
| external | `cloudflare` | `munpae-cf` | public zone(s) | tunnel / public target |

Two rules make this safe:

- **Give each instance a distinct `MUNPAE_OWNER_ID`.** The TXT registry keys
  ownership on it; two instances sharing an owner-id would fight over each
  other's records.
- **Scope each instance** with `MUNPAE_DOMAIN_FILTER` so they don't both claim
  the same names — unless split-horizon is intended.

**Split-horizon** (same name, different target internally vs. externally) works
naturally: both instances publish `app.example.com`, each with its own target,
into different DNS servers (bind vs. Cloudflare). They never collide, and
distinct owner-ids keep their bookkeeping separate.

## Observability

Unless `MUNPAE_METRICS_ADDR` is blank, munpae serves:

- `GET /healthz` — liveness (`200 ok`).
- `GET /metrics` — Prometheus metrics: `munpae_ready`,
  `munpae_reconciles_total{result}`, `munpae_reconcile_duration_seconds`,
  `munpae_last_reconcile_timestamp_seconds`,
  `munpae_last_reconcile_success_timestamp_seconds`, `munpae_managed_records`,
  `munpae_changes_total{action}`, `munpae_watch_restarts_total`,
  `munpae_build_info`.

Ready-made Grafana and Prometheus artifacts ship in
[`dashboards/`](../dashboards): a dashboard (`grafana-dashboard.json`) and alert
rules (`prometheus-alerts.yaml`) covering readiness, stale/failing reconciles,
and watcher flapping.
