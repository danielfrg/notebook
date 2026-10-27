# Task 13 implementation report

## Status

Implemented Live Preview replacements for bullets, task checkboxes, horizontal rules, local images, and frontmatter properties. Replaced the placeholder wikilink with an interactive anchor widget. The existing 500,000 character guard, state field recomputation, safe comment filtering, and wikilink label rules remain in place.

## Changed files

- `frontend/src/lib/editor/widgets.ts` (new): `LivePreviewWidget`, `toggleTaskAt`, and `taskToggleExtension`. Task clicks change one checkbox character. Properties clicks and Enter/Space move the cursor into the original fenced range. Wikilink clicks emit a bubbling `notebook:wikilink` event with the source range and text.
- `frontend/src/lib/editor/livePreview.ts`: collectors for list markers, tasks, rules, image syntax and embeds, and frontmatter; widgets are built as CodeMirror replacement decorations from the state field. Frontmatter uses a block replacement. Local assets must resolve and have an image extension; remote, missing, and non-image targets remain visible text with `nb-link`.
- `frontend/src/editor.css`: token-based presentation for bullets, checkboxes, checked task text, rules, images, properties, and list values. Existing link, tag, quote, and list styles were reused.
- `frontend/src/lib/editor/livePreview.test.ts`: new coverage for every requested decoration, the task toggle in LF and CRLF documents, the properties block replacement, and widget equality/event handling; updated Task 12 placeholder expectations.

## RED and GREEN evidence

After adding the requested tests and before implementing behavior, `pnpm --dir frontend exec node --test src/lib/editor/livePreview.test.ts` reported 27 tests: 22 passed, 5 failed. The five failures were the new bullet/task/rule, image, frontmatter, toggle, and widget tests. The initial missing-module and TypeScript strip syntax errors in the test scaffolding were corrected before recording this behavioral RED run.

Final command: `pnpm --dir frontend test && pnpm --dir frontend check` passed: 133 tests, 0 failures; `svelte-check` reported 0 errors and 0 warnings. `git diff --check` passed.

## Browser and layout checks

No browser check was run. The current app does not mount `livePreview()` or supply `previewContext` in a screen, so a browser session would not exercise these widgets without adding a temporary page. I inspected collector output for nested lists, a quote inside a list, and a list inside a quote. Task indentation now counts through the checkbox marker, so wrapped task text aligns under task text; the existing combined `.nb-quote.nb-list` CSS rule covers a quoted list marker. The quote inside a list receives `nb-quote` on its own lines and keeps the original indentation in source.

## Remaining concerns

- The app has not yet connected the `notebook:wikilink` event to note navigation; the widget is clickable and exposes the source target for the host to handle.
- Pixel-level appearance of wrapped and nested quote/list combinations remains unverified until Live Preview is mounted in the app. The parser and decoration positions were inspected, but no browser rendering was available for this isolated task.
