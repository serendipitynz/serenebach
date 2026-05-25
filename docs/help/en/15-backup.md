---
title: Backup
slug: backup
order: 15
---

# Backup

All Serene Bach data can be exported into a single ZIP archive.

## Running from the CLI

```bash
./serenebach backup --out ./backup-2026-05-23.zip
```

## Options

| Flag | Default | Description |
|---|---|---|
| `--out <path>` | `backup-YYYY-MM-DD-HHMMSS.zip` | Output path (`-` for stdout) |
| `--include-analytics` | off | Include analytics / MCP audit DBs (only when stored separately) |
| `--include-public` | off | Include static rebuild output |
| `--exclude <names>` | (none) | Omit `images` and/or `templates` |
| `--quiet` | off | Suppress progress output |

## Archive contents

```
backup-2026-05-23-093045.zip
├── manifest.json
├── db/
│   ├── serenebach.db        ← Consistent snapshot via VACUUM INTO
│   ├── analytics.db         ← Only with --include-analytics + separate file
│   └── mcp_audit.db         ← Same as above
├── img/                     ← Uploaded images
├── templates/               ← Template assets
└── public/                  ← Only with --include-public
```

## Restoring

There is no `restore` subcommand yet. Follow these manual steps:

1. Unzip the archive
2. Place `db/serenebach.db` at the desired location
3. Copy `img/`, `templates/`, and `public/` as needed
4. Run `./serenebach migrate`

## Security

- The ZIP file is created with `0o600` permissions
- When running under CGI, `--out` must be given explicitly
