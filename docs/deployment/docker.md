---
title: Docker
parent: Deployment
nav_order: 1
---

# Docker

Release images are published to:

```text
ghcr.io/sethwv/my-eink-library:latest
```

Development builds use `dev` and immutable `dev-<short-sha>` tags. Versioned releases also publish exact semantic-version and major/minor tags.

Use the repository's `docker-compose.yml` with an ignored `docker-compose.override.yml` for credentials and the host library directory:

```yaml
services:
  eink-library:
    environment:
      LIBRARY_USER: reader
      LIBRARY_PASS: choose-a-password
      SESSION_SECRET: choose-a-long-random-secret
    volumes:
      - /path/to/epubs:/library:ro
```

Start it with:

```bash
docker compose up -d
```

The override file is ignored by Git. It is preferred over committing credentials in the tracked Compose file. Docker Compose's project `.env` file only performs variable substitution unless the Compose configuration passes values into the container, so use the `environment` block above for this project.
