---
title: Image uploads
slug: images
order: 100
---

# Image uploads

The **Images** screen manages the images you use in entries and on OG cards. Uploaded images are served from the public site under `/img/...`.

## Uploading

Drag files onto the images screen, or click to pick. Multiple files at once are fine.

From the entry editor, use **Insert image** to drop in an existing upload. Dragging an image directly onto the body area uploads and inserts in one go.

## Supported formats

- JPEG
- PNG
- GIF
- WebP

The per-file size limit comes from `SB_UPLOAD_MAX_MB` (default 10 MB).

## Listing

The images screen toggles between grid and list views. You can also filter by filename.

The list view exposes filename, size, and dimensions. The same library powers both the entry editor's image picker and the OG card background picker.

## Copying image URLs

Copy a URL to paste into entries or templates:

```html
<img src="/img/2026/04/sample-ab12cd.jpg" alt="">
```

Markdown entries get the Markdown image syntax when you use the editor's insert button.

## Alt text

You can attach alt text to an image. When the AI writing assist is configured, alt text suggestions can be generated automatically after upload.

Alt text is read out by screen readers and shown when an image fails to load. Provide a short description for any meaningful image.

## Deleting

Deleting an image removes the file. Entries and templates that referenced it will show a broken image until you fix them.

Before deleting, double-check whether the image is in use.

## Static rebuild interaction

When you run a static rebuild, uploaded images are copied to the output directory. Static deployments serve entry images and OG card images directly from there.

## Related pages

- [Writing and managing entries](entries)
- [Publishing settings and OG cards](settings-publishing)
