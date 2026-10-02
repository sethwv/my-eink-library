---
title: Debian Linux
parent: Deployment
nav_order: 4
---

# Debian Linux

Download the package matching the machine architecture from the [GitHub release](https://github.com/sethwv/my-sideload-library/releases): `sideload-library_<version>_linux_amd64.deb` for x86-64 or `sideload-library_<version>_linux_arm64.deb` for 64-bit ARM. Install it with:

```bash
sudo apt install ./sideload-library_<version>_linux_amd64.deb
```

The package installs the executable at `/usr/bin/sideload-library`. It intentionally does not create a user, service, or configuration because library locations and credentials are installation-specific. The following creates a dedicated system user and persistent directories for a systemd deployment:

```bash
sudo useradd --system --create-home --home-dir /var/lib/sideload-library --shell /usr/sbin/nologin sideload-library
sudo install -d -o sideload-library -g sideload-library -m 0750 /etc/sideload-library
```

Create `/etc/sideload-library/sideload-library.env` with the required paths. The `sideload-library` user must be able to read `LIBRARY_PATH` and write `DATA_DIR`.

```dotenv
LIBRARY_PATH=/srv/epubs
DATA_DIR=/var/lib/sideload-library
PORT=8080
```

Protect the file because it controls the service configuration:

```bash
sudo chown root:sideload-library /etc/sideload-library/sideload-library.env
sudo chmod 640 /etc/sideload-library/sideload-library.env
```

Then create `/etc/systemd/system/sideload-library.service`:

```ini
[Unit]
Description=my-sideload-library
After=network-online.target
Wants=network-online.target

[Service]
User=sideload-library
Group=sideload-library
EnvironmentFile=/etc/sideload-library/sideload-library.env
ExecStart=/usr/bin/sideload-library
Restart=on-failure

[Install]
WantedBy=multi-user.target
```

Enable and start the service:

```bash
sudo systemctl daemon-reload
sudo systemctl enable --now sideload-library
sudo systemctl status sideload-library
```

`EnvironmentFile` is systemd's supported way to load the `.env`-style file. Do not pass this file directly to the executable; it only reads variables inherited from its process environment.

The first service startup presents a one-time setup page for creating the first administrator. To recover access later, stop the service and run `sudo -u sideload-library DATA_DIR=/var/lib/sideload-library /usr/bin/sideload-library admin reset-password <username> <password>`; it sets the supplied password without running the server.
