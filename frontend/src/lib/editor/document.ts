import { history } from "@codemirror/commands";
import {
  Annotation,
  EditorState,
  Prec,
  Text,
  type Compartment,
  type Extension,
  type Transaction,
} from "@codemirror/state";
import { EditorView } from "@codemirror/view";
import type { detectLineSeparator } from "./linesep.ts";

/*
 * The document of the editor as a whole: the text of a note, held with the
 * line separator the note uses, and replaced when another version of the note
 * comes in. State code only, without a DOM; Editor.svelte puts it in a view.
 */

/** The line separator of a text, as detectLineSeparator names it. */
export type LineSeparator = ReturnType<typeof detectLineSeparator>;

/** The compartments of what comes with a loaded document: its line separator and its history. */
export type DocumentCompartments = { separator: Compartment; history: Compartment };

/** Marks the transactions that replace the document, which are no edit of the user's. */
const replacement = Annotation.define<boolean>();

const crlf: Extension = [
  EditorState.lineSeparator.of("\r\n"),
  // A paste or a drop has the line breaks of wherever it came from: it gets
  // the document's, as Enter does. Else a line would hold a bare LF, and the
  // note would be saved with mixed line breaks.
  EditorView.clipboardInputFilter.of((text) => text.replace(/\r\n?|\n/g, "\r\n")),
];

/**
 * A document whose line breaks a state cannot keep is not edited, whatever
 * else is configured. Read-only stops the commands and the keyboard; the change
 * filter also drops any change that some extension dispatches without looking
 * at it (keeping its selection and effects). Only a replacement gets through.
 */
const mixed: Extension = Prec.highest([
  EditorState.readOnly.of(true),
  EditorView.editable.of(false),
  EditorState.changeFilter.of(
    (transaction) => !transaction.docChanged || transaction.annotation(replacement) === true,
  ),
]);

/**
 * What a document with that line separator needs. CRLF becomes the separator,
 * so that sliceDoc(0) gives the text back as it was; LF is CodeMirror's own;
 * mixed line breaks (a bare CR, or LF and CRLF both) make it read-only, since
 * any edit would save them all as one.
 */
export function separatorExtension(separator: LineSeparator): Extension {
  return separator === "\r\n" ? crlf : separator === "mixed" ? mixed : [];
}

const readOnly: Extension = [EditorState.readOnly.of(true), EditorView.editable.of(false)];
const writable: Extension = [];

/** The host's read-only setting: no command changes the text, and the text cannot be focused to type in. */
export function readOnlyExtension(on: boolean): Extension {
  return on ? readOnly : writable;
}

/** The lines of a text, split as an editor state with that line separator splits it. */
export function toDocument(text: string, separator: LineSeparator): Text {
  return Text.of(text.split(separator === "\r\n" ? "\r\n" : /\r\n?|\n/));
}

/** The extensions that come with a document, each in its compartment: its separator's, and a history. */
export function documentExtensions(
  separator: LineSeparator,
  compartments: DocumentCompartments,
): Extension {
  return [
    compartments.separator.of(separatorExtension(separator)),
    compartments.history.of(history()),
  ];
}

const sharedStart = (a: string, b: string): number => {
  const most = Math.min(a.length, b.length);
  let count = 0;
  while (count < most && a[count] === b[count]) count++;
  return count;
};

const sharedEnd = (a: string, b: string): number => {
  const most = Math.min(a.length, b.length);
  let count = 0;
  while (count < most && a[a.length - 1 - count] === b[b.length - 1 - count]) count++;
  return count;
};

/**
 * The change that turns the document current into next, or null when they are
 * the same: one replacement from where they first differ to where they last
 * do. It is compared line by line, the line breaks included, so whatever both
 * have at their start and end is left alone, and positions there do not move.
 */
export function replaceDocumentSpec(
  current: Text,
  next: Text,
): { from: number; to: number; insert: Text } | null {
  if (current.eq(next)) return null;
  const a = current.toJSON();
  const b = next.toJSON();
  // The shared start: equal lines with the line break after them, then the
  // shared start of the first lines that differ.
  let line = 0;
  let from = 0;
  while (line < a.length - 1 && line < b.length - 1 && a[line] === b[line]) {
    from += a[line].length + 1;
    line++;
  }
  from += sharedStart(a[line], b[line]);
  // The shared end, the same way from the end, but not into the shared start.
  let i = a.length - 1;
  let j = b.length - 1;
  let end = 0;
  while (i > 0 && j > 0 && a[i] === b[j]) {
    end += a[i].length + 1;
    i--;
    j--;
  }
  end = Math.min(end + sharedEnd(a[i], b[j]), current.length - from, next.length - from);
  return { from, to: current.length - end, insert: next.slice(from, next.length - end) };
}

/**
 * The transactions that make text the document of state, as another version
 * of the note from the outside (another note, or this one reloaded from disk).
 * Dispatch them together; they are no edit of the user's (changedByUser).
 *
 * - The change is the smallest single one, so the selection stays where its
 *   text is, or goes to the edge of the change when its text is gone (never
 *   out of range), and the view keeps its scroll position unless the change
 *   spans what it shows (two edits far apart make one change from the first
 *   to the last).
 * - The line separator is the new text's.
 * - The history starts anew: undo does not reach into a text that was
 *   replaced. A field stays as it is while it is configured, so the history
 *   is taken out and put back.
 */
export function replaceDocument(
  state: EditorState,
  text: string,
  separator: LineSeparator,
  compartments: DocumentCompartments,
): readonly Transaction[] {
  const replaced = state.update({
    changes: replaceDocumentSpec(state.doc, toDocument(text, separator)) ?? [],
    effects: [
      compartments.separator.reconfigure(separatorExtension(separator)),
      compartments.history.reconfigure([]),
    ],
    annotations: replacement.of(true),
  });
  const fresh = replaced.state.update({
    effects: compartments.history.reconfigure(history()),
    annotations: replacement.of(true),
  });
  return [replaced, fresh];
}

/**
 * Whether transactions changed the document by an edit of the user's, typed
 * or by a command, and not by a replacement.
 */
export function changedByUser(transactions: readonly Transaction[]): boolean {
  return transactions.some(
    (transaction) => transaction.docChanged && transaction.annotation(replacement) !== true,
  );
}
