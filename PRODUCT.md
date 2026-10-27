# Product

## Users

People who keep notes as plain Markdown in Obsidian-style vaults and want to read
and edit them from a desktop browser or phone, and to let their agents read them.

Assumes deployment on a trusted network; there is no auth.

## Product Purpose

Open any note in any configured vault, edit it in a Live Preview editor, and have
every change written straight back to its `.md` file.

Keep the files the source of truth so Obsidian, Syncthing, and agents can keep
working on the same vaults.

Success means finding a note quickly, editing it in place, and finding the file on
disk exactly as expected: only the text that was edited has changed.

## Brand Personality

Quiet, focused, and calm. The note carries the interface.

### Tone

Do not be overly explicit about explaining every part of the UI
with text. It should be intuitive. Only add explanatory messages when it's needed,
such as a change on disk that conflicts with an edit or a note that cannot be edited.

## Anti-references

Avoid accounts and sharing flows, a database or index that competes with the files,
plugin systems, graph views, promotional dashboards, and interface text that
explains every control.

## Design Principles

- Treat the Markdown files as the source of truth; the editor's document is exactly the file text.
- Never rewrite text the user did not edit. Opening and saving a note without
  edits leaves the file unchanged; a rendering limit may show something raw but
  never alters it.
- Display Obsidian syntax correctly and keep it intact: wikilinks, embeds, tags,
  highlights, task lists, frontmatter, line endings, and BOM.
- Stay safe next to other writers (Syncthing, Obsidian, agents): a save carries
  the revision the editor loaded, and a mismatch is an explicit conflict, never a
  silent overwrite.
- Keep the file tree and search one step from the note. Distinguish loading, an
  empty vault, no search results, and request failures.
- Preserve unsaved edits and warn before the page is left.

## Agent Access

Agents use typed MCP tools to list, search, and read notes across vaults. MCP is
read-only by default: the write tools (`create_note`, `update_note`,
`append_to_note`) exist only when enabled and never in read-only mode, and they
follow the same path and revision rules as the HTTP API. Configured excludes hide
paths, such as recorded conversations, from every tool; excluded notes behave as
not found.
