# Task 13 fix round 1 report

## Result

Implementation commit: `aee5c377ed399de79d2e5029b5677cad59104f3c` (`fix(editor): preserve live preview task and link interactions`; includes `Co-Authored-By: OpenAI Codex <noreply@openai.com>`).

- Task traversal now visits inline children after recording the task widget, so strong text, tags, and wikilinks inside tasks are decorated. Checked styling spans task continuation lines.
- Widget equality includes source offsets. Task, wikilink, and properties pointer actions run on `mousedown` before CodeMirror changes selection. Task and wikilink actions derive their current source positions with `view.posAtDOM` and the syntax tree; keyboard-generated clicks still activate tasks and links. Task toggles use `input.toggle`.
- Markdown images resolve angle-wrapped local paths and `%20` spaces through `resolveAsset`; remote targets remain plain link text. Frontmatter with no displayable properties stays raw and editable instead of showing an empty panel.
- Initial selection/frontmatter cursor policy was left for Task 14. No theme or layout files changed. All fixtures were synthetic.

## Test-first evidence

All commands ran from `/Users/danrodriguez/code/danielfrg/notebook`. The focused command form was `pnpm --dir frontend exec node --test --test-name-pattern='PATTERN' src/lib/editor/livePreview.test.ts`.

| Regression | RED before production change | GREEN after change |
| --- | --- | --- |
| Task inline children and checked continuation | `PATTERN='task text keeps|checked task text styling'`: 0/2 passed; missing `nb-strong` and continuation mark | Same command: 2/2 passed |
| Local image paths with spaces | `PATTERN='local markdown image targets'`: 0/1 passed; angle-wrapped path remained `nb-link` | Same command: 1/1 passed |
| Empty frontmatter | Initial `PATTERN='empty frontmatter stays'`: 0/1 passed; empty properties widget. A second focused RED after adding comment-only YAML also showed `widget 0-17` instead of raw lines | Same command after each fix: 1/1 passed |
| Distinct toggle user event | Initial `PATTERN='toggleTaskAt changes'`: 0/1 passed; got `input` instead of `input.toggle` | Same command: 1/1 passed |
| Widget source range equality | Initial `PATTERN='widgets compare payloads'`: 0/1 passed; shifted task widget compared equal | Same command: 1/1 passed |
| Pointer interaction and properties entry | Browser RED and GREEN below, against the real CodeMirror view | Browser actions passed |

The first combined RED command was `pnpm --dir frontend exec node --test --test-name-pattern='task text keeps|local markdown image targets|empty frontmatter stays|toggleTaskAt changes|widgets compare payloads' src/lib/editor/livePreview.test.ts`: 0/5 passed for the expected missing behaviors. A persisted state regression inserts `intro\n` above a task, checks the shifted widget range, and toggles the shifted source marker. `pnpm --dir frontend exec node --test src/lib/editor/livePreview.test.ts` first reported 31/32 passing because an existing task test expected checked styling to stop on line one; its expectation was updated for the parser's lazy continuation, then the full suite passed.

The first `pnpm --dir frontend test && pnpm --dir frontend check` ran 138/138 tests but `check` found one TypeScript narrowing error in the new test's callback (`task!.to` inferred as `never`). The test now collects matching ranges in an array.

Final command: `pnpm --dir frontend test && pnpm --dir frontend check && git diff --check` exited 0: 138 tests passed, 0 failed; `svelte-check` found 0 errors and 0 warnings; diff whitespace check passed.

## Browser verification

A temporary `frontend/task13-harness.html` mounted `livePreview()` in a real `EditorView` with synthetic text `- [ ] task\n[[Known]]\nend` and a synthetic `Known.md` context. I ran `pnpm --dir frontend dev --host 0.0.0.0 --port 5173`, opened `/task13-harness.html` through T3 preview, used `preview_snapshot` to locate the checkbox and link, `preview_click` to exercise real pointer events, and `preview_evaluate` to read editor state and emitted events.

| Browser step | Before fix | After fix |
| --- | --- | --- |
| Click task checkbox with cursor at document end | Source remained `- [ ] task`; selection jumped from 24 to 0 and widget disappeared | Source became `- [x] task`; `aria-checked=true`; selection stayed 24 |
| Click `Known` wikilink | No `notebook:wikilink` event; selection jumped to 11 | One event: `{source:"[[Known]]",from:11,to:20,resolved:true}`; selection stayed 24 |
| Insert `intro\n` at offset 0, keep cursor at end, click checkbox | Not separately measured before the range fix | Only shifted marker changed: `intro\n- [ ] task...`; selection stayed 30; checkbox showed unchecked |
| Click shifted wikilink | Old click was lost in the pointer race | Event used current `{from:17,to:26,source:"[[Known]]"}` |
| Activate task and link with `.click()` (`detail=0`) | Not separately checked | Task toggled once, link emitted one more event, selection stayed 30 |
| Click a properties panel for `---\ntitle: Hi\n---\nbody` | Selection landed at 0 when the panel disappeared | Selection landed at 4, inside the YAML; panel opened as raw frontmatter |

The preview showed no browser console errors. The temporary harness was removed and the dev server stopped. The app still does not mount Live Preview, so this verifies widget interaction in the isolated editor rather than an end-to-end app screen. Touch input and pixel layout were not part of this interaction check; the requested desktop pointer behavior is verified.
