---
title: Windows
parent: Deployment
nav_order: 2
---

# Windows

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
