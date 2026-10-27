# Design System

Notebook is a simple self-hosted application.

The interface should use a small consistent vocabulary rather than
one-off styling for each new control.

## Component Rules

`frontend/src/theme.css` is the only file with palette values: docbank-inspired
token overrides of kit-ui. Replacing it with the shared theme of the other
self-hosted apps must restyle the app without other edits, so keep colors and
other theme values out of every other file.
Application layout and interaction rules belong in `frontend/src/app.css`.
Editor-specific styling uses semantic tokens plus `--nb-*` tokens, which
`app.css` defines with fallbacks to generic kit-ui tokens.

- Use Svelte 5 runes, typed props, and callback props.
- Keep desktop and phone layouts, keyboard focus, and recoverable error states.
- Load Inter and IBM Plex Mono from Google Fonts with `display=swap`. Do not
embed font binaries in the repository or production assets.
- Inter is for interface text and note prose; IBM Plex Mono is for the wordmark,
code, and technical data. Use neutral surfaces, subtle one-pixel borders,
rounded controls, and no decorative shadows.
- Preserve `scrollbar-gutter: stable`.
- Import shared controls directly from `@kenn-io/kit-ui`.
- Use semantic theme tokens and keep the library's default control sizes.
- Keep local components for app-specific behavior, not pass-through wrappers.
- Give fields one visible focus treatment and keep pressed controls stationary.

## Appearance and Accessibility

Start in dark mode and respect explicit light or system preferences. The header
ThemeToggle saves the choice under `notebook-theme`. System mode follows the
operating system. Keep focus visible, controls labeled, and reduced-motion
preferences respected. Phone layouts must not widen the page.

## Application Patterns

Use a resizable, collapsible left sidebar for the vault switcher, search box, and
file tree. The main pane holds the breadcrumb, formatting toolbar, mode toggle,
save status, actions menu, and theme toggle above the editor, which sits in a
centered column no wider than 760px. Routes are hash routes, `#/<vault>/<path>`;
there are no tabs, and the browser history is the back stack. An empty search box
shows the tree; text shows full-text results.

Live Preview hides Markdown markers except on the element or line the cursor
touches, like Obsidian. Source mode is the same editor with decorations off and
syntax highlighting on. Tables stay raw text, and remote images are never loaded:
they show as links. The editor never reformats text; its document is exactly the
file.

Saving is automatic: one second after the last edit, on blur, and on `Mod-s`, with
the status in the header. Unsaved changes warn before the page is left. The open
note and the file list refresh on their own while the tab is visible. An external
change reloads silently when the buffer is clean; with unsaved edits a conflict
banner offers Load disk version or Overwrite.

Shortcuts: `Mod-o` quick switcher, `Mod-p` commands, `Mod-s` save, `Mod-e` mode,
`Mod-Shift-f` search.
