---
title: File uploads
slug: images
order: 100
---

# File uploads

The **Library** screen manages the files you use in entries and on OG cards. Uploaded files are served from the public site under `/img/...`.

## Uploading

Drag files onto the library screen, or click to pick. Multiple files at once are fine.

From the entry editor, use **Insert file** to drop in an existing upload. Dragging a file directly onto the body area uploads and inserts in one go.

## Supported formats

| Kind | Extensions |
|------|------------|
| Image | `.png`, `.jpg`, `.jpeg`, `.gif`, `.webp` |
| Audio | `.mp3`, `.ogg`, `.m4a` |
| Document | `.pdf`, `.txt`, `.md` |
| Movie | `.mp4`, `.webm` |

The per-file size limit comes from `SB_UPLOAD_MAX_MB` (default 10 MB).

## Listing

The library screen toggles between grid and list views. You can also filter by filename.

The list view exposes filename, size, and dimensions (for images). The same library powers both the entry editor's file picker and the OG card background picker. The OG picker only shows images.

## Copying file URLs

Copy a URL to paste into entries or templates. The inserted snippet depends on the file kind:

- Image … `<img src="..." alt="">` / `![alt](...)`
- Audio … `<audio controls src="..."></audio>` / link
- Movie … `<video controls src="..."></video>` / link
- Document … `<a href="..." download>filename</a>` / link

## Alt text

You can attach alt text to an image. When the AI writing assist is configured, alt text suggestions can be generated automatically after upload.

Alt text is read out by screen readers and shown when an image fails to load. Provide a short description for any meaningful image.

## Deleting

Deleting a file removes it from disk. Entries and templates that referenced it will show a broken link until you fix them.

Before deleting, double-check whether the file is in use.

## Static rebuild interaction

When you run a static rebuild, uploaded files are copied to the output directory. Static deployments serve entry images and OG card images directly from there.

## Related pages

- [Writing and managing entries](entries)
- [Publishing settings and OG cards](settings-publishing)
