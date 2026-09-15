---
title: Deployment
nav_order: 3
---

# Deployment

my-eink-library can run in Docker, directly on Windows, or from the Debian package on 64-bit x86 and ARM Linux. Every option needs a readable EPUB directory, a separate writable data directory, and the required environment variables below.

## Docker

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

## Windows

Download `eink-library_<version>_windows_amd64.exe` from the [GitHub release](https://github.com/sethwv/my-eink-library/releases). Create a data directory outside the application download directory, then launch the executable from PowerShell with its required configuration:

```powershell
New-Item -ItemType Directory -Force C:\eink-library-data
$env:LIBRARY_PATH = "D:\Books"
$env:DATA_DIR = "C:\eink-library-data"
$env:LIBRARY_USER = "reader"
$env:LIBRARY_PASS = "choose-a-password"
$env:SESSION_SECRET = "choose-a-long-random-secret"
$env:PORT = "8080"
& "C:\eink-library\eink-library_<version>_windows_amd64.exe"
```

Open `http://localhost:8080` after it starts. The process must remain running, so use Task Scheduler or a Windows service wrapper for an always-on installation.

The executable does not load `.env` files itself. For repeatable local launches, save the environment assignments and final command above in a private PowerShell script, such as `run-eink-library.ps1`, and keep that script out of source control.

## Debian Linux

Download the package matching the machine architecture from the [GitHub release](https://github.com/sethwv/my-eink-library/releases): `eink-library_<version>_linux_amd64.deb` for x86-64 or `eink-library_<version>_linux_arm64.deb` for 64-bit ARM. Install it with:

```bash
sudo apt install ./eink-library_<version>_linux_amd64.deb
```

The package installs the executable at `/usr/bin/eink-library`. It intentionally does not create a user, service, or configuration because library locations and credentials are installation-specific. The following creates a dedicated system user and persistent directories for a systemd deployment:

```bash
sudo useradd --system --create-home --home-dir /var/lib/eink-library --shell /usr/sbin/nologin eink-library
sudo install -d -o eink-library -g eink-library -m 0750 /etc/eink-library
```

Create `/etc/eink-library/eink-library.env` with the required settings. The `eink-library` user must be able to read `LIBRARY_PATH` and write `DATA_DIR`.

```dotenv
LIBRARY_PATH=/srv/epubs
DATA_DIR=/var/lib/eink-library
LIBRARY_USER=reader
LIBRARY_PASS=choose-a-password
SESSION_SECRET=choose-a-long-random-secret
PORT=8080
```

Protect the file because it holds credentials:

```bash
sudo chown root:eink-library /etc/eink-library/eink-library.env
sudo chmod 640 /etc/eink-library/eink-library.env
```

Then create `/etc/systemd/system/eink-library.service`:

```ini
[Unit]
Description=my-eink-library
After=network-online.target
Wants=network-online.target

[Service]
User=eink-library
Group=eink-library
EnvironmentFile=/etc/eink-library/eink-library.env
ExecStart=/usr/bin/eink-library
Restart=on-failure

[Install]
WantedBy=multi-user.target
```

Enable and start the service:

```bash
sudo systemctl daemon-reload
sudo systemctl enable --now eink-library
sudo systemctl status eink-library
```

`EnvironmentFile` is systemd's supported way to load the `.env`-style file. Do not pass this file directly to the executable; it only reads variables inherited from its process environment.

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
