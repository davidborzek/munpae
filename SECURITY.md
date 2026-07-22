# Security policy

## Reporting a vulnerability

Please **do not** open a public issue for security vulnerabilities.

Instead, report them privately via GitHub's
[security advisories](https://github.com/davidborzek/munpae/security/advisories/new)
("Report a vulnerability"). You will receive a response as soon as possible, and
disclosure will be coordinated with you.

## Scope

munpae programs DNS records in a backend using credentials you supply (TSIG
keys, a Cloudflare API token, or a webhook endpoint) via environment variables.
Keep those secrets out of version control and restrict them to the minimum
required permissions (e.g. a Cloudflare token scoped to the managed zone).
