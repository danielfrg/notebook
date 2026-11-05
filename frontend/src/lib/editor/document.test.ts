import test from "node:test";
import assert from "node:assert/strict";
import { insertNewlineAndIndent, undo } from "@codemirror/commands";
import {
  ChangeSet,
  Compartment,
  EditorSelection,
  EditorState,
  Prec,
  StateEffect,
  Text,
  type Extension,
  type Transaction,
} from "@codemirror/state";
import { EditorView } from "@codemirror/view";
import {
  changedByUser,
  documentExtensions,
  readOnlyExtension,
  replaceDocument,
  replaceDocumentSpec,
  separatorExtension,
  toDocument,
  type DocumentCompartments,
} from "./document.ts";
import { detectLineSeparator } from "./linesep.ts";

/** An editor state as the editor makes one for a text, with its compartments. */
function load(
  text: string,
  extensions: Extension = [],
): { state: EditorState; compartments: DocumentCompartments } {
  const compartments = { separator: new Compartment(), history: new Compartment() };
  const separator = detectLineSeparator(text);
  const state = EditorState.create({
    doc: toDocument(text, separator),
    extensions: [documentExtensions(separator, compartments), extensions],
  });
  return { state, compartments };
}

/** The state after a document is replaced with text, as the editor does when its value changes. */
function reload(state: EditorState, text: string, compartments: DocumentCompartments): EditorState {
  const transactions = replaceDocument(state, text, detectLineSeparator(text), compartments);
  assert.ok(transactions.length > 0);
  assert.equal(changedByUser(transactions), false, "a replacement is nobody's edit");
  return transactions[transactions.length - 1].state;
}

/** Runs a state command on a state and returns the state it dispatched, or null. */
function command(
  run: (target: { state: EditorState; dispatch: (tr: Transaction) => void }) => boolean,
  state: EditorState,
) {
  let next: EditorState | null = null;
  run({ state, dispatch: (tr) => (next = tr.state) });
  return next as EditorState | null;
}

test("replaceDocumentSpec is the change between the common start and end of two documents", () => {
  const cases: [current: string, next: string, change: string | null][] = [
    ["abc", "abc", null],
    ["abc", "abXc", "2-2 X"],
    ["a\nb\nc", "a\nB\nc", "2-3 B"],
    ["a\nb", "a\nb\nc", "3-3 \nc"],
    ["x\ny", "y", "0-2 "],
    ["a\nb\nc", "a\nc", "2-4 "],
    ["", "abc", "0-0 abc"],
    ["abc", "", "0-3 "],
    ["aa", "aaa", "2-2 a"],
    ["one\n\nthree", "one\ntwo\nthree", "4-4 two"],
  ];
  for (const [current, next, change] of cases) {
    const spec = replaceDocumentSpec(Text.of(current.split("\n")), Text.of(next.split("\n")));
    assert.equal(
      spec && `${spec.from}-${spec.to} ${spec.insert.toString()}`,
      change,
      JSON.stringify([current, next]),
    );
  }
});

test("replaceDocumentSpec turns any document into any other, changing no more than it must", () => {
  // Every pair of texts of up to four characters from "a", "b" and a line break.
  const texts = [""];
  for (let length = 1, last = [""]; length <= 4; length++) {
    last = last.flatMap((text) => ["a", "b", "\n"].map((char) => text + char));
    texts.push(...last);
  }
  for (const a of texts) {
    for (const b of texts) {
      const current = Text.of(a.split("\n"));
      const next = Text.of(b.split("\n"));
      const spec = replaceDocumentSpec(current, next);
      const label = JSON.stringify([a, b]);
      if (spec === null) {
        assert.equal(a, b, label);
        continue;
      }
      assert.ok(ChangeSet.of(spec, current.length).apply(current).eq(next), label);
      // Neither end of the change is the same in both texts, or it would be shorter.
      const removed = a.slice(spec.from, spec.to);
      const inserted = spec.insert.toString();
      assert.ok(removed.length === 0 || inserted.length === 0 || removed[0] !== inserted[0], label);
      assert.ok(
        removed.length === 0 || inserted.length === 0 || removed.at(-1) !== inserted.at(-1),
        label,
      );
    }
  }
});

test("toDocument splits a text into the lines an editor state with its separator has", () => {
  for (const text of [
    "a\nb",
    "a\r\nb\r\n",
    "a\r\nb\nc",
    "a\rb",
    "",
    "\uFEFF---\r\na: 1\r\n---\r\n",
  ]) {
    const separator = detectLineSeparator(text);
    const state = EditorState.create({ doc: text, extensions: separatorExtension(separator) });
    assert.deepEqual(
      toDocument(text, separator).toJSON(),
      state.doc.toJSON(),
      JSON.stringify(text),
    );
  }
  assert.deepEqual(toDocument("a\r\nb", "\r\n").toJSON(), ["a", "b"]);
  assert.deepEqual(toDocument("a\nb\r\nc\rd", "mixed").toJSON(), ["a", "b", "c", "d"]);
});

test("a CRLF document stays CRLF through typing, Enter and pasting", () => {
  let { state } = load("one\r\ntwo\r\n");
  assert.equal(state.lineBreak, "\r\n");
  state = state.update({ changes: { from: 3, insert: " more" }, selection: { anchor: 8 } }).state;
  state = command(insertNewlineAndIndent, state) ?? assert.fail("Enter did nothing");
  state = state.update(state.replaceSelection("new")).state;
  assert.equal(state.sliceDoc(0), "one more\r\nnew\r\ntwo\r\n");

  // A paste comes with whatever line breaks the clipboard had; they become the document's.
  const pasted = state
    .facet(EditorView.clipboardInputFilter)
    .reduce((text, filter) => filter(text, state), "a\nb\r\nc\rd");
  assert.equal(pasted, "a\r\nb\r\nc\r\nd");
  state = state.update(state.replaceSelection(state.toText(pasted))).state;
  assert.equal(state.sliceDoc(0), "one more\r\nnewa\r\nb\r\nc\r\nd\r\ntwo\r\n");

  // An LF document has no use for the filter: CodeMirror splits every line break already.
  assert.deepEqual(load("a\nb").state.facet(EditorView.clipboardInputFilter), []);
});

test("a document with mixed line breaks is read-only, whatever else is configured", () => {
  const editable = (state: EditorState) => [state.readOnly, state.facet(EditorView.editable)];
  assert.deepEqual(editable(load("a\nb").state), [false, true]);
  assert.deepEqual(editable(load("a\r\nb").state), [false, true]);
  assert.deepEqual(editable(load("a\r\nb\nc").state), [true, false]);
  assert.deepEqual(editable(load("a\rb").state), [true, false]);
  // Not even a setting that says otherwise, at a higher precedence, makes it editable.
  const editableAnyway = Prec.high([EditorView.editable.of(true), EditorState.readOnly.of(false)]);
  assert.deepEqual(editable(load("a\nb", editableAnyway).state), [false, true]);
  assert.deepEqual(editable(load("a\rb", editableAnyway).state), [true, false]);
  // The host's read-only setting adds to it.
  assert.deepEqual(editable(load("a\nb", [readOnlyExtension(true)]).state), [true, false]);
  assert.deepEqual(editable(load("a\rb", [readOnlyExtension(false)]).state), [true, false]);
  assert.deepEqual(editable(load("a\nb", [readOnlyExtension(false)]).state), [false, true]);
});

test("a document with mixed line breaks takes no change but a replacement", () => {
  let { state, compartments } = load("a\r\nb\nc");
  // A change dispatched without a look at readOnly, as an extension might dispatch one, is dropped.
  const edit = state.update({ changes: { from: 0, insert: "x" }, selection: { anchor: 1 } });
  assert.equal(edit.docChanged, false);
  assert.deepEqual(edit.state.doc.toJSON(), ["a", "b", "c"]);
  assert.equal(changedByUser([edit]), false);
  // A selection or an effect alone goes through.
  assert.equal(state.update({ selection: { anchor: 2 } }).state.selection.main.head, 2);
  const marker = StateEffect.define<number>();
  assert.ok(state.update({ effects: marker.of(1) }).effects.some((effect) => effect.is(marker)));
  // A replacement still replaces it: with another mixed text, with an LF text, and back to mixed.
  state = reload(state, "a\r\nb\nc\nd", compartments);
  assert.deepEqual([state.doc.toJSON(), state.readOnly], [["a", "b", "c", "d"], true]);
  state = reload(state, "p\nq", compartments);
  assert.deepEqual([state.sliceDoc(0), state.readOnly], ["p\nq", false]);
  assert.equal(
    state.update({ changes: { from: 0, insert: "x" } }).state.sliceDoc(0),
    "xp\nq",
    "an LF document takes changes",
  );
  state = reload(state, "m\r\nn\no", compartments);
  assert.deepEqual([state.doc.toJSON(), state.readOnly], [["m", "n", "o"], true]);
});

test("replacing the document changes only what differs, and keeps the selection where its text is", () => {
  const { state: start, compartments } = load("one\ntwo\nthree");
  const at = (anchor: number) => start.update({ selection: EditorSelection.cursor(anchor) }).state;
  const cases: [cursor: number, text: string, after: number][] = [
    [1, "one\nTWO\nthree", 1], // before the change
    [13, "zero\none\ntwo\nthree", 18], // after it
    [5, "one\nTWO\nthree", 4], // inside it: to its start
    [13, "one", 3], // past the end of a shorter text: to its end
  ];
  for (const [cursor, text, after] of cases) {
    const transactions = replaceDocument(at(cursor), text, "\n", compartments);
    const state = transactions[transactions.length - 1].state;
    assert.equal(state.sliceDoc(0), text, text);
    assert.equal(state.selection.main.head, after, text);
    assert.equal(transactions.filter((tr) => tr.docChanged).length, 1, text);
    assert.equal(
      transactions.some((tr) => tr.scrollIntoView),
      false,
      text,
    );
  }
  // The change is the smallest one: only the line that differs is replaced.
  const [first] = replaceDocument(start, "one\nTWO\nthree", "\n", compartments);
  const changed: string[] = [];
  first.changes.iterChanges((from, to, _fromB, _toB, inserted) =>
    changed.push(`${from}-${to} ${inserted}`),
  );
  assert.deepEqual(changed, ["4-7 TWO"]);
});

test("a replaced document starts a history of its own", () => {
  let { state, compartments } = load("a");
  state = state.update({ changes: { from: 1, insert: "b" }, userEvent: "input" }).state;
  assert.ok(command(undo, state), "the edit can be undone");
  state = reload(state, "ab\nc", compartments);
  assert.equal(command(undo, state), null, "nothing from before the replacement is undone");
  state = state.update({ changes: { from: 0, insert: "z" }, userEvent: "input" }).state;
  assert.equal(command(undo, state)?.sliceDoc(0), "ab\nc");
});

test("replacing the document takes on the line separator of the new text", () => {
  let { state, compartments } = load("a\nb");
  state = reload(state, "a\r\nb\r\nc", compartments);
  assert.equal(state.lineBreak, "\r\n");
  assert.equal(state.sliceDoc(0), "a\r\nb\r\nc");
  state = state.update({ changes: { from: state.doc.length, insert: Text.of(["", "d"]) } }).state;
  assert.equal(state.sliceDoc(0), "a\r\nb\r\nc\r\nd");

  // A text that mixes them is read-only until a text that does not replaces it.
  state = reload(state, "a\nb\r\nc", compartments);
  assert.equal(state.readOnly, true);
  assert.deepEqual(state.doc.toJSON(), ["a", "b", "c"]);
  state = reload(state, "a\nb", compartments);
  assert.equal(state.readOnly, false);
  assert.equal(state.lineBreak, "\n");
  assert.equal(state.sliceDoc(0), "a\nb");

  // The same lines with other line breaks: no change to the document, only to its separator.
  const transactions = replaceDocument(state, "a\r\nb", "\r\n", compartments);
  assert.equal(
    transactions.some((tr) => tr.docChanged),
    false,
  );
  assert.equal(transactions[transactions.length - 1].state.sliceDoc(0), "a\r\nb");
});

test("changedByUser tells an edit from a replacement and from a move", () => {
  const { state, compartments } = load("a");
  assert.equal(changedByUser([state.update({ changes: { from: 0, insert: "x" } })]), true);
  assert.equal(changedByUser([state.update({ selection: { anchor: 1 } })]), false);
  assert.equal(changedByUser(replaceDocument(state, "b", "\n", compartments)), false);
  assert.equal(changedByUser([]), false);
});
