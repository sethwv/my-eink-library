# eink-library

A single small Docker app that browses a read-only EPUB library and serves it
over a simple authenticated web UI, designed to work well in an e-reader's
built-in browser (Kobo/Kindle). Replacement for Calibre + calibre-web when all
you need is: browse covers in a sortable grid, download the original EPUB, or
download a Kobo-optimized KEPUB converted on the fly.

Metadata (title/author/series/cover) is read entirely from each EPUB's
embedded OPF package document and cover image — no sidecar files required.

## Running

```bash
cp docker-compose.yml docker-compose.override.yml   # optional, or edit directly
docker compose up -d --build
```

Edit `docker-compose.yml` first:
- Set `LIBRARY_USER` / `LIBRARY_PASS` to your desired login.
- Set `SESSION_SECRET` to a long random string.
- Point the `/library` volume mount at your real EPUB directory (mounted `:ro`).

Then visit `http://localhost:8080` and log in.

## Configuration

All configuration is via environment variables:

| Var | Default | Purpose |
|---|---|---|
| `LIBRARY_PATH` | `/library` | read-only directory to scan for `.epub` files |
| `DATA_DIR` | `/data` | writable dir for the SQLite index + cover cache |
| `LIBRARY_USER` | *(required)* | login username |
| `LIBRARY_PASS` | *(required)* | login password |
| `SESSION_SECRET` | *(required)* | signing key for session cookies |
| `PORT` | `8080` | HTTP listen port |
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
