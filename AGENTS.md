# Agent Instructions

Notebook is a self-hosted multi-vault Markdown notes app.
It builds one Go binary that serves a Huma HTTP API, an MCP endpoint, and an
embedded Svelte 5 frontend whose CodeMirror 6 editor reads and writes the notes
straight from the vault directories.

Read `PRODUCT.md` for user intent and `DESIGN.md` before changing the frontend.

The `README.md` and `justfile` own setup and usage.
They are written for humans: avoid big restructures, but update them when changes are user facing.

## Architecture

- Go for the API in /cmd and /internal.
- TypeScript and Svelte 5 for the frontend in /frontend.
- Should build into a single binary with an embedded frontend.
- `internal/vault` owns all file access and the in-memory search snapshot;
  `internal/httpapi` and `internal/mcp` are thin adapters over it, and
  `internal/web` embeds `frontend/dist`.

## Behavior Invariants

- The Markdown files are the source of truth. There is no database, persistent
  index, embeddings, or note cache; search uses an in-memory snapshot refreshed by
  stat (size, mtime) at most every 2 seconds.
- Never rewrite text the user did not edit. Opening a note and saving it without
  edits must not change the file: the editor document is exactly the file text,
  the line separator (LF or CRLF) and BOM are preserved, and content is never
  truncated or re-encoded. A parser or rendering limit may look raw; it must never
  alter the file.
- Writes are atomic and revisioned. The revision is the lowercase hex SHA-256 of
  the file bytes; saving requires the revision the editor loaded, and a mismatch is
  a conflict that writes nothing. Write a hidden temp file in the same directory,
  sync it, then rename it over the target. Rename and move never overwrite; delete
  moves the entry to `.trash/` inside the vault.
- Any path component starting with `.` is hidden and inaccessible, and symlinks are
  never listed or followed. File access stays confined to the vault root. A note is
  a UTF-8 `*.md` file of at most 2 MiB.
- Mutating API requests need `X-Notebook: 1`, a same-host `Origin` if present,
  `application/json` when they have a body, and at most 4 MiB.
  `NOTEBOOK_READONLY=true` answers 405 to all of them.
- MCP is read-only by default. Write tools exist only with `NOTEBOOK_MCP_WRITE=true`
  and never in read-only mode, and they use the same validation as the HTTP API.
  `NOTEBOOK_MCP_EXCLUDE` hides paths from every MCP tool; excluded paths behave as
  not found. Errors are short and never expose filesystem paths.

## Frontend Conventions

`DESIGN.md` owns the theme, typography, layout, and interaction rules; read it before changing the frontend.
`frontend/src/theme.css` is the only file with palette values.
