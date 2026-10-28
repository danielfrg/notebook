---
name: Notebook
description: Quiet Markdown editing with compact navigation and a readable document canvas.
colors:
  light-bg-primary: "#ffffff"
  light-bg-surface: "#ffffff"
  light-bg-surface-hover: "#ebebed"
  light-bg-inset: "#f2f2f3"
  light-border-default: "#dddddf"
  light-border-muted: "#e7e7e9"
  light-text-primary: "#25262a"
  light-text-secondary: "#44464c"
  light-text-muted: "#6a6d75"
  light-accent-blue: "#7965ce"
  light-accent-amber: "#b45309"
  light-accent-purple: "#7965ce"
  light-accent-red: "#c62828"
  dark-bg-primary: "#1f2023"
  dark-bg-surface: "#25262b"
  dark-bg-surface-hover: "#303136"
  dark-bg-inset: "#232428"
  dark-border-default: "#383a3f"
  dark-border-muted: "#2b2d32"
  dark-text-primary: "#f4f4f5"
  dark-text-secondary: "#e5e6ea"
  dark-text-muted: "#9c9da1"
  dark-accent-blue: "#7965ce"
  dark-accent-amber: "#f0a64a"
  dark-accent-purple: "#7965ce"
  dark-accent-red: "#f27474"
typography:
  interface:
    fontFamily: "Inter, sans-serif"
    fontSize: "0.8125rem"
    lineHeight: 1.5
  prose:
    fontFamily: "Inter, sans-serif"
    fontSize: "1rem"
    lineHeight: 1.7
  technical:
    fontFamily: "IBM Plex Mono, monospace"
rounded:
  sm: "6px"
  md: "8px"
  lg: "12px"
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

Notebook follows the Linear-inspired component gallery approved in October 2026: charcoal surfaces in dark mode, soft neutral surfaces in light mode, compact navigation, a rounded document frame, pill actions, and neutral floating menus. The note carries the interface. Keep kit-ui and the two-column shell, with the vault selector in the sidebar and note breadcrumb and title as document context.

## Colors

The frontmatter records the current light and dark palette from `frontend/src/theme.css`; CSS remains the implementation source of truth. Mode prefixes identify snapshots, not new CSS variables. The remaining semantic accents and overlay colors live in that same theme file.

Use `--bg-sidebar` for the shell and sidebar, falling back to `--bg-primary`; the document uses `--bg-primary`. Use `--bg-inset` for fields and optional `--bg-popover` / `--bg-notification` tokens for floating surfaces, falling back to `--bg-surface`. Menus use a lighter neutral surface with inset selected rows and trailing checks. Muted borders and text provide quiet separation and navigation context. `--brand-accent` is the single editable brand color; the generic `--accent-blue` and `--accent-purple` aliases carry it into existing controls. The optional `--text-accent` derives a readable text tint from the same brand; the editor consumes it through `--nb-accent`, falling back to `--accent-blue`. Use amber for attention and semantic red for errors. Active file rows use a neutral fill.

`theme.css` is the only implementation file with palette values. It overrides generic tokens and must remain replaceable as a whole with the shared homelab theme (for example, `golinks/frontend/src/theme.css`) without other edits. Keep optional tokens behind generic kit-ui fallbacks; sibling apps need no palette changes. Reusable kit-ui presentation belongs in `frontend/src/components.css`, shell layout in `frontend/src/app.css`, and editor styling consumes semantic tokens plus the `--nb-*` tokens defined there.

## Typography

Load variable Inter (100–900) and IBM Plex Mono from Google Fonts with `display=swap`; do not embed font binaries in the repository or production assets. Inter serves interface text, vault identity, note titles, and note prose. IBM Plex Mono serves code and technical data. The wordmark uses normal case and the interface font.

The interface and navigation use 13px, action labels use 12px/500, and metadata uses 11–12px. Prose uses 16px with a 1.7 line height. Live Preview headings use semibold weight and a 1.3 line height; the first heading is 1.75em with slightly tighter tracking. Preserve kit-ui's larger handheld type scale so inputs do not zoom on iOS.

## Layout

Use a resizable, collapsible left sidebar for the vault switcher, search, and file tree. Its initial width is 244px; respect the user's persisted `notebook-sidebar` width (accepted range 200–600px). The document pane has a quiet one-pixel border, 12px corners, and an 8px inset from the window. The sidebar search and action row aligns with the compact 44px document header. Keep 12px horizontal insets around sidebar controls and file rows, and anchor the vault selector in a bottom footer.

Center the editor and its quiet formatting strip within a maximum 760px column. Keep grouped formatting controls near the document, with history actions at the end. Desktop tree rows have a 32px minimum height and 14px depth indentation; coarse-pointer rows have a 40px minimum. Keep library control sizes rather than scaling every control to match tree density.

At the 900px sidebar breakpoint and below, the sidebar overlays the document. At 640px and below, breadcrumb context moves to a separate mobile row, Live Preview shortens to Live, and the save label becomes an accessible compact status dot. Between 641px and 1100px, omit the parent breadcrumb to preserve room for the title and controls. Phone layouts must not widen the page. Panes scroll inside the viewport-height shell; preserve `scrollbar-gutter: stable`.

## Elevation & Depth

Use tonal layering and subtle one-pixel dividers. Controls stay flat and stationary: raised, sunken, and panel shadows are disabled, and press transforms are disabled. Floating menus and the overlay sidebar may use kit-ui's theme shadow tokens; do not add decorative shadows to the document or navigation.

## Shapes

Action buttons and mode buttons use full pill corners; 28px icon utilities are circular. Fields and compact 30px select triggers use 8px corners. Menus and the document frame use 12px corners; dialogs use 14px corners. File rows, inline code, and tags use the small radius. Status dots and list bullets remain circular.

## Components

Use Svelte 5 runes, typed props, and callback props. Import shared controls directly from `@kenn-io/kit-ui`, including sidebar, top bar, inputs, segmented mode control, icon buttons, theme toggle, dialogs and menus. Keep local components for app-specific behavior; avoid pass-through wrappers. The vault selector uses a placement action to align its selected 32px option row with the trigger while retaining kit-ui selection, dismissal, and keyboard behavior. The notifications renderer reads the real kit-ui flash store, so expiry and dismissal remain owned by the library.

- **Navigation:** the vault selector sits in the bottom footer; omit the logo and favicon. Place a circular icon-only plus menu beside the search field for creating notes and folders, with the circular sidebar collapse button immediately to its right. Keep the plus button transparent and omit the Files section header. Search has one outer border, which takes the accent on focus. An empty search field shows the tree; text shows full-text results. File rows show names only, with a chevron for expandable folders and a neutral selected fill. Omit file icons and metadata. Reveal file actions on hover or keyboard focus; keep them available on phones. Distinguish loading, an empty vault, no results, and request failures.
- **Document header:** show parent-folder breadcrumb and note title without repeating the vault name. Place save state before the top-right mode, actions, and theme controls. The title truncates within the available width. Reserve a stable 116px save-status slot on desktop so changing labels cannot move controls; on phones retain a 12px status-dot slot and accessible label. Save state includes text alongside a small dot; conflicts and errors remain accessible status messages. Avoid explanatory interface copy unless it helps resolve a problem.
- **Focus and appearance:** fields use only one colored border, with no focus ring or shadow. Invalid fields retain their red border. Preserve visible keyboard focus and accessible labels on other controls, and keep pressed controls stationary. The editing surface is identified by its caret; suppress the pane-sized CodeMirror focus outline within the app. Start in dark mode and respect explicit light or system preferences. The header ThemeToggle stores `notebook-theme`; system mode follows the operating system. Respect reduced motion, including a steady editor cursor.
- **Notifications:** show compact neutral toasts at the top center, with a Lucide status icon, message, and dismiss control. Keep the kit-ui store's expiry and stacking behavior. Only expose actions backed by actual app behavior; do not add a visual-only Undo to note deletion.
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
