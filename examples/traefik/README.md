# traefik example

Runs munpae with the `traefik` source in **dry-run**. Hostnames are derived
from Traefik router rules — **no per-app DNS labels** — and each router's
**entrypoint** picks the target from the anchor map on the Traefik container.

## Run

```sh
docker compose up --build
```

munpae's log prints the plan:

```
msg="dry-run CREATE" name=api.example.com  type=CNAME targets=[internal.example.com]
msg="dry-run CREATE" name=shop.example.com type=CNAME targets=[external.example.com]
```

## What it demonstrates

- The Traefik container carries the **entrypoint → target anchors**
  (`munpae.dns/traefik.entrypoint.<ep>.target`) — a topology fact owned by the
  reverse proxy, not by munpae.
- **`api`** is routed on `web-internal`, so its `Host(...)` name resolves to
  `internal.example.com`.
- **`shop`** is routed on `web-external`, so it resolves to
  `external.example.com`.
- munpae reads the routers' `Host(...)` rules for the names; the record type is
  inferred from the anchor target (hostname → CNAME).

A per-app `munpae.dns/target` label still wins over the entrypoint anchor if you
need to override one service.

## Split horizon

A router listed on **both** entrypoints is published for both — run one munpae
instance per horizon and scope each with `MUNPAE_TRAEFIK_ENTRYPOINTS` (e.g. the
internal instance publishes all, the external one sets
`MUNPAE_TRAEFIK_ENTRYPOINTS=web-external`). See
[docs/sources.md](../../docs/sources.md#traefik).

## Publish for real

Drop `MUNPAE_DRY_RUN` and set a provider (see the
[providers docs](../../docs/providers.md)).

## Tear down

```sh
docker compose down
```
