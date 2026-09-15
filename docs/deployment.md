---
title: Deployment
nav_order: 3
---

# Deployment

## Container image

Release images are published to:

```text
ghcr.io/sethwv/my-eink-library:latest
```

Development builds use `dev` and immutable `dev-<short-sha>` tags. Versioned releases also publish exact semantic-version and major/minor tags.

## Persistent data

Keep `DATA_DIR` on persistent storage. It contains the SQLite index, user database, cover cache, and application settings. The source EPUB directories can remain read-only.

## Public URLs and email

Set `PUBLIC_URL` to the externally reachable HTTPS URL when password-reset or invitation email is enabled. This prevents emailed links from being constructed from a client-controlled request host.

## Configuration

| Variable | Default | Purpose |
|---|---|---|
| `LIBRARY_PATH` | `/library` | Comma-separated EPUB directories |
| `DATA_DIR` | `/data` | Writable application data |
| `LIBRARY_USER` | Required | First-run administrator username |
| `LIBRARY_PASS` | Required | First-run administrator password |
| `SESSION_SECRET` | Required | Session-cookie signing key |
| `PORT` | `8080` | HTTP listen port |
| `SITE_NAME` | `eink-library` | Display name |
| `PAGE_SIZE` | `48` | Books per page |
| `PUBLIC_URL` | Empty | Trusted URL for emailed links |

For the full development environment and contribution terms, see [CONTRIBUTING.md](https://github.com/sethwv/my-eink-library/blob/main/CONTRIBUTING.md).
