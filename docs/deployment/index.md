---
title: Deployment
nav_order: 3
has_children: true
---

# Deployment

my-eink-library can run in Docker, directly on Windows, or from a Debian package on 64-bit x86 and ARM Linux. Choose a platform from the navigation to get started.

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
| `COVER_WIDTH` | `300` | Cover thumbnail width in pixels |
| `PAGE_SIZE` | `48` | Books per page |
| `SESSION_TTL` | `720h` | Login-session lifetime |
| `HARDCOVER_API_TOKEN` | Empty | One-time Hardcover integration bootstrap token |
| `PUBLIC_URL` | Empty | Trusted URL for emailed links |

For the full development environment and contribution terms, see [CONTRIBUTING.md](https://github.com/sethwv/my-eink-library/blob/main/CONTRIBUTING.md).
