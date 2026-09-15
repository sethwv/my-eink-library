---
title: Debian Linux
parent: Deployment
nav_order: 3
---

# Debian Linux

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
