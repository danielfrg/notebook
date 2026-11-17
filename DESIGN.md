---
name: Notebook
description: Quiet Markdown editing with compact navigation and a readable document canvas.
---
# Design System: Notebook

## Overview

The note carries the interface. Notebook is a two-column shell: a resizable, collapsible sidebar with the vault selector and file tree, and the document with its breadcrumb, title, and formatting strip. The look is the shared Cadence theme on ui-sdk; this file records only what is specific to Notebook.

## Theme and the shared design system

`frontend/src/design-system/` is synced from the shared design-system repo (theme tokens and components, plus the `theme-toggle` component) and is never edited by hand. A change to it is made in that repo, synced, and then committed here.

- `main.ts` imports the synced `theme/tokens.css`, `theme/components.css`, and each used custom component's CSS first, then `app.css`, then `editor.css`, so app rules win ties with the theme.
- `app.css` starts with `@layer ui-sdk;` and holds layout and Notebook-specific pieces. It may adjust a control's layout (width, spacing), never its look. It loads after the theme, so it wins ties with theme rules; prefer tokens and class selectors over `!important`.
- Use tokens (`--ink`, `--muted`, `--line`, `--space-*`, `--radius-*`, `--mono`), never palette literals in app or editor styles. The editor reads semantic tokens plus the `--nb-*` tokens defined in `app.css`.
- Use ui-sdk components for controls (`Button`, `Input`, `Menu`, `SegmentedControl`, `Toolbar`, `Dialog`, `AlertDialog`, `Command`, `Drawer`, `Resizable`, `Toast`). Do not hand-roll a `role=` pattern the library ships. Local components (`Modal`, `Spinner`, `Kbd`) cover only what it does not.
- Fonts are the system stack and `--mono`; `--mono` is for code and identifiers. Never load web fonts or embed font binaries.

## Typography

The interface uses the theme's 13px base, metadata 12px. Note prose is 16px with a 1.7 line height. Live Preview headings use semibold weight and a 1.3 line height; the first heading is 1.75em with slightly tighter tracking. The wordmark and note title use the interface font in normal case.

## Layout

Use a resizable, collapsible left sidebar for the vault menu, header actions, and file tree. Its initial width is 272px; respect the user's persisted `notebook-sidebar` width (accepted range 200–600px). The sidebar header aligns with the compact 44px document header. Keep 12px horizontal insets around sidebar controls and file rows.

Center the editor and its formatting strip within a maximum 760px column. Keep grouped formatting controls near the document, with history actions at the end. Desktop tree rows have a 32px minimum height and 14px depth indentation; coarse-pointer rows have a 40px minimum.

At 900px and below the sidebar is a drawer over the document. At 640px and below, breadcrumb context moves to a separate mobile row, Live Preview shortens to Live, and the save label becomes an accessible compact status dot. Between 641px and 1100px, omit the parent breadcrumb to preserve room for the title and controls. Phone layouts must not widen the page. Panes scroll inside the viewport-height shell; preserve `scrollbar-gutter: stable`.

## Elevation and shape

Use tonal layering and subtle one-pixel dividers. Do not add decorative shadows to the document or navigation, and do not give a panel both a border and a shadow. Floating menus, dialogs, and toasts use the theme's own surfaces.

## Components

Use Svelte 5 runes, typed props, and callback props. Keep local components for app-specific behavior; avoid pass-through wrappers. Dialogs use the local `Modal` wrapper over ui-sdk `Dialog`, with the parent owning the open state; destructive confirmations use `AlertDialog`. Keyboard shortcuts live in `lib/shortcuts.ts` (combo parsing, a scope stack so an open palette suspends the app shortcuts, and key labels).

- **Navigation:** the sidebar header puts the vault menu (bold name with up/down chevrons) on the left, and on the right ghost icon buttons for search (opens the Mod-k palette), new note, new folder, the sort toggle (name tree or recently edited list grouped by Today, Yesterday, Previous 7 days, then month), and hide. Omit the logo and favicon. File rows show names only, with a chevron for folders and an aligned spacer for notes; the row menu is a small vertical ellipsis, hidden until hover or keyboard focus and always visible on phones. The sidebar full-text search field appears only when opened (Mod-Shift-f). With no note open, the start page offers New note, Search, and recent notes. Distinguish loading, an empty vault, no results, and request failures.
- **Document header:** Back and Forward (session-only, `Mod-[` / `Mod-]`), then the parent-folder breadcrumb and the note title, which is edited in place: it looks like text, Enter or leaving it renames the note, and Escape restores it. Place the mode control, the actions menu, and the theme toggle at the top right. The title truncates within the available width. Save state is a small light at the right of the formatting strip, before Copy as Markdown: muted when saved, strong when unsaved, accent while saving, and the danger color in conflict or error; its word is the tooltip and accessible name. Avoid explanatory interface copy unless it helps resolve a problem.
- **Focus and appearance:** use the theme's focus treatment on controls. The editing surface is identified by its caret; suppress the pane-sized CodeMirror focus outline within the app. Start in dark mode and respect an explicit light or system choice. The header theme toggle uses the shared `color-scheme` key; system mode follows the operating system. Respect reduced motion, including a steady editor cursor.
- **Notifications:** show toasts at the top center with a message and a dismiss control. Only expose actions backed by actual app behavior; do not add a visual-only Undo to note deletion.
- **Editor:** Live Preview renders notes immediately when opened, including frontmatter properties as a panel of 12px rows: a type icon, an editable name, a value control (text, date, checkbox, or removable chips for lists), and an × to delete the property on hover, with + Add property below. Each control rewrites only its own text in the frontmatter; properties the panel cannot read stay hidden and untouched, and Source mode shows the raw YAML. The formatting strip ends with Copy as Markdown; a dashed line appears under it once the note scrolls. A quiet bottom bar shows the word count at the right. Reveal Markdown markers on the element or line touched by the cursor only while the editor is focused; an unfocused selection does not reveal source. Source mode uses the same editor with decorations off and syntax highlighting on. Tables remain raw text. Remote images show as links and are never loaded. Preserve Obsidian syntax, frontmatter, line endings, and BOM; the editor document is exactly the file text and never reformats untouched content. The property panel's own inputs and buttons are reset in `editor.css` with raised specificity, because the theme styles bare controls.
- **Saving and recovery:** save automatically one second after the last edit, on blur, and on `Mod-s`, with header status. Warn before leaving with unsaved changes. Refresh the open note and file list while the tab is visible. Reload an external change silently when the buffer is clean; with unsaved edits, show a conflict banner offering Load disk version or Overwrite. Saves carry the loaded revision so external changes are never silently overwritten.
- **Routes and shortcuts:** hash routes are `#/<vault>/<path>`; there are no tabs. Back and Forward keep their own in-memory trail for the session. `Mod-k` opens the search palette (fuzzy note names, full-text Content results with highlighted snippets, and commands including switching vault; Escape always closes it), and `Mod-o` / `Mod-p` open it too. Keep `Mod-s` save, `Mod-e` mode, and `Mod-Shift-f` sidebar search.

## Do's and Don'ts

- Do use semantic tokens and ui-sdk components.
- Do keep the note readable, save state visible, and keyboard focus clear.
- Do preserve desktop and phone layouts and recoverable errors.
- Don't edit `frontend/src/design-system/` or add palette literals to application or editor styles.
- Don't use monospace or uppercase styling for the wordmark or note title.
- Don't add decorative shadows, press movement, or a pane-sized editor focus ring.
- Don't reformat Markdown or alter text the user did not edit.
