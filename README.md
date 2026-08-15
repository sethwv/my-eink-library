# eink-library

A single small Docker app that browses a read-only EPUB library and serves it
over a simple authenticated web UI, designed to work well in an e-reader's
built-in browser (Kobo/Kindle). Replacement for Calibre + calibre-web when all
you need is: browse covers in a sortable grid, download the original EPUB, or
download a Kobo-optimized KEPUB converted on the fly.

Metadata (title/author/series/cover) is read entirely from each EPUB's
embedded OPF package document and cover image — no sidecar files required.

## Running

Don't put real credentials in the tracked `docker-compose.yml` — it ships with
placeholder values on purpose. Instead, create a `docker-compose.override.yml`
(already in `.gitignore`, so it's never committed) with your real values:

```yaml
services:
  eink-library:
    environment:
      - LIBRARY_USER=your-username
      - LIBRARY_PASS=your-password
      - SESSION_SECRET=some-long-random-string
    volumes:
      - /path/to/your/epubs:/library:ro
```

To combine multiple folders into one library, bind-mount each and list them
in `LIBRARY_PATH` as a comma-separated list:

```yaml
services:
  eink-library:
    environment:
      - LIBRARY_PATH=/library/fiction,/library/nonfiction
    volumes:
      - /path/to/fiction:/library/fiction:ro
      - /path/to/nonfiction:/library/nonfiction:ro
```

Then:

```bash
BUILD_VERSION="$(sh scripts/build-version)" docker compose up -d --build
```

Compose automatically merges `docker-compose.override.yml` over
`docker-compose.yml`. Visit `http://localhost:8080` and log in.

The footer displays the current exact Git tag when the checkout is clean and
tagged, otherwise its short commit SHA, followed by the image build date. A
dirty checkout also uses its short commit SHA.

## Users

`LIBRARY_USER`/`LIBRARY_PASS` are **bootstrap-only**: on first startup (empty
user database), they create the first account as an admin. After that, they're
ignored — logins are checked against a `users` table in `DATA_DIR`, and the
admin can add/remove other users or reset passwords from **Manage users**
(linked in the library page header when logged in as an admin). Every user has
equal read/download access to the library; `is_admin` only gates the user
management page. The last remaining admin can't be deleted.

## Configuration

All configuration is via environment variables:

| Var | Default | Purpose |
|---|---|---|
| `LIBRARY_PATH` | `/library` | comma-separated list of read-only directories to scan for `.epub` files, combined into one library |
| `DATA_DIR` | `/data` | writable dir for the SQLite index, users DB, cover cache |
| `LIBRARY_USER` | *(required)* | bootstrap admin username (first run only) |
| `LIBRARY_PASS` | *(required)* | bootstrap admin password (first run only) |
| `SESSION_SECRET` | *(required)* | signing key for session cookies |
| `PORT` | `8080` | HTTP listen port |
| `SITE_NAME` | `eink-library` | display name shown in the nav, login page, and browser tab |
| `COVER_WIDTH` | `300` | cover thumbnail width in px |
| `PAGE_SIZE` | `48` | books per grid page |
| `SESSION_TTL` | `720h` | how long a login session lasts |

The library is never written to. The app indexes it into a local SQLite
database in `DATA_DIR` and watches it for changes while running.

## Development

```bash
go build ./...
go test ./...
go run ./cmd/server   # requires LIBRARY_USER/LIBRARY_PASS/SESSION_SECRET set
```
