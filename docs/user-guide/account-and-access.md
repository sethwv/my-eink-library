---
title: Account
parent: User Guide
nav_order: 3
---

# Account

Open the account menu from the top navigation to change the reading appearance or reach account actions.

## Choose an appearance

Select **Auto**, **Light**, or **Dark** in the account menu.

- **Auto** follows the browser's system preference when available.
- **Light** keeps the light application appearance.
- **Dark** keeps the dark application appearance.

The choice is stored in a browser cookie for up to a year. It applies on the device and browser where you set it, not to every device using the account.

{% include screenshot-pair.html id="account-appearance" %}

## Change your password or weekly digest

Select **Change password** from the account menu to set a new password. Enter the current password, choose the new one, then select **Change password**. Use a password you do not reuse elsewhere.

The same page has the **New-book digest** preference. Enable it and select **Save** to receive a weekly list of books added to the library. The library administrator must configure outbound email before the digest controls become available.

If you forgot your password, use the sign-in page's password-reset flow. That flow is unavailable until the administrator configures SMTP and the public HTTPS URL. Contact the library administrator if the reset email does not arrive.

{% include screenshot-pair.html id="account-preferences" %}

## Create an e-reader bookmark link

If the account menu offers **Bookmark link**, it can create one passwordless link for an e-reader browser that does not retain cookies between sessions.

1. Open the account menu and select **Bookmark link**.
2. Select **Create bookmark link**.
3. When the library opens, use the e-reader browser's own bookmark command immediately.

{% include screenshot-pair.html id="account-bookmark" %}

Treat this link as a password. Anyone who has it can use the library as your account. The link is shown only in the browser address bar after it is created. Creating or regenerating a link invalidates the old one straight away.

{: .warning }
Do not send a bookmark link in email, chat, or a shared note. Bookmark sessions can browse the library and use shelves, but cannot change account settings or use administration. Select **Log in** from the account menu to regain a full session.
