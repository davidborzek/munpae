# Examples

Runnable Compose setups. Each runs munpae in **dry-run** (logs the plan, touches
no DNS); set a provider to publish for real.

- [`docker-labels/`](docker-labels) — the `docker` source: declare records with
  explicit `munpae.dns/*` labels on any container.
- [`traefik/`](traefik) — the `traefik` source: hostnames derived from Traefik
  router rules, targets from an entrypoint→anchor map on the Traefik container
  (no per-app DNS labels).
