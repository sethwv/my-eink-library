# Contributing to my-eink-library

By submitting a pull request, you confirm that you have the right to submit the contribution and license it under the GNU Affero General Public License v3.0 only.

If your employer or another party owns the contribution, obtain its authorization before submitting it.

Contributions must not include code, assets, or data whose license is incompatible with AGPL-3.0-only. Preserve all required third-party notices and identify their source and license in the pull request.

Submitting a pull request grants my-eink-library and its maintainers a perpetual, worldwide, non-exclusive, royalty-free, irrevocable license to use, modify, distribute, sublicense, and relicense the contribution as part of my-eink-library under any [OSI-approved open-source license](https://opensource.org/licenses). This permission does not transfer copyright ownership.

## Development Environment

Install Go 1.25.7 and Docker Compose. Clone the repository, then download dependencies:

```bash
cd src
go mod download
```

For a local server, point the app at writable data plus an EPUB directory:

```bash
LIBRARY_PATH=/path/to/epubs \
DATA_DIR=/tmp/my-eink-library-data \
(cd src && go run ./cmd/server)
```

The first run logs a generated password for the `admin` account once. Keep `DATA_DIR` if that password needs recovery: `(cd src && DATA_DIR=/tmp/my-eink-library-data go run ./cmd/server admin reset-password admin)` generates and prints a replacement password without starting the server. The same break-glass CLI can create an account with `admin create-user <username> <password> [role]`.

For Docker development, add local credentials and the EPUB bind mount in `docker-compose.override.yml`, then run `docker compose up --build`.

## Configuration

| Variable | Default | Purpose |
|---|---|---|
| `LIBRARY_PATH` | `/library` | Comma-separated read-only EPUB directories |
| `DATA_DIR` | `/data` | Writable SQLite indexes, user database, and cover cache |
| `SESSION_SECRET` | Empty | Runtime session-cookie signing-key override |
| `PORT` | `8080` | HTTP listen port |
| `SITE_NAME` | `eink-library` | Displayed site name |
| `COVER_WIDTH` | `300` | Cover thumbnail width in pixels |
| `PAGE_SIZE` | `48` | Books per page |
| `SESSION_TTL` | `720h` | Login-session lifetime |
| `HARDCOVER_API_TOKEN` | Empty | One-time bootstrap token for the Hardcover integration |
| `PUBLIC_URL` | Empty | Trusted public base URL for invite and reset links |

`SESSION_SECRET` is generated and stored in `DATA_DIR/users.db` when unset. Setting it uses that value for the current process without changing the stored secret, so changing or removing an override invalidates active sessions. `HARDCOVER_API_TOKEN`, integration settings, and server settings become database-backed after initial setup. Use the admin UI to change persisted settings later.

## Validation

Run these before opening a pull request:

```bash
cd src
go test ./...
go vet ./...
go build ./...
```

UI changes should also be checked in a modern browser. Kobo's browser has older QtWebKit limitations, so confirm device behavior when changing CSS or JavaScript compatibility-sensitive code.
