---
title: Windows
parent: Deployment
nav_order: 3
---

# Windows

Download `sideload-library_<version>_windows_amd64.exe` from the [GitHub release](https://github.com/sethwv/my-sideload-library/releases). Create a data directory outside the application download directory, then launch the executable from PowerShell with its required configuration:

```powershell
New-Item -ItemType Directory -Force C:\sideload-library-data
$env:LIBRARY_PATH = "D:\Books"
$env:DATA_DIR = "C:\sideload-library-data"
$env:PORT = "8080"
& "C:\sideload-library\sideload-library_<version>_windows_amd64.exe"
```

Put the process behind an HTTPS reverse proxy, then open its HTTPS URL. Port 8080 is HTTP for the reverse proxy only because session cookies require HTTPS. The process must remain running, so use Task Scheduler or a Windows service wrapper for an always-on installation.

The executable does not load `.env` files itself. For repeatable local launches, save the environment assignments and final command above in a private PowerShell script, such as `run-sideload-library.ps1`, and keep that script out of source control.

The first launch displays a one-time setup page to create the first administrator. To reset a password without starting the server, use the same data directory: `$env:DATA_DIR = "C:\sideload-library-data"; & "C:\sideload-library\sideload-library_<version>_windows_amd64.exe" admin reset-password admin <password>`.
