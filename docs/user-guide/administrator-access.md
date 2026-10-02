---
title: Admin
parent: User Guide
nav_order: 4
---

# Admin

Open the account menu and select **Admin** to manage users or server functions. What you can see depends on your role.

| Role | Access |
|---|---|
| Member | Browse the library and use personal account features. |
| User Manager | Manage users, roles, invitations, and bookmark-link permission. |
| Server Manager | Manage the server, settings, email, integrations, and library metadata. |
| Admin | All user and server management functions. |

## Choose the right role

Members read books and manage their own account. User Managers handle people, invitations, roles, and bookmark permission. Server Managers maintain the application. Admins have both sets of permissions.

{% include screenshot-pair.html id="admin-manage-user" %}

## Add a member

1. Sign in as an Admin or User Manager and open **Admin**.
2. Select the **Users** tab and choose **Add user**.
3. Enter a username and password, leave the role as **Member**, then select **Create user**.
4. Give the user their initial password through a private channel.

Add an email address if the user should be able to reset their password. The email field is optional when creating a user, but self-service password reset needs an address, working server email settings, and the Password reset setting enabled in Admin Setup.

{% include screenshot-pair.html id="admin-add-user" %}

## Manage access

Use **Manage** beside an existing user to change their role, grant or revoke bookmark-link access, update their email address, reset their password, disable or re-enable their account, or delete their account. Disabling preserves the user's data but blocks password, bookmark-link, reset-link, and invitation access until re-enabled. The final enabled administrator cannot be disabled. Give the lowest role that allows the user to do their work. A User Manager cannot manage server configuration, while a Server Manager cannot manage users.

{% include screenshot-pair.html id="admin-manage-user" %}

## Invite a user by email

To send an invitation, select **Invite by email**, provide the username, email address, and role, then send it. Invitations need SMTP and a public HTTPS URL. Configure and test those first in [Server](../server-settings-and-email/); the invite and resend controls remain unavailable until both are set.

{% include screenshot-pair.html id="admin-invite-user" %}

Pending invitations can be resent from the user's Manage panel. Once a user has accepted an invitation, that panel provides an administrator password-reset action instead.

{: .warning }
Deleting a user removes their access. Check that the account is no longer needed before selecting **Delete user**.

On first startup, the application presents a one-time setup page to create the first administrator account and signs that account in automatically. If access is lost later, stop the application and run `sideload-library admin reset-password <username> <password>` with the same `DATA_DIR`; the break-glass command sets the supplied password without starting the server. See [Deployment](../../deployment/) for platform-specific commands.
