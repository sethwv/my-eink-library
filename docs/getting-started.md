---
title: Getting started
nav_order: 2
---

# Getting started

Docker is the quickest way to run my-eink-library. Your EPUB files remain mounted read-only, while its index and user database live in a named Docker volume. For Windows and Debian package installation, see [Deployment](deployment.html).

## 1. Create an override file

Copy this into `docker-compose.override.yml` next to the repository's Compose file. The override is ignored by Git, so it is the right place for local paths and credentials.

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

## 2. Start the library

```bash
docker compose up -d
```

Open `http://localhost:8080` and sign in with the bootstrap credentials from the override file.

## 3. Add more library folders

Mount each folder and list its matching container paths in `LIBRARY_PATH`:

```yaml
services:
  eink-library:
    environment:
      LIBRARY_PATH: /library/fiction,/library/nonfiction
    volumes:
      - /path/to/fiction:/library/fiction:ro
      - /path/to/nonfiction:/library/nonfiction:ro
```

The first configured folder wins when duplicate EPUBs are found.

{: .note }
`LIBRARY_USER` and `LIBRARY_PASS` create the first administrator only. Later user management happens in the web UI.
