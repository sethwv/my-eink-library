---
title: Docker
parent: Deployment
nav_order: 1
---

# Docker

Release images are published to:

```text
ghcr.io/sethwv/my-sideload-library:latest
```

Development builds use `<branch>` and immutable `<branch>-<short-sha>` tags, such as `main-a1b2c3d`. Versioned releases also publish exact semantic-version and major/minor tags.

Use the repository's `docker-compose.yml` with an ignored `docker-compose.override.yml` for the host library directory:

```yaml
services:
  sideload-library:
    volumes:
      - /path/to/epubs:/library:ro
```

Start it with:

```bash
docker compose up -d
```

On first startup, open the application to create the first administrator account in the one-time setup page. The override file is ignored by Git. Docker Compose's project `.env` file only performs variable substitution unless the Compose configuration passes values into the container, so use the `environment` block above for application configuration when needed.
