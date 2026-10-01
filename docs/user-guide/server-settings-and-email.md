---
title: Server
parent: User Guide
nav_order: 6
---

# Server

Admins and Server Managers can open **Admin** from the account menu. The Server tab shows the running library's indexed-book counts, configured paths, data directory, uptime, and last scan.

## Maintain the library index

Open **Tasks** and queue **Scan Library** after adding, removing, or replacing EPUB files in the configured library folders. A scan also runs when the server starts. See [Tasks](../tasks/) for queue status and history.

Use **Clear library and queue scan** on the Server tab when the derived index needs to be rebuilt from the configured folders. This removes indexed books, enrichment state, locations, and book-to-shelf memberships, then queues a fresh scan. Shelf definitions remain in place.

{% include screenshot-pair.html id="admin-server" %}

## Configure the library

Open **Configuration** to change the site name, cover width, books per page, and session length. Use a positive number for cover width and books per page. Session length uses a Go duration such as `720h` for thirty days.

Set **Public URL** to the externally reachable HTTPS address, for example `https://library.example.com`. It is required for password-reset and invitation messages so links in email point to the correct public site. It does not replace the HTTPS reverse-proxy requirement in [Deployment](../../deployment/).

{% include screenshot-pair.html id="admin-configuration" %}

## Send a test email

1. Open the **SMTP** tab.
2. Enter the provider hostname, port, encryption method, login, password, and sender name/address.
3. Select **Save SMTP settings**.
4. Enter a controlled recipient address in the test form and select **Send test email**.
5. Confirm delivery and check that any invitation or reset link uses the configured Public URL.

Choose the encryption mode required by your provider: **SSL/TLS**, **STARTTLS**, or **None**. Leave the password field blank when updating another SMTP field and keeping the stored password. Treat SMTP credentials as secrets and restrict server-management access accordingly.

{% include screenshot-pair.html id="admin-smtp" %}

{: .note }
The weekly new-book digest, invitations, and password resets all depend on working SMTP. A successful save alone does not confirm delivery, so always use the test message.

When `SESSION_SECRET` is unset, the application generates and persists a session signing secret in `DATA_DIR/users.db`. Set `SESSION_SECRET` only for a runtime override: it never overwrites the persisted value, and changing an override invalidates existing login sessions.
