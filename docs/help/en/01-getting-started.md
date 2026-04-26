---
title: Getting started
slug: getting-started
order: 10
---

# Getting started

Serene Bach runs as a single executable backed by a SQLite database. You can run it as a long-lived server on a VPS, or as a CGI program on shared hosting that supports CGI.

## Requirements

- Linux, macOS, or Windows
- A directory the binary can write to (database + uploaded images)
- A web server with CGI execution enabled, if you intend to run it as a CGI program

When using a pre-built binary, no separate Go / Python / Ruby runtime is required.

## Initial setup

The first time you run Serene Bach, create the admin user and the default template in the database:

```bash
./serenebach seed
```

The initial user is `admin` with password `changeme`. **Change the password before going public.**

To start with an empty blog (no sample entries), run:

```bash
SB_SEED_NO_SAMPLES=1 ./serenebach seed
```

## Running as a server

```bash
./serenebach --addr=:8080 serve
```

Open `http://localhost:8080/admin/login` in your browser to reach the admin UI. The public site is at `http://localhost:8080/`.

Use `--db` to point at a specific database file:

```bash
./serenebach --db=/var/lib/serenebach/blog.db --addr=:8080 serve
```

## Running as CGI

The same binary can be deployed as a CGI program. Enable CGI on your web server and make the binary executable:

```bash
chmod +x serenebach.cgi
```

When invoked through CGI, Serene Bach detects the environment automatically and processes one request per invocation.

## Writing your first entry

1. Log in to the admin UI.
2. Click **New Entry** in the left sidebar.
3. Enter a title and body.
4. Set the category, tags, and posted date as needed.
5. Set the status to **Published** and save.

Saved entries appear immediately on the public site. Click **Public site** at the top of the sidebar to view it.

## Next pages

- [Writing and managing entries](entries)
- [Template editing](templates)
- [Publishing settings and OG cards](settings-publishing)
