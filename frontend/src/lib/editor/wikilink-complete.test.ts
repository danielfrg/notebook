import assert from "node:assert/strict";
import { test } from "node:test";
import { EditorState } from "@codemirror/state";
import { CompletionContext } from "@codemirror/autocomplete";
import { wikilinkCompletions, wikilinkSource } from "./wikilink-complete.ts";

test("completion filters by title and inserts the basename", () => {
  assert.deepEqual(wikilinkCompletions(["folder/Fuzzy Notes.md", "Other.md"], "fuz"), [
    { label: "Fuzzy Notes", detail: "folder", apply: "Fuzzy Notes" },
  ]);
});

test("duplicate basenames carry distinguishing folder details and honor limit", () => {
  const hits = wikilinkCompletions(["a/Ideas.md", "b/Ideas.md"], "idea", 1);
  assert.deepEqual(hits, [{ label: "Ideas", detail: "a", apply: "Ideas" }]);
});

test("a root note with a duplicate basename is identified as the vault root", () => {
  assert.deepEqual(wikilinkCompletions(["Ideas.md", "a/Ideas.md"], "idea")[0], {
    label: "Ideas",
    detail: "Vault root",
    apply: "Ideas",
  });
});

test("source starts only after opening brackets and inserts missing closing brackets", () => {
  const source = wikilinkSource(() => ["folder/Known.md"]);
  const state = EditorState.create({ doc: "Before [[Kno" });
  const result = source(new CompletionContext(state, state.doc.length, false));
  assert.ok(result && !(result instanceof Promise));
  assert.equal(result.from, 9);
  const option = result.options[0];
  assert.equal(typeof option?.apply, "function");
  if (typeof option.apply !== "function") throw new Error("Expected a completion callback");
  const calls: { from: number; to: number; insert: string }[] = [];
  option.apply(
    {
      state,
      dispatch: (change: { changes: (typeof calls)[number] }) => calls.push(change.changes),
    } as never,
    option,
    9,
    12,
  );
  assert.deepEqual(calls, [{ from: 9, to: 12, insert: "Known]]" }]);
  assert.equal(source(new CompletionContext(EditorState.create({ doc: "Known" }), 5, false)), null);
});

test("completion keeps closing brackets that already follow the cursor", () => {
  const source = wikilinkSource(() => ["Known.md"]);
  const state = EditorState.create({ doc: "[[Kno]]" });
  const result = source(new CompletionContext(state, 5, false));
  assert.ok(result && !(result instanceof Promise));
  const option = result.options[0];
  if (typeof option.apply !== "function") throw new Error("Expected a completion callback");
  let insert = "";
  option.apply(
    {
      state,
      dispatch: (spec: { changes: { insert: string } }) => {
        insert = spec.changes.insert;
      },
    } as never,
    option,
    2,
    5,
  );
  assert.equal(insert, "Known");
});
