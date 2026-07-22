# docker-labels example

Runs munpae against two labelled workloads (`db`, `app`) in **dry-run**, so it
logs the DNS records it would publish without touching any backend. This uses
the `docker` source: you declare records with explicit `munpae.dns/*` labels.

## Run

```sh
docker compose up --build
```

munpae's log prints the plan on every reconcile:

```
msg="dry-run CREATE" name=db.example.com type=A targets=[192.0.2.2]
msg="dry-run CREATE" name=app.example.com type=CNAME targets=[ingress.example.com]
msg="dry-run CREATE" name=www.example.com type=CNAME targets=[ingress.example.com]
```

Metrics and health are exposed on the host:

```sh
curl -fsS localhost:9333/healthz    # -> ok
curl -fsS localhost:9333/metrics | grep '^munpae_'
```

## What it demonstrates

- **`db`** publishes a single **A** record via explicit
  `munpae.dns/hostname` + `munpae.dns/target` labels.
- **`app`** publishes **two names** (`app`, `www`) sharing one **CNAME** target
  — the record type is inferred from the target (hostname → CNAME, IP → A).

## Publish for real

Drop dry-run and point munpae at a backend. For a bind server via RFC2136:

```yaml
environment:
  MUNPAE_PROVIDER: rfc2136
  MUNPAE_DOMAIN_FILTER: example.com
  MUNPAE_RFC2136_HOST: 192.0.2.53
  MUNPAE_RFC2136_ZONE: example.com
  MUNPAE_RFC2136_TSIG_KEYNAME: munpae
  MUNPAE_RFC2136_TSIG_SECRET: base64-secret==
```

See the [providers docs](../../docs/providers.md) for Cloudflare and webhook
backends.

## Tear down

```sh
docker compose down
```
