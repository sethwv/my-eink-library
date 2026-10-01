---
title: macOS
parent: Deployment
nav_order: 2
---

# macOS

Download the executable for your Mac from the [GitHub release](https://github.com/sethwv/my-eink-library/releases): `eink-library_<version>_darwin_arm64` for Apple Silicon, or `eink-library_<version>_darwin_amd64` for Intel Macs. Save it outside the application data directory, then make it executable:

```bash
chmod +x eink-library_<version>_darwin_arm64
```

Create a persistent data directory and launch the server with the required configuration:

```bash
mkdir -p "$HOME/Library/Application Support/eink-library"
LIBRARY_PATH="$HOME/Books" \
DATA_DIR="$HOME/Library/Application Support/eink-library" \
PORT=8080 \
./eink-library_<version>_darwin_arm64
```

Replace `darwin_arm64` with `darwin_amd64` on Intel Macs. Put the process behind an HTTPS reverse proxy before signing in because session cookies require HTTPS. For an always-on installation, run the executable with a `launchd` service.

macOS may block an unsigned executable that was downloaded from the internet. After attempting to open it, go to **System Settings > Privacy & Security**, scroll to the bottom, and choose **Open Anyway**. The first launch displays a one-time setup page to create the first administrator. To reset a password without starting the server, use the same data directory:

```bash
DATA_DIR="$HOME/Library/Application Support/eink-library" \
./eink-library_<version>_darwin_arm64 admin reset-password admin <password>
```
