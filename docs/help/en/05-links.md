---
title: Links
slug: links
order: 50
---

# Links

The **Links** screen manages a curated list of external links (a blogroll) for the sidebar or wherever your template surfaces it. The feature is inherited from SB3 and is rendered via the `link` block and the `{link_list}` tag in templates.

The Links screen is available to admin and power users. Regular users do not see it.

## Links and groups

There are two row types:

| Type | Purpose |
|---|---|
| Link | A single destination (name + URL + how to open) |
| Group | A heading that bundles multiple links together |

A group can contain links, but a group cannot contain another group — nesting is one level only.

## Creating and editing

Click **New link** to open the create form with a type selector. From a group's edit page, the same button creates a link directly under that group.

Main fields for a link:

| Field | Description |
|---|---|
| Name | The text shown for the link |
| URL | The destination URL |
| Description | Free-form description (rendered if your template supports it) |
| Target | `target` attribute (`_self`, `_blank`, …) |
| Parent group | Set only when placing the link inside a group |
| Visible / Hidden | Temporarily hides the row without deleting it |

Groups only carry a name and a description — no URL or target.

## Reordering

Drag rows on the list page to change the top-level order. Reorder a group's members from the group's own edit page.

## How it appears on the public site

In a template, wrap a `<!-- BEGIN link --> ... <!-- END link -->` block around the `{link_list}` tag. Registered links are emitted as a `<ul>` fragment; groups become nested `<ul>` elements.

```html
<!-- BEGIN link -->
<aside class="blogroll">
  <h3>Links</h3>
  {link_list}
</aside>
<!-- END link -->
```

Rows marked **Hidden** are excluded from the `{link_list}` output.

## Migrating from SB2 / SB3

The importer does not currently bring across SB2's `link.cgi` data or SB3's link rows. After importing, re-register links manually from the admin UI.

## Related pages

- [Template editing](templates)
- [Blog design and OG cards](design)
