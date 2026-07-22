# Providers

A provider reads and writes records in an actual DNS backend. Each munpae
instance uses **exactly one**, selected with `MUNPAE_PROVIDER`. To publish to
several backends, run [one instance per provider](usage.md#multiple-instances).

| `MUNPAE_PROVIDER` | Backend |
|---|---|
| `rfc2136` | bind (and other RFC2136 servers) via dynamic UPDATE + TSIG |
| `cloudflare` | Cloudflare DNS |
| `webhook` | any [external-dns webhook provider](webhook-provider.md) |

Under `--dry-run` the real provider is bypassed entirely — changes are logged,
never sent.

All providers enforce the [domain filter](usage.md#scoping-with-a-domain-filter)
and never write outside managed zones. Records sharing a name+type are read as
one multi-target endpoint and the full record set is converged on write.

## `rfc2136`

Programs a zone on a bind (or compatible) server via **dynamic UPDATE with
TSIG**, and reads the current records via **AXFR** (zone transfer). Both are
TSIG-signed over TCP.

```yaml
MUNPAE_PROVIDER: "rfc2136"
MUNPAE_RFC2136_HOST: "192.0.2.53"
MUNPAE_RFC2136_ZONE: "example.com"
MUNPAE_RFC2136_TSIG_KEYNAME: "munpae"
MUNPAE_RFC2136_TSIG_SECRET: "base64-secret=="
# MUNPAE_RFC2136_PORT: "53"
# MUNPAE_RFC2136_TSIG_ALGORITHM: "hmac-sha256"
```

The server must allow the TSIG key to **both update and transfer** the zone —
munpae reads via AXFR and writes via UPDATE:

```
key "munpae" {
    algorithm hmac-sha256;
    secret "base64-secret==";
};
zone "example.com" {
    type master;
    allow-update { key "munpae"; };
    allow-transfer { key "munpae"; };
};
```

## `cloudflare`

Programs records in every zone the API token can access, resolving each record
to its most specific accessible zone. The record listing is paginated, so large
zones are handled correctly.

```yaml
MUNPAE_PROVIDER: "cloudflare"
MUNPAE_CF_API_TOKEN: "…"        # DNS edit permission on the zone(s)
MUNPAE_CF_PROXIED: "true"       # optional global default
```

**Proxying.** `MUNPAE_CF_PROXIED` sets the default orange-cloud state for
`A`/`AAAA`/`CNAME` records (TXT is never proxied). Override it per record with a
container label:

```yaml
labels:
  munpae.dns/hostname: direct.example.com
  munpae.dns/cloudflare-proxied: "false"   # this record bypasses the global default
```

Proxied records are created with Cloudflare's automatic TTL, as the API
requires.

For a Cloudflare Tunnel setup, point records at the tunnel: a proxied `CNAME` to
`<tunnel-id>.cfargotunnel.com` (e.g. via `MUNPAE_DEFAULT_TARGET`).

## `webhook`

An HTTP client that speaks the external-dns webhook protocol, letting any
external-dns webhook provider server act as the backend. See its own page:
[Webhook provider](webhook-provider.md).

## Ownership registry

Independently of the provider, `MUNPAE_REGISTRY` controls ownership tracking:

- `txt` (default) — records munpae creates get a companion ownership TXT; only
  matching-owner records are ever modified or deleted. This is what makes
  automatic management safe. See [Architecture → Ownership](architecture.md#ownership).
- `noop` — no ownership; munpae manages every record in the zone. Only use this
  for a zone dedicated entirely to munpae.

Ownership is keyed on `MUNPAE_OWNER_ID`; give each instance a unique one.
