---
title: Server
parent: User Guide
nav_order: 5
---

# Server

Admins and Server Managers can open **Admin** from the account menu. The Server tab shows the running library's indexed-book counts, configured paths, data directory, uptime, and last scan.

## Minimum: keep the index current

Use **Rescan library now** after adding, removing, or replacing EPUB files in the configured library folders. A rescan checks the current folders and updates the index.

Use **Force full reimport** only when a normal rescan cannot repair an indexing problem. It rebuilds the index from the source folders, so allow it to finish before investigating missing books or metadata.

## General settings

Open **Configuration** to change the site name, cover width, books per page, and session length. Use a positive number for cover width and books per page. Session length uses a Go duration such as `720h` for thirty days.

Set **Public URL** to the externally reachable HTTPS address, for example `https://library.example.com`. It is required for password-reset and invitation messages so links in email point to the correct public site. It does not replace the HTTPS reverse-proxy requirement in [Deployment](../../deployment/).

{% include screenshot-pair.html id="admin-configuration" %}

## Advanced: configure and test email

1. Open the **SMTP** tab.
2. Enter the provider hostname, port, encryption method, login, password, and sender name/address.
3. Select **Save SMTP settings**.
4. Enter a controlled recipient address in the test form and select **Send test email**.
5. Confirm delivery and check that any invitation or reset link uses the configured Public URL.

Choose the encryption mode required by your provider: **SSL/TLS**, **STARTTLS**, or **None**. Leave the password field blank when updating another SMTP field and keeping the stored password. Treat SMTP credentials as secrets and restrict server-management access accordingly.

{: .note }
The weekly new-book digest, invitations, and password resets all depend on working SMTP. A successful save alone does not confirm delivery, so always use the test message.

## Session secrets

Current releases require `SESSION_SECRET` in the deployment environment. A pending change will generate and persist a secret when it is unset while retaining an environment override for operations that require it. Follow the deployment and release documentation for the version you run; changing a signing secret invalidates existing login sessions.
