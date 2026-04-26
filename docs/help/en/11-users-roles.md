---
title: Users and roles
slug: users-roles
order: 110
---

# Users and roles

You can run the blog with multiple users. There are three roles: **Admin**, **Power**, and **Regular**.

## Role differences

| Role | What they can do |
|---|---|
| Admin | Full admin features. Can manage users |
| Power | Manage entries, images, categories, links, templates, comments, etc. Cannot manage users |
| Regular | Manage entries, images, tags, comments, analytics, and their own profile |

Regular users have no access to categories, links, templates, design settings, or user management. For entries and images, they can only edit and delete the ones they themselves authored.

## Managing users

The **Users** screen is admin-only.

What you can do:

- Add a user
- Change display name, email, role
- Change password
- Edit profile
- Delete a user
- Reorder

If you need to rename someone's login name, recreate the user rather than editing in place.

## Protecting the admin role

To keep the admin UI usable, Serene Bach refuses these operations:

- Deleting yourself
- Deleting the last admin
- Demoting yourself out of the admin role

There must always be at least one admin.

## Profile

Every user can edit their own profile under **Profile**.

| Field | Description |
|---|---|
| Display name | Used as the author name on the public site |
| Email | Internal contact. Not normally published |
| About me | Shown on the profile page or via templates |
| Body format | Render the "About me" as HTML or Markdown |
| List me | Whether to include this user on the profile listing |

If you use the AI writing assist, configure it per-user under the profile or AI Settings tab.

## MCP "Acts as" user

MCP tokens are bound to an "Acts as" user. Entries an AI agent creates or updates through that token are recorded as authored by the bound user.

Creating a dedicated AI-only user makes it easier to spot which entries went through an AI agent later.

## Related pages

- [AI integration and MCP](mcp)
- [Writing and managing entries](entries)
