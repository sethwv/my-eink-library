---
title: Quick Start
nav_order: 2
---

# Quick Start

Docker is the quickest way to run my-sideload-library. Your EPUB files remain mounted read-only, while its index and user database live in a named Docker volume. For Windows and Debian package installation, see [Deployment](deployment/).

## 1. Create an override file

Copy this into `docker-compose.override.yml` next to the repository's Compose file. The override is ignored by Git, so it is the right place for the local library path.

```yaml
services:
  sideload-library:
    volumes:
      - /path/to/epubs:/library:ro
```

## 2. Start the library

```bash
docker compose up -d
```

Put the service behind an HTTPS reverse proxy, then open its HTTPS URL. The one-time setup page creates the first administrator account and signs it in automatically. The container's port 8080 is HTTP for the reverse proxy only; session cookies require HTTPS.

## 3. Add more library folders

Mount each folder and list its matching container paths in `LIBRARY_PATH`:

```yaml
services:
  sideload-library:
    environment:
      LIBRARY_PATH: /library/fiction,/library/nonfiction
    volumes:
      - /path/to/fiction:/library/fiction:ro
      - /path/to/nonfiction:/library/nonfiction:ro
```

The first configured folder wins when duplicate EPUBs are found.

{: .note }
To recover access, stop the service and run `docker compose run --rm sideload-library admin reset-password <username> <password>`. The command sets the supplied password without starting a web server.
