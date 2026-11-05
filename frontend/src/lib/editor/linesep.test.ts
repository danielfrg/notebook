import test from "node:test";
import assert from "node:assert/strict";
import { EditorState } from "@codemirror/state";
import { detectLineSeparator } from "./linesep.ts";

const BOM = "\uFEFF";

test("detectLineSeparator names the one line break a text uses, or says it mixes them", () => {
  const cases: [text: string, separator: ReturnType<typeof detectLineSeparator>][] = [
    ["a\nb", "\n"],
    ["a\nb\n", "\n"],
    ["a\r\nb\r\n", "\r\n"],
    ["\r\n", "\r\n"],
    ["abc", "\n"], // no line break at all
    ["", "\n"],
    ["a\r\nb\nc", "mixed"],
    ["a\nb\r\n", "mixed"],
    ["a\rb", "mixed"], // a bare CR, which no editor state can keep
    ["a\r", "mixed"],
    ["a\r\r\nb", "mixed"],
    // a byte order mark is no line break
    [BOM + "a\r\nb", "\r\n"],
    [BOM + "a\nb", "\n"],
    [BOM, "\n"],
  ];
  for (const [text, separator] of cases)
    assert.equal(detectLineSeparator(text), separator, JSON.stringify(text));
});

test("a CRLF text comes back whole from an editor state that uses CRLF", () => {
  for (const doc of ["a\r\nb\r\n", BOM + "---\r\ntitle: A\r\n---\r\nBody\r\n"]) {
    const crlf = EditorState.create({ doc, extensions: [EditorState.lineSeparator.of("\r\n")] });
    assert.equal(crlf.sliceDoc(0), doc, JSON.stringify(doc));
    // Without the separator the state keeps the lines but joins them with LF.
    assert.equal(
      EditorState.create({ doc }).sliceDoc(0),
      doc.replaceAll("\r\n", "\n"),
      JSON.stringify(doc),
    );
  }
});
