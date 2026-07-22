# Installation

munpae runs as a single container on the Docker host it manages. It needs:

- **Read access to the Docker socket** — to list containers and watch events.
- **Network access to your DNS backend** — a bind server (RFC2136), the
  Cloudflare API, or a webhook provider endpoint.

## Image

Released versions are published to the GitHub Container Registry:

```sh
docker pull ghcr.io/davidborzek/munpae:latest
```

To build from source instead — for development, or before the first release —
the provided [`Dockerfile`](../Dockerfile) produces a static, distroless,
non-root binary:

```sh
docker build -t munpae:local .
```

Or build the binary directly (Go 1.26+):

```sh
CGO_ENABLED=0 go build -o munpae ./cmd/munpae
```

## Run with Docker Compose

```yaml
services:
  munpae:
    image: munpae:local
    restart: unless-stopped
    environment:
      MUNPAE_SOURCES: "docker,traefik"
      MUNPAE_PROVIDER: "rfc2136"
      MUNPAE_DOMAIN_FILTER: "example.com"
      MUNPAE_RFC2136_HOST: "192.0.2.53"
      MUNPAE_RFC2136_ZONE: "example.com"
      MUNPAE_RFC2136_TSIG_KEYNAME: "munpae"
      MUNPAE_RFC2136_TSIG_SECRET: "base64-secret=="
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock:ro
    ports:
      - "9333:9333"   # optional: expose /metrics + /healthz
```

munpae reads the Docker endpoint from the standard environment
(`DOCKER_HOST`, else `unix:///var/run/docker.sock`), so the bind mount above is
all that is normally required.

> **Docker socket permissions.** The image runs as a non-root user. The
> mounted socket must be readable by that user — depending on your host you may
> need to add the container to the `docker` group (`group_add:` with the
> socket's GID) or adjust socket permissions. This is the same constraint any
> non-root Docker-socket consumer has.

## Verify it is running

Always start with a **dry run** so nothing is written while you confirm the
records munpae derives:

```sh
docker run --rm \
  -e MUNPAE_DRY_RUN=true \
  -e MUNPAE_SOURCES=docker \
  -e MUNPAE_PROVIDER=rfc2136 \
  -e MUNPAE_DOMAIN_FILTER=example.com \
  -v /var/run/docker.sock:/var/run/docker.sock:ro \
  munpae:local
```

The log prints each `dry-run CREATE/UPDATE/DELETE` it would perform. When it
looks right, remove `MUNPAE_DRY_RUN`.

munpae also has a small command-line interface: `--dry-run` (equivalent to the
env var), `--version`, and `--help`.

If `MUNPAE_METRICS_ADDR` is set (default `:9333`), two HTTP endpoints are
available:

- `GET /healthz` — liveness, returns `200 ok`.
- `GET /metrics` — Prometheus metrics (`munpae_*`).

```sh
curl -fsS localhost:9333/healthz    # -> ok
```

## Next steps

- [Configuration reference](configuration.md) — every variable.
- [Usage](usage.md) — sources, providers, and multi-instance setups.
