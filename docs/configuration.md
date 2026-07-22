# Configuration reference

munpae is configured entirely through `MUNPAE_*` environment variables — ideal
for a Compose file. Invalid values (e.g. a malformed duration) fail fast at
startup with a clear error rather than silently falling back.

- **Lists** (`MUNPAE_SOURCES`, `MUNPAE_DOMAIN_FILTER`,
  `MUNPAE_TRAEFIK_ENTRYPOINTS`) are comma-separated; surrounding spaces are
  trimmed and empty entries dropped (`a, b ,, c` → `a b c`).
- **Durations** use Go syntax (`60s`, `1m`, `500ms`).
- **Booleans** accept `true`/`false`.

## Core

| Variable | Default | Purpose |
|---|---|---|
| `MUNPAE_SOURCES` | `docker` | Enabled [sources](sources.md), comma-separated: `docker`, `traefik`. |
| `MUNPAE_PROVIDER` | `rfc2136` | DNS backend: `rfc2136`, `cloudflare`, `webhook`. |
| `MUNPAE_REGISTRY` | `txt` | Ownership tracking: `txt` (safe) or `noop` (manage everything). |
| `MUNPAE_OWNER_ID` | `munpae` | Ownership id written into TXT records. **Must be unique per instance.** |
| `MUNPAE_TXT_PREFIX` | `munpae.` | Prefix for ownership TXT record names. Keep stable once set. |
| `MUNPAE_DOMAIN_FILTER` | _(none)_ | Only manage names under these zones, e.g. `example.com`. Empty = all. |
| `MUNPAE_DEFAULT_TARGET` | _(none)_ | Fallback RDATA target when a source yields none (e.g. a LAN IP or tunnel host). |
| `MUNPAE_POLICY` | `upsert-only` | `upsert-only` never deletes; `sync` also deletes stale owned records. |
| `MUNPAE_LABEL_PREFIX` | `munpae` | Docker label namespace → `<prefix>.dns/*`. |
| `MUNPAE_RESYNC_INTERVAL` | `60s` | Periodic full resync (clamped to > 0). |
| `MUNPAE_DEBOUNCE_DELAY` | `1s` | Event coalescing window (clamped to > 0). |
| `MUNPAE_METRICS_ADDR` | `:9333` | Listen address for `/metrics` + `/healthz`; blank disables the server. |
| `MUNPAE_LOG_LEVEL` | `info` | `debug`, `info`, `warn`, `error`. |
| `MUNPAE_DRY_RUN` | `false` | Log the plan, change nothing. |

## traefik source

| Variable | Default | Purpose |
|---|---|---|
| `MUNPAE_TRAEFIK_ENTRYPOINTS` | _(all)_ | Publish only routers on these Traefik entrypoints. Unset = all. See [Sources](sources.md#traefik). |

## rfc2136 provider

Used when `MUNPAE_PROVIDER=rfc2136`.

| Variable | Default | Purpose |
|---|---|---|
| `MUNPAE_RFC2136_HOST` | _(required)_ | DNS server address. |
| `MUNPAE_RFC2136_PORT` | `53` | DNS server port. |
| `MUNPAE_RFC2136_ZONE` | _(required)_ | Managed zone, e.g. `example.com`. |
| `MUNPAE_RFC2136_TSIG_KEYNAME` | _(required)_ | TSIG key name. |
| `MUNPAE_RFC2136_TSIG_SECRET` | _(required)_ | Base64 TSIG secret. |
| `MUNPAE_RFC2136_TSIG_ALGORITHM` | `hmac-sha256` | `hmac-sha1`/`-sha224`/`-sha256`/`-sha384`/`-sha512`. |

## cloudflare provider

Used when `MUNPAE_PROVIDER=cloudflare`.

| Variable | Default | Purpose |
|---|---|---|
| `MUNPAE_CF_API_TOKEN` | _(required)_ | Cloudflare API token with DNS edit rights on the zone(s). |
| `MUNPAE_CF_PROXIED` | `false` | Proxy A/AAAA/CNAME through Cloudflare by default. Overridable per record via the `munpae.dns/cloudflare-proxied` label. |

## webhook provider

Used when `MUNPAE_PROVIDER=webhook`. See [Webhook provider](webhook-provider.md).

| Variable | Default | Purpose |
|---|---|---|
| `MUNPAE_WEBHOOK_URL` | _(required)_ | Base URL of the external-dns webhook provider server. |
| `MUNPAE_WEBHOOK_TIMEOUT` | `10s` | Per-request HTTP timeout. |
