import { defaultKeymap, historyKeymap, indentWithTab, redo, undo } from "@codemirror/commands";
import {
  EditorSelection,
  type ChangeSpec,
  type EditorState,
  type Line,
  type SelectionRange,
  type StateCommand,
  type Transaction,
} from "@codemirror/state";
import type { Command, KeyBinding } from "@codemirror/view";

/*
 * The formatting commands of the editor and its keys. The commands edit the
 * Markdown as text, the way a person would type it: each one is a single
 * transaction (one undo step), works on every range of the selection or on
 * every line it touches, and never inserts or removes a line break, so a CRLF
 * document keeps its line breaks. On a read-only document they do nothing and
 * return false.
 */

export type EditorCommand =
  | "bold"
  | "italic"
  | "strike"
  | "code"
  | "heading"
  | "bullet"
  | "ordered"
  | "task"
  | "quote"
  | "link"
  | "undo"
  | "redo";
export type FormatCommand = Exclude<EditorCommand, "undo" | "redo">;

/**
 * The user event of a format command. It is an input, but not a typed one, so
 * that the history keeps it as an undo step of its own instead of joining it
 * to the typing around it.
 */
const FORMAT = "input.format";

/**
 * An inline mark, and how to read the runs of its character on either side of
 * a text: how many of them to take away, or 0 when the text is not wrapped in
 * the mark.
 */
type Wrapper = { mark: string; taken(before: number, after: number): number };

const wrappers: Record<"bold" | "italic" | "strike" | "code", Wrapper> = {
  // "*" is italic and "**" bold, so that "***" is both: a run of two or more
  // holds bold, an odd run holds italic.
  bold: { mark: "**", taken: (before, after) => (Math.min(before, after) >= 2 ? 2 : 0) },
  italic: { mark: "*", taken: (before, after) => (Math.min(before, after) % 2 === 1 ? 1 : 0) },
  strike: { mark: "~~", taken: (before, after) => (Math.min(before, after) >= 2 ? 2 : 0) },
  // A code span opens and closes with a run of backticks of any length.
  code: { mark: "`", taken: (before, after) => Math.min(before, after) },
};

/** How far outside a range to look for the run of a mark. */
const LOOK = 16;

const leading = (text: string, char: string): number => {
  let count = 0;
  while (count < text.length && text[count] === char) count++;
  return count;
};

const trailing = (text: string, char: string): number => {
  let count = 0;
  while (count < text.length && text[text.length - 1 - count] === char) count++;
  return count;
};

/** The range from..to, facing the way range does. */
const facing = (range: SelectionRange, from: number, to: number): SelectionRange =>
  range.anchor <= range.head ? EditorSelection.range(from, to) : EditorSelection.range(to, from);

/**
 * Toggles an inline mark around each range: off when the range starts and ends
 * with it or sits right between two of it, else on, the empty pair for a
 * cursor. Whatever was selected stays selected. Positions are read with
 * doc.sliceString, where a line break is one character, as in the document.
 */
function toggleWrapper({ mark, taken }: Wrapper): StateCommand {
  const char = mark[0];
  return ({ state, dispatch }) => {
    if (state.readOnly) return false;
    const { doc } = state;
    const spec = state.changeByRange((range) => {
      const { from, to } = range;
      const text = doc.sliceString(from, to);
      const half = Math.floor(text.length / 2);
      const inside = taken(Math.min(leading(text, char), half), Math.min(trailing(text, char), half));
      if (inside > 0) {
        return {
          changes: [
            { from, to: from + inside },
            { from: to - inside, to },
          ],
          range: facing(range, from, to - 2 * inside),
        };
      }
      const before = trailing(doc.sliceString(Math.max(0, from - LOOK), from), char);
      const after = leading(doc.sliceString(to, Math.min(doc.length, to + LOOK)), char);
      const outside = taken(before, after);
      if (outside > 0) {
        return {
          changes: [
            { from: from - outside, to: from },
            { from: to, to: to + outside },
          ],
          range: facing(range, from - outside, to - outside),
        };
      }
      return {
        changes: [
          { from, insert: mark },
          { from: to, insert: mark },
        ],
        range: facing(range, from + mark.length, to + mark.length),
      };
    });
    dispatch(state.update(spec, { scrollIntoView: true, userEvent: FORMAT }));
    return true;
  };
}

/** Where the text of a line starts for a heading or a list item: after its indentation and quote marks. */
const LEAD = /^(?:[ \t]*>)*[ \t]*/;
const HEADING = /^#{1,6}(?:[ \t]+|$)/;
const TASK = /^(?:[-*+]|\d{1,9}[.)])[ \t]+\[[ xX]\](?:[ \t]+|$)/;
const BULLET = /^[-*+](?:[ \t]+|$)/;
const ORDERED = /^\d{1,9}[.)](?:[ \t]+|$)/;
/** A quote mark, after the indentation, with the one space that may follow it. */
const QUOTE = /^[ \t]*>[ \t]?/;

/**
 * The lines that the selection touches, each once and in order. A range that
 * ends at the very start of a line, as one that selects whole lines does,
 * leaves that line out.
 */
function selectedLines(state: EditorState): Line[] {
  const lines: Line[] = [];
  let next = 1;
  for (const range of state.selection.ranges) {
    const first = state.doc.lineAt(range.from).number;
    let last = state.doc.lineAt(range.to).number;
    if (last > first && state.doc.line(last).from === range.to) last--;
    for (let number = Math.max(first, next); number <= last; number++) lines.push(state.doc.line(number));
    next = Math.max(next, last + 1);
  }
  return lines;
}

/** The lines to change: those with text, or all of them when none has any (a cursor on an empty line). */
function withText<T extends { blank: boolean }>(lines: T[]): T[] {
  return lines.some((line) => !line.blank) ? lines.filter((line) => !line.blank) : lines;
}

/**
 * Dispatches line changes as one format transaction. Cursors and the start of
 * a range move along with a prefix inserted before them.
 */
function editLines(state: EditorState, dispatch: (transaction: Transaction) => void, specs: ChangeSpec[]): boolean {
  const changes = state.changes(specs);
  dispatch(
    state.update({ changes, selection: state.selection.map(changes, 1), scrollIntoView: true, userEvent: FORMAT }),
  );
  return true;
}

/**
 * Cycles the heading of the selected lines: none, #, ##, ###, then none
 * again (as also from a deeper heading). The first line sets the level that
 * every line gets.
 */
const heading: StateCommand = ({ state, dispatch }) => {
  if (state.readOnly) return false;
  const lines = withText(
    selectedLines(state).map((line) => {
      const lead = LEAD.exec(line.text)?.[0].length ?? 0;
      const rest = line.text.slice(lead);
      const found = HEADING.exec(rest)?.[0] ?? "";
      const from = line.from + lead;
      return { from, to: from + found.length, level: found.trimEnd().length, blank: rest.trim() === "" };
    }),
  );
  const level = lines[0].level >= 3 ? 0 : lines[0].level + 1;
  const insert = level > 0 ? `${"#".repeat(level)} ` : "";
  const specs = lines
    .filter(({ from, to }) => state.doc.sliceString(from, to) !== insert)
    .map(({ from, to }) => ({ from, to, insert }));
  return editLines(state, dispatch, specs);
};

type ListKind = "bullet" | "ordered" | "task";

const LIST_MARKERS: [kind: ListKind, marker: RegExp][] = [
  ["task", TASK],
  ["bullet", BULLET],
  ["ordered", ORDERED],
];

/** The list marker a text starts with, with the space after it, or null. */
function listMarker(text: string): { kind: ListKind; length: number } | null {
  for (const [kind, marker] of LIST_MARKERS) {
    const found = marker.exec(text);
    if (found) return { kind, length: found[0].length };
  }
  return null;
}

const LIST_PREFIX: Record<ListKind, (index: number) => string> = {
  bullet: () => "- ",
  ordered: (index) => `${index + 1}. `,
  task: () => "- [ ] ",
};

/**
 * Toggles a list prefix on the selected lines that have text: off when every
 * one of them is that kind of item, else on for those that are not (and an
 * ordered list numbers them all from 1). Another kind of list marker is
 * replaced, not stacked; indentation and quote marks stay in front.
 */
function toggleList(kind: ListKind): StateCommand {
  return ({ state, dispatch }) => {
    if (state.readOnly) return false;
    const lines = withText(
      selectedLines(state).map((line) => {
        const lead = LEAD.exec(line.text)?.[0].length ?? 0;
        const rest = line.text.slice(lead);
        const marker = listMarker(rest);
        const from = line.from + lead;
        return { from, to: from + (marker?.length ?? 0), kind: marker?.kind ?? null, blank: rest.trim() === "" };
      }),
    );
    const off = lines.every((line) => line.kind === kind);
    const specs: ChangeSpec[] = [];
    lines.forEach((line, index) => {
      if (off) specs.push({ from: line.from, to: line.to });
      else if (line.kind !== kind || kind === "ordered") {
        const insert = LIST_PREFIX[kind](index);
        if (state.doc.sliceString(line.from, line.to) !== insert) specs.push({ from: line.from, to: line.to, insert });
      }
    });
    return editLines(state, dispatch, specs);
  };
}

/**
 * Toggles the quote mark of the selected lines: off (one level) when every line
 * with text has one, else on for those without it. A blank line between them
 * gets a bare ">", so that the quote goes on across it.
 */
const quote: StateCommand = ({ state, dispatch }) => {
  if (state.readOnly) return false;
  const lines = selectedLines(state).map((line) => ({
    line,
    mark: QUOTE.exec(line.text)?.[0],
    blank: line.text.trim() === "",
  }));
  const hasText = lines.some(({ blank }) => !blank);
  const off = hasText && lines.every(({ mark, blank }) => blank || mark !== undefined);
  const specs: ChangeSpec[] = off
    ? lines.flatMap(({ line, mark }) =>
        mark === undefined ? [] : [{ from: line.from + mark.indexOf(">"), to: line.from + mark.length }],
      )
    : lines
        .filter(({ mark }) => mark === undefined)
        .map(({ line, blank }) => ({ from: line.from, insert: blank && hasText ? ">" : "> " }));
  return editLines(state, dispatch, specs);
};

/** Makes each range the text of a link, [text](), with the cursor where the URL goes (in the brackets for a cursor). */
const link: StateCommand = ({ state, dispatch }) => {
  if (state.readOnly) return false;
  const spec = state.changeByRange((range) => ({
    changes: [
      { from: range.from, insert: "[" },
      { from: range.to, insert: "]()" },
    ],
    range: EditorSelection.cursor(range.empty ? range.from + 1 : range.to + 3),
  }));
  dispatch(state.update(spec, { scrollIntoView: true, userEvent: FORMAT }));
  return true;
};

export const formatCommands: Record<FormatCommand, Command> = {
  bold: toggleWrapper(wrappers.bold),
  italic: toggleWrapper(wrappers.italic),
  strike: toggleWrapper(wrappers.strike),
  code: toggleWrapper(wrappers.code),
  heading,
  bullet: toggleList("bullet"),
  ordered: toggleList("ordered"),
  task: toggleList("task"),
  quote,
  link,
};

/** Every command the host may run, by name: the format commands, and undo and redo of the history. */
export const editorCommands: Record<EditorCommand, Command> = { ...formatCommands, undo, redo };

/**
 * The editor's keys, in the order they are tried: the notebook's own first (so
 * Mod-i is italic, not CodeMirror's selectParentSyntax), then CodeMirror's
 * default and history keys and Tab to indent. Enter and Backspace come with
 * the Markdown language, at a higher precedence: they continue and end lists
 * and quotes. Not bound, as the host owns them: Mod-e, Mod-o, Mod-p and
 * Mod-Shift-f; there is no search keymap, so Mod-f is the browser's.
 *
 * Strike is Mod-Shift-x (as in Google Docs and Slack) and inline code is
 * Mod-Shift-m (monospace, as in Confluence): Mod-e, the usual key for code, is
 * the host's mode toggle, and Mod-` switches windows on a Mac. Mod-Shift-z
 * redoes everywhere, and so does Mod-y except on a Mac, where it stays the
 * browser's.
 */
export function editorKeymap(onsave: () => void): KeyBinding[] {
  return [
    {
      key: "Mod-s",
      run: () => {
        onsave();
        return true;
      },
      preventDefault: true,
      stopPropagation: true,
    },
    { key: "Mod-b", run: formatCommands.bold },
    { key: "Mod-i", run: formatCommands.italic },
    { key: "Mod-Shift-x", run: formatCommands.strike },
    { key: "Mod-Shift-m", run: formatCommands.code },
    { key: "Mod-Shift-z", run: redo, preventDefault: true },
    ...defaultKeymap,
    ...historyKeymap,
    indentWithTab,
  ];
}
