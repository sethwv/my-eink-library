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

## Account settings

Select **Account** from the account menu to change your password or manage an e-reader bookmark link. To change your password, enter the current password, choose the new one, then select **Change password**. Use a password you do not reuse elsewhere.

If the library administrator configures outbound email, an **Email** tab becomes available. Use it to add, change, or remove your email address and to opt in to the **New-book digest**, a weekly list of books added to the library.

If you forgot your password, use the sign-in page's password-reset flow when it is available. The administrator must configure email and explicitly enable self-service password reset. Contact the library administrator if the reset email does not arrive.

{% include screenshot-pair.html id="account-preferences" %}
{% include screenshot-pair.html id="account-password" %}

## Create an e-reader bookmark link

Your **Account** page may offer bookmark-link controls. They create one passwordless link for an e-reader browser that does not retain cookies between sessions.

1. Open the account menu and select **Account**.
2. Select **Create bookmark link**.
3. When the library opens, use the e-reader browser's own bookmark command immediately.

{% include screenshot-pair.html id="account-bookmark" %}

Treat this link as a password. Anyone who has it can use the library as your account. The link is shown only in the browser address bar after it is created. Creating or regenerating a link invalidates the old one straight away.

{: .warning }
Do not send a bookmark link in email, chat, or a shared note. Bookmark sessions can browse the library and use shelves, but cannot change account settings or use administration. Select **Log in** from the account menu to regain a full session.
