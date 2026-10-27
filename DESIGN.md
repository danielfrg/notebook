---
name: Notebook
description: Quiet Markdown editing with compact navigation and a readable document canvas.
colors:
  light-bg-primary: "#fafafa"
  light-bg-surface: "#ffffff"
  light-bg-surface-hover: "#e5e5e8"
  light-bg-inset: "#f0f0f2"
  light-border-default: "#e1e1e5"
  light-border-muted: "#ebebee"
  light-text-primary: "#252529"
  light-text-secondary: "#56565e"
  light-text-muted: "#6b6b75"
  light-accent-blue: "#0969da"
  light-accent-amber: "#b45309"
  light-accent-purple: "#6b5fc7"
  light-accent-red: "#c62828"
  dark-bg-primary: "#202123"
  dark-bg-surface: "#252628"
  dark-bg-surface-hover: "#303136"
  dark-bg-inset: "#191a1c"
  dark-border-default: "#303136"
  dark-border-muted: "#2a2b2e"
  dark-text-primary: "#e9e9ed"
  dark-text-secondary: "#b8b8c0"
  dark-text-muted: "#9797a2"
  dark-accent-blue: "#75b7ff"
  dark-accent-amber: "#f0a64a"
  dark-accent-purple: "#a995f5"
  dark-accent-red: "#f27474"
typography:
  interface:
    fontFamily: "Inter, sans-serif"
    fontSize: "0.875rem"
    lineHeight: 1.5
  prose:
    fontFamily: "Inter, sans-serif"
    fontSize: "1rem"
    lineHeight: 1.7
  technical:
    fontFamily: "IBM Plex Mono, monospace"
rounded:
  sm: "5px"
  md: "6px"
  lg: "10px"
spacing:
  space-1: "2px"
  space-2: "4px"
  space-3: "6px"
  space-4: "8px"
  space-5: "12px"
  space-6: "16px"
---
# Design System: Notebook

## Overview

Notebook follows Linear's in-app visual direction: charcoal surfaces in dark mode, soft neutral surfaces in light mode, compact navigation, subtle borders, and restrained blue accents. The note carries the interface. Keep kit-ui and the two-column shell; use the vault name as sidebar identity and the note breadcrumb and title as document context.

## Colors

The frontmatter records the current light and dark palette from `frontend/src/theme.css`; CSS remains the implementation source of truth. Mode prefixes identify snapshots, not new CSS variables. The remaining semantic accents and overlay colors live in that same theme file.

Use `--bg-primary` for the app, sidebar, and reading pane; `--bg-inset` for inset controls; and `--bg-surface` for floating surfaces. The app is a continuous surface separated by a quiet sidebar divider. Use muted borders for quiet separation and secondary or muted text for navigation context. The `--accent-blue` token supplies blue links, selections, carets, and focus states through `--nb-accent`. Use amber for unsaved or attention states and the semantic error token for errors. Active file rows use a neutral fill.

`theme.css` is the only implementation file with palette values. It overrides kit-ui tokens and must remain replaceable as a whole with the shared homelab theme (for example, `golinks/frontend/src/theme.css`) without other edits. Keep generic kit-ui tokens swappable; sibling apps need no palette changes. Application layout and interaction rules belong in `frontend/src/app.css`; editor styling consumes semantic tokens plus `--nb-*` tokens defined there with generic kit-ui fallbacks.

## Typography

Load Inter and IBM Plex Mono from Google Fonts with `display=swap`; do not embed font binaries in the repository or production assets. Inter serves interface text, vault identity, note titles, and note prose. IBM Plex Mono serves code and technical data. The wordmark uses normal case and the interface font.

The interface base is 14px, compact navigation and titles use 13px, and metadata uses 11–12px. Prose uses 16px with a 1.7 line height. Live Preview headings use semibold weight and a 1.3 line height; the first heading is 1.75em with slightly tighter tracking. Preserve kit-ui's larger handheld type scale so inputs do not zoom on iOS.

## Layout

Use a resizable, collapsible left sidebar for the vault switcher, search, and file tree. Its initial width is 264px; respect the user's persisted `notebook-sidebar` width (accepted range 200–600px). The document pane has no outer frame, inset margin, or rounded corners. The sidebar search and action row aligns with the 48px document header. Keep 12px horizontal insets around sidebar controls and file rows, and anchor the vault selector in a bottom footer.

Center the editor and its quiet formatting strip within a maximum 760px column. Keep grouped formatting controls near the document, with history actions at the end. Desktop tree rows have a 32px minimum height and 14px depth indentation; coarse-pointer rows have a 40px minimum. Keep library control sizes rather than scaling every control to match tree density.

At the 900px sidebar breakpoint and below, the sidebar overlays the document. At 640px and below, breadcrumb context moves to a separate mobile row, Live Preview shortens to Live, and the save label becomes an accessible compact status dot. Between 641px and 1100px, omit the parent breadcrumb to preserve room for the title and controls. Phone layouts must not widen the page. Panes scroll inside the viewport-height shell; preserve `scrollbar-gutter: stable`.

## Elevation & Depth

Use tonal layering and subtle one-pixel dividers. Controls stay flat and stationary: raised, sunken, and panel shadows are disabled, and press transforms are disabled. Floating menus and the overlay sidebar may use kit-ui's theme shadow tokens; do not add decorative shadows to the document or navigation.

## Shapes

Use the small radius for quiet action triggers, code and tags; the medium radius for standard controls and code-block corners; and the large radius for file rows and the quick palette. Their current values are in the frontmatter. Status dots and list bullets remain circular.

## Components

Use Svelte 5 runes, typed props, and callback props. Import shared controls directly from `@kenn-io/kit-ui`, including sidebar, top bar, inputs, segmented mode control, icon buttons, theme toggle, dialogs and menus. Keep local components for app-specific behavior; avoid pass-through wrappers.

- **Navigation:** the vault selector sits in the bottom footer; omit the logo and favicon. Place an icon-only plus menu beside the search field for creating notes and folders, with the sidebar collapse button immediately to its right. Keep the plus button transparent and omit the Files section header. Search has one outer border, which turns blue on focus. An empty search field shows the tree; text shows full-text results. File rows show names only, with a chevron for expandable folders and a neutral selected fill. Omit file icons and metadata. Reveal file actions on hover or keyboard focus; keep them available on phones. Distinguish loading, an empty vault, no results, and request failures.
- **Document header:** show parent-folder breadcrumb and note title without repeating the vault name. Place save state before the top-right mode, actions, and theme controls. The title truncates within the available width. Reserve a stable 116px save-status slot on desktop so changing labels cannot move controls; on phones retain a 12px status-dot slot and accessible label. Save state includes text alongside a small dot; conflicts and errors remain accessible status messages. Avoid explanatory interface copy unless it helps resolve a problem.
- **Focus and appearance:** give fields one visible focus treatment, preserve control focus rings and accessible labels, and keep pressed controls stationary. The editing surface is identified by its caret; suppress the pane-sized CodeMirror focus outline within the app. Start in dark mode and respect explicit light or system preferences. The header ThemeToggle stores `notebook-theme`; system mode follows the operating system. Respect reduced motion, including a steady editor cursor.
- **Editor:** Live Preview renders notes immediately when opened, including frontmatter properties. Reveal Markdown markers on the element or line touched by the cursor only while the editor is focused; an unfocused selection does not reveal source. Source mode uses the same editor with decorations off and syntax highlighting on. Tables remain raw text. Remote images show as links and are never loaded. Preserve Obsidian syntax, frontmatter, line endings, and BOM; the editor document is exactly the file text and never reformats untouched content.
- **Saving and recovery:** save automatically one second after the last edit, on blur, and on `Mod-s`, with header status. Warn before leaving with unsaved changes. Refresh the open note and file list while the tab is visible. Reload an external change silently when the buffer is clean; with unsaved edits, show a conflict banner offering Load disk version or Overwrite. Saves carry the loaded revision so external changes are never silently overwritten.
- **Routes and shortcuts:** hash routes are `#/<vault>/<path>`; there are no tabs, and browser history is the back stack. Preserve `Mod-o` quick switcher, `Mod-p` commands, `Mod-s` save, `Mod-e` mode, and `Mod-Shift-f` search.

## Do's and Don'ts

- Do use semantic tokens and import shared controls directly from kit-ui.
- Do keep the note readable, save state visible, and keyboard focus clear.
- Do preserve desktop and phone layouts, recoverable errors, and the shared-theme replacement contract.
- Don't add palette literals to application or editor styles.
- Don't use monospace or uppercase styling for the wordmark or note title.
- Don't add decorative shadows, press movement, duplicate field focus treatments, or a pane-sized editor focus ring.
- Don't reformat Markdown or alter text the user did not edit.
