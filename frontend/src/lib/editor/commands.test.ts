import test from "node:test";
import assert from "node:assert/strict";
import { history, indentMore, redo, selectAll, selectParentSyntax, undo } from "@codemirror/commands";
import { insertNewlineContinueMarkup } from "@codemirror/lang-markdown";
import {
  EditorSelection,
  EditorState,
  StateEffect,
  type Extension,
  type SelectionRange,
  type Transaction,
} from "@codemirror/state";
import { keymap, type Command, type EditorView, type KeyBinding } from "@codemirror/view";
import { editorCommands, editorKeymap, formatCommands, type FormatCommand } from "./commands.ts";
import { notebookMarkdown } from "./markdown.ts";

/*
 * A document and its selection are written as one text with marks: "|" is a
 * cursor, "{" and "}" are the start and end of a selected range. The marks are
 * no characters of the document. Line breaks are written "\n", and every case
 * runs twice: on that text, and on the same text with CRLF line breaks in an
 * editor state that keeps them.
 */

/** The document without its marks, and the selection that the marks stand for. */
function parse(marked: string): { doc: string; selection: EditorSelection } {
  const ranges: SelectionRange[] = [];
  let doc = "";
  let start = -1;
  for (const char of marked) {
    if (char === "|") ranges.push(EditorSelection.cursor(doc.length));
    else if (char === "{") start = doc.length;
    else if (char === "}") ranges.push(EditorSelection.range(start, doc.length));
    else doc += char;
  }
  return { doc, selection: EditorSelection.create(ranges.length > 0 ? ranges : [EditorSelection.cursor(0)]) };
}

/** The document of state with its selection marked, its line breaks written "\n". */
function mark(state: EditorState): string {
  const doc = state.doc.toString();
  let marked = "";
  let at = 0;
  const put = (pos: number, text: string) => {
    marked += doc.slice(at, pos) + text;
    at = pos;
  };
  for (const range of state.selection.ranges) {
    if (range.empty) put(range.head, "|");
    else {
      put(range.from, "{");
      put(range.to, "}");
    }
  }
  return marked + doc.slice(at);
}

/** An editor state for a marked text, with history and multiple selections, its line breaks LF or CRLF. */
function stateOf(marked: string, crlf: boolean, extensions: Extension[] = []): EditorState {
  const { doc, selection } = parse(marked);
  return EditorState.create({
    doc: crlf ? doc.replaceAll("\n", "\r\n") : doc,
    selection,
    extensions: [
      EditorState.allowMultipleSelections.of(true),
      history(),
      crlf ? EditorState.lineSeparator.of("\r\n") : [],
      extensions,
    ],
  });
}

/**
 * Runs a command on a stand-in for the view with only what a command may use,
 * its state and dispatch, and says what it did.
 */
function run(command: Command, state: EditorState): { handled: boolean; state: EditorState; transactions: Transaction[] } {
  const transactions: Transaction[] = [];
  const target = {
    state,
    dispatch(transaction: Transaction) {
      transactions.push(transaction);
      target.state = transaction.state;
    },
  };
  const handled = command(target as unknown as EditorView);
  return { handled, state: target.state, transactions };
}

const cases: [command: FormatCommand, before: string, after: string][] = [
  // Inline marks wrap each range, keeping its text selected; an empty one gets the empty pair.
  ["bold", "a {b} c", "a **{b}** c"],
  ["bold", "a | c", "a **|** c"],
  ["bold", "{a} and {b}", "**{a}** and **{b}**"],
  ["bold", "{a\nb}", "**{a\nb}**"],
  // They unwrap when the marks are just outside the range or are its ends.
  ["bold", "a **{b}** c", "a {b} c"],
  ["bold", "a {**b**} c", "a {b} c"],
  ["bold", "a **|** c", "a | c"],
  ["bold", "**{a}** and **{b}**", "{a} and {b}"],
  // Bold and italic share the "*": a run of three is both.
  ["bold", "*{b}*", "***{b}***"],
  ["bold", "***{b}***", "*{b}*"],
  ["italic", "{b}", "*{b}*"],
  ["italic", "*{b}*", "{b}"],
  ["italic", "{*b*}", "{b}"],
  ["italic", "|", "*|*"],
  ["italic", "*|*", "|"],
  ["italic", "**{b}**", "***{b}***"],
  ["italic", "{**b**}", "*{**b**}*"],
  ["italic", "***{b}***", "**{b}**"],
  ["italic", "**|**", "***|***"],
  ["strike", "{b}", "~~{b}~~"],
  ["strike", "~~{b}~~", "{b}"],
  ["strike", "{~~b~~}", "{b}"],
  ["strike", "|", "~~|~~"],
  ["code", "a {b} c", "a `{b}` c"],
  ["code", "a `{b}` c", "a {b} c"],
  ["code", "a {`b`} c", "a {b} c"],
  ["code", "|", "`|`"],
  ["code", "``|``", "|"],

  // A heading cycles none, #, ##, ###, none, on the text of the line only.
  ["heading", "a|", "# a|"],
  ["heading", "# a|", "## a|"],
  ["heading", "## a|", "### a|"],
  ["heading", "### a|", "a|"],
  ["heading", "###### a|", "a|"],
  ["heading", "|a", "# |a"],
  ["heading", "#|", "## |"],
  ["heading", "#tag|", "# #tag|"], // a tag is no heading
  ["heading", "x\n|", "x\n# |"],
  ["heading", "> a|", "> # a|"], // inside a quote
  ["heading", "{a\n# b}", "# {a\n# b}"], // the first line sets the level of all
  ["heading", "{a\n\nb}", "# {a\n\n# b}"],

  // List and quote prefixes toggle on every line the selection touches.
  ["bullet", "a|", "- a|"],
  ["bullet", "- a|", "a|"],
  ["bullet", "* a|", "a|"],
  ["bullet", "-|", "|"],
  ["bullet", "|", "- |"],
  ["bullet", "{a\nb}", "- {a\n- b}"],
  ["bullet", "{- a\n- b}", "{a\nb}"],
  ["bullet", "{- a\nb}", "{- a\n- b}"], // a mixed selection adds the prefix where it is missing
  ["bullet", "{a\n\nb}", "- {a\n\n- b}"], // blank lines stay blank
  ["bullet", "{a\n}b", "- {a\n}b"], // a range that ends at the start of a line leaves that line
  ["bullet", "|a\n|b", "- |a\n- |b"],
  ["bullet", "1. a|", "- a|"], // another marker is replaced, not stacked
  ["bullet", "- [ ] a|", "- a|"],
  ["bullet", "  - a|", "  a|"], // indentation stays
  ["bullet", "> a|", "> - a|"],
  ["ordered", "{a\nb\nc}", "1. {a\n2. b\n3. c}"],
  ["ordered", "{1. a\n2. b}", "{a\nb}"],
  ["ordered", "1) a|", "a|"],
  ["ordered", "- a|", "1. a|"],
  ["ordered", "{a\n- b}", "1. {a\n2. b}"],
  ["ordered", "{3. a\nb}", "{1. a\n2. b}"],
  ["ordered", "{a\n\nb}", "1. {a\n\n2. b}"],
  ["task", "a|", "- [ ] a|"],
  ["task", "- [ ] a|", "a|"],
  ["task", "- [x] a|", "a|"],
  ["task", "- a|", "- [ ] a|"],
  ["task", "1. a|", "- [ ] a|"],
  ["task", "{a\nb}", "- [ ] {a\n- [ ] b}"],
  ["task", "{- [x] a\nb}", "{- [x] a\n- [ ] b}"],
  ["quote", "a|", "> a|"],
  ["quote", "> a|", "a|"],
  ["quote", ">a|", "a|"],
  ["quote", "> > a|", "> a|"],
  ["quote", "|", "> |"],
  ["quote", "{a\nb}", "> {a\n> b}"],
  ["quote", "{a\n\nb}", "> {a\n>\n> b}"], // one quote across the blank line
  ["quote", "{> a\n>\n> b}", "{a\n\nb}"],
  ["quote", "{> a\nb}", "{> a\n> b}"],
  ["quote", "- a|", "> - a|"], // a quote holds a list

  // A link puts the selection in brackets and the cursor in the parentheses.
  ["link", "a {b} c", "a [b](|) c"],
  ["link", "a | c", "a [|]() c"],
  ["link", "{a} {b}", "[a](|) [b](|)"],
  ["link", "{a\nb}", "[a\nb](|)"],
];

test("format commands edit the Markdown of every selected range or line, in one undoable transaction", () => {
  for (const crlf of [false, true]) {
    for (const [name, before, after] of cases) {
      const label = `${name} ${JSON.stringify(before)}${crlf ? " in CRLF" : ""}`;
      const { handled, state, transactions } = run(formatCommands[name], stateOf(before, crlf));
      assert.equal(handled, true, label);
      assert.equal(transactions.length, 1, label);
      assert.equal(mark(state), after, label);
      // The document keeps its own line breaks: a CRLF text gets no bare LF.
      const text = parse(after).doc;
      assert.equal(state.sliceDoc(0), crlf ? text.replaceAll("\n", "\r\n") : text, label);
      // One undo brings back the text and the selection.
      const undone = run(undo, state);
      assert.equal(undone.handled, true, label);
      assert.equal(mark(undone.state), before, label);
    }
  }
});

test("a format command is an undo step of its own, apart from the typing before it", () => {
  let state = stateOf("a|", false);
  state = state.update({ changes: { from: 1, insert: "b" }, selection: { anchor: 2 }, userEvent: "input.type" }).state;
  state = run(formatCommands.bold, state).state;
  state = run(formatCommands.italic, state).state;
  assert.equal(mark(state), "ab***|***");
  state = run(undo, state).state;
  assert.equal(mark(state), "ab**|**");
  state = run(undo, state).state;
  assert.equal(mark(state), "ab|");
});

test("no command changes a read-only document", () => {
  for (const [name, command] of Object.entries(editorCommands)) {
    let state = stateOf("a {b} c", false);
    // An edit, undone, so that undo and redo would both have work to do.
    state = state.update({ changes: { from: 0, insert: "x" }, userEvent: "input" }).state;
    state = state.update({ changes: { from: 0, insert: "y" }, userEvent: "input" }).state;
    state = run(undo, state).state;
    state = state.update({ effects: StateEffect.appendConfig.of(EditorState.readOnly.of(true)) }).state;
    const { handled, transactions } = run(command, state);
    assert.equal(handled, false, name);
    assert.equal(transactions.length, 0, name);
  }
});

test("every editor command has a command, undo and redo those of the history", () => {
  assert.deepEqual(Object.keys(editorCommands).sort(), [
    "bold",
    "bullet",
    "code",
    "heading",
    "italic",
    "link",
    "ordered",
    "quote",
    "redo",
    "strike",
    "task",
    "undo",
  ]);
  for (const name of Object.keys(formatCommands) as FormatCommand[]) assert.equal(editorCommands[name], formatCommands[name]);
  assert.equal(editorCommands.undo, undo);
  assert.equal(editorCommands.redo, redo);
});

type Platform = "mac" | "win" | "linux";

/** A key name as CodeMirror normalizes it for a platform: modifiers in one order, Mod as Meta on a Mac and Ctrl elsewhere. */
function normalize(name: string, platform: Platform): string {
  const parts = name.split(/-(?!$)/);
  const key = parts.pop() ?? "";
  const has = (modifier: RegExp) => parts.some((part) => modifier.test(part));
  const mod = has(/^mod$/i);
  return [
    has(/^s(hift)?$/i) && "Shift",
    (has(/^(cmd|meta|m)$/i) || (mod && platform === "mac")) && "Meta",
    (has(/^(c|ctrl|control)$/i) || (mod && platform !== "mac")) && "Ctrl",
    has(/^a(lt)?$/i) && "Alt",
    key,
  ]
    .filter(Boolean)
    .join("-");
}

/** The bindings of a key in a state on a platform, in the order CodeMirror tries them, as [command, binding]. */
function bindings(state: EditorState, key: string, platform: Platform): [Command, KeyBinding][] {
  const want = normalize(key, platform);
  const found: [Command, KeyBinding][] = [];
  for (const binding of state.facet(keymap).flat()) {
    const name = binding[platform] || binding.key;
    if (!name) continue;
    if (binding.run && normalize(name, platform) === want) found.push([binding.run, binding]);
    if (binding.shift && normalize(`Shift-${name}`, platform) === want) found.push([binding.shift, binding]);
  }
  return found;
}

test("the keymap runs the notebook's commands first and leaves the host's shortcuts and search alone", () => {
  let saves = 0;
  // The editor's keys: the Markdown keymap that comes with the language, then the editor's own.
  const state = EditorState.create({ extensions: [notebookMarkdown(), keymap.of(editorKeymap(() => saves++))] });
  for (const platform of ["mac", "win", "linux"] as const) {
    const first = (key: string) => bindings(state, key, platform)[0]?.[0];
    assert.equal(first("Mod-b"), formatCommands.bold, platform);
    // Italic first, and CodeMirror's default keymap after it: its Mod-i only runs when italic does not.
    assert.deepEqual(
      bindings(state, "Mod-i", platform).map(([command]) => command),
      [formatCommands.italic, selectParentSyntax],
      platform,
    );
    assert.equal(first("Mod-a"), selectAll, platform);
    assert.equal(first("Mod-Shift-x"), formatCommands.strike, platform);
    assert.equal(first("Mod-Shift-m"), formatCommands.code, platform);
    assert.equal(first("Enter"), insertNewlineContinueMarkup, platform);
    assert.equal(first("Tab"), indentMore, platform);
    assert.equal(first("Mod-z"), undo, platform);
    assert.equal(first("Mod-Shift-z"), redo, platform);
    for (const key of ["Mod-e", "Mod-o", "Mod-p", "Mod-Shift-f", "Mod-f"]) {
      assert.deepEqual(bindings(state, key, platform), [], `${platform} ${key} is not the editor's`);
    }
    // Mod-s saves, and the event goes no further: not to the browser, nor to a shortcut of the page.
    const [save, binding] = bindings(state, "Mod-s", platform)[0];
    const before = saves;
    assert.equal(save(null as unknown as EditorView), true, platform);
    assert.equal(saves, before + 1, platform);
    assert.equal(binding.preventDefault, true, platform);
    assert.equal(binding.stopPropagation, true, platform);
  }
  // Mod-y redoes on Windows and Linux; on a Mac it stays the browser's.
  assert.equal(bindings(state, "Mod-y", "win")[0]?.[0], redo);
  assert.equal(bindings(state, "Mod-y", "linux")[0]?.[0], redo);
});

test("Enter continues a list or a quote with the document's own line break", () => {
  const cases: [before: string, after: string][] = [
    ["- a|", "- a\n- |"],
    ["1. a|", "1. a\n2. |"],
    ["- [x] a|", "- [x] a\n- [ ] |"],
    ["> a|", "> a\n> |"],
    ["---\nt: 1\n---\n- a|", "---\nt: 1\n---\n- a\n- |"], // after frontmatter
  ];
  for (const crlf of [false, true]) {
    for (const [before, after] of cases) {
      const label = `${JSON.stringify(before)}${crlf ? " in CRLF" : ""}`;
      const { handled, state } = run(insertNewlineContinueMarkup, stateOf(before, crlf, [notebookMarkdown()]));
      assert.equal(handled, true, label);
      assert.equal(mark(state), after, label);
      const text = parse(after).doc;
      assert.equal(state.sliceDoc(0), crlf ? text.replaceAll("\n", "\r\n") : text, label);
    }
  }
});
