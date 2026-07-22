# Webhook provider

The `webhook` provider lets munpae drive **any DNS backend that implements the
[external-dns webhook protocol](https://kubernetes-sigs.github.io/external-dns/latest/tutorials/webhook-provider/)**.
Instead of building in code for each backend, munpae acts as an HTTP client and
delegates record operations to an external server (typically run as a sidecar).

Because munpae speaks the same protocol external-dns does, the existing
ecosystem of external-dns webhook provider servers works unchanged.

```yaml
MUNPAE_PROVIDER: "webhook"
MUNPAE_WEBHOOK_URL: "http://127.0.0.1:8888"
# MUNPAE_WEBHOOK_TIMEOUT: "10s"
```

## Protocol

munpae is the client; the configured server implements the provider. All
request/response bodies use the media type
`application/external.dns.webhook+json;version=1`.

| Step | Method + route | Purpose |
|---|---|---|
| Negotiation | `GET /` | Handshake; munpae verifies the `Content-Type`. Done once at startup. |
| Records | `GET /records` | Server returns the current records. |
| Adjust | `POST /adjustendpoints` | Server normalizes the desired records before planning. Optional — a `404` is treated as identity. |
| Apply | `POST /records` | munpae sends the changes (`Create` / `UpdateOld` / `UpdateNew` / `Delete`). |

Serialization matches external-dns' `endpoint.Endpoint` and `plan.Changes` Go
types byte-for-byte, so a compliant server needs no munpae-specific handling.

On each reconcile the sequence is:

```
GET / (once at startup)  →  POST /adjustendpoints  →  GET /records  →  POST /records
```

Only `2xx` responses are treated as success; `POST /records` must return `204`.

## Running a server

Point `MUNPAE_WEBHOOK_URL` at the server's base URL. The recommended setup is a
sidecar listening on `localhost` in the same host as munpae, exactly as
external-dns recommends:

```yaml
services:
  munpae:
    image: munpae:local
    environment:
      MUNPAE_PROVIDER: "webhook"
      MUNPAE_WEBHOOK_URL: "http://dns-webhook:8888"
      MUNPAE_DOMAIN_FILTER: "example.com"
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock:ro

  dns-webhook:
    image: ghcr.io/<some-external-dns-webhook-provider>   # e.g. an existing provider server
    # …provider-specific credentials…
```

munpae still applies its own [domain filter](usage.md#scoping-with-a-domain-filter)
and [ownership registry](providers.md#ownership-registry) on top of the webhook
backend — the ownership TXT records are sent to the webhook server like any
other record.

## Notes and limitations

- **No in-client retries.** external-dns retries `5xx` responses with backoff;
  munpae does not — a failed reconcile is logged and retried on the next resync
  (`MUNPAE_RESYNC_INTERVAL`), consistent with the other providers.
- **`/adjustendpoints` is best-effort.** If the server does not implement it
  (`404`), munpae proceeds with the endpoints unchanged.
- The server-side `/healthz` and metrics endpoints from the external-dns spec
  are the server's concern; munpae only calls the record routes above.
