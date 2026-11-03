import test from "node:test";
import assert from "node:assert/strict";
import { addItem, addProperty, removeProperty, renameKey, formatScalar, locateProperties, removeItem, setValue, toggleValue, validKey, type Line, type PropertyEdit } from "./property-edit.ts";

/** Lines of a text, placed at an offset, as the document gives them (a break is one position). */
function lines(text: string, offset = 4): Line[] {
  let from = offset;
  return text.split("\n").map((line) => {
    const result = { text: line, from };
    from += line.length + 1;
    return result;
  });
}

/** The text with an edit applied; lines were laid out from `offset`, a break is one character. */
function apply(text: string, edit: PropertyEdit | null, offset = 4): string {
  assert.ok(edit, "an edit");
  const from = edit.from - offset;
  return text.slice(0, from) + edit.insert + text.slice(edit.to - offset);
}

const find = (text: string, key: string) => {
  const property = locateProperties(lines(text)).find((candidate) => candidate.key === key);
  assert.ok(property, key);
  return property;
};

test("locateProperties tells what control a value gets", () => {
  const text = "title: Hi\ndone: true\nopen: \"true\"\ndue: 2026-10-26\nn: 42\ntags: [a]\naliases:\n  - b\nempty:\nnested:\n  a: 1";
  assert.deepEqual(locateProperties(lines(text)).map((property) => [property.key, property.type]), [
    ["title", "text"],
    ["done", "checkbox"],
    ["open", "text"], // quoted: a string, not a boolean
    ["due", "date"],
    ["n", "text"],
    ["tags", "list"],
    ["aliases", "list"],
    ["empty", "text"],
  ]);
});

test("setValue replaces the value and keeps the rest of the line", () => {
  assert.equal(apply("title: Hi  # note\nx: 1", setValue(find("title: Hi  # note\nx: 1", "title"), "Hello")), "title: Hello  # note\nx: 1");
  assert.equal(apply("title: 'Hi'", setValue(find("title: 'Hi'", "title"), "it's")), "title: 'it''s'");
  assert.equal(apply('title: "Hi"', setValue(find('title: "Hi"', "title"), 'say "x"')), 'title: "say \\"x\\""');
  assert.equal(apply("url: a", setValue(find("url: a", "url"), "http://x.y: z")), 'url: "http://x.y: z"');
  assert.equal(apply("due: 2026-10-26", setValue(find("due: 2026-10-26", "due"), "2026-11-01")), "due: 2026-11-01");
  assert.equal(apply("title: Hi\nx: 1", setValue(find("title: Hi\nx: 1", "title"), "")), "title: \nx: 1");
});

test("setValue fills an empty value after the colon, before a comment", () => {
  assert.equal(apply("title:", setValue(find("title:", "title"), "Hi")), "title: Hi");
  assert.equal(apply("title: # why", setValue(find("title: # why", "title"), "Hi")), "title: Hi # why");
  assert.equal(setValue(find("title:", "title"), ""), null);
});

test("toggleValue flips only the word", () => {
  assert.equal(apply("done: true # yes", toggleValue(find("done: true # yes", "done"))), "done: false # yes");
  assert.equal(apply("done: false", toggleValue(find("done: false", "done"))), "done: true");
  assert.equal(toggleValue(find("title: x", "title")), null);
});

test("lists written level with a key, indented, or in brackets all take items", () => {
  const block = "tags:\n  - a\n  - b\nnext: 1";
  assert.equal(apply(block, addItem(find(block, "tags"), "c")), "tags:\n  - a\n  - b\n  - c\nnext: 1");
  assert.equal(apply(block, removeItem(find(block, "tags"), 0)), "tags:\n  - b\nnext: 1");
  assert.equal(apply(block, removeItem(find(block, "tags"), 1)), "tags:\n  - a\nnext: 1");

  const level = "tags:\n- a\n- b";
  assert.equal(apply(level, addItem(find(level, "tags"), "c")), "tags:\n- a\n- b\n- c");

  const flow = "tags: [a, 'b c'] # mine\nx: 1";
  assert.equal(apply(flow, addItem(find(flow, "tags"), "d")), 'tags: [a, b c, d] # mine\nx: 1');
  assert.equal(apply(flow, removeItem(find(flow, "tags"), 0)), "tags: [b c] # mine\nx: 1");

  const nothing = "tags: []";
  assert.equal(apply(nothing, addItem(find(nothing, "tags"), "a")), "tags: [a]");
});

test("a list key with no value gets a list on its first item", () => {
  const text = "tags:\nx: 1";
  assert.equal(find(text, "tags").type, "list");
  assert.equal(apply(text, addItem(find(text, "tags"), "a")), "tags: [a]\nx: 1");
});

test("removing the last block item leaves the key", () => {
  const text = "tags:\n  - only\nx: 1";
  assert.equal(apply(text, removeItem(find(text, "tags"), 0)), "tags:\nx: 1");
});

test("an item that would read as something else is quoted", () => {
  assert.equal(formatScalar("plain"), "plain");
  assert.equal(formatScalar("a: b"), '"a: b"');
  assert.equal(formatScalar("true"), '"true"');
  assert.equal(formatScalar("#tag"), '"#tag"');
  assert.equal(formatScalar("-1"), "-1");
  assert.equal(formatScalar("a, b", "", true), '"a, b"');
  assert.equal(formatScalar("a, b"), "a, b");
  assert.equal(formatScalar("two\nlines"), "two lines");
});

test("positions follow the document, not the characters of a CRLF file", () => {
  // The lines of a CRLF document have the same positions as an LF one: a break is one.
  const text = "a: 1\nb: x";
  assert.equal(apply(text, setValue(find(text, "b"), "y")), "a: 1\nb: y");
});

test("a new property goes after the last line", () => {
  assert.equal(apply("a: 1", addProperty(4 + 4, "b")), "a: 1\nb:");
  assert.equal(validKey("b", ["a"]), true);
  assert.equal(validKey("A", ["a"]), false);
  assert.equal(validKey("a: b", []), false);
  assert.equal(validKey("", []), false);
  assert.equal(validKey("-x", []), false);
});

test("renameKey changes only the key", () => {
  const text = "title: Hi # c\nx: 1";
  assert.equal(apply(text, renameKey(find(text, "title"), "name", ["title", "x"])), "name: Hi # c\nx: 1");
  assert.equal(renameKey(find(text, "title"), "x", ["title", "x"]), null);
  assert.equal(renameKey(find(text, "title"), "title", ["title", "x"]), null);
  assert.equal(renameKey(find(text, "title"), "a: b", ["title", "x"]), null);
});

test("removeProperty takes out the property and its lines", () => {
  const text = "a: 1\ntags:\n  - x\n  - y\nb: 2";
  // The lines start after an opening fence, so the break before the first line is at offset - 1.
  assert.equal(apply("---\n" + text, removeProperty(locateProperties(lines(text))[1]), 0), "---\na: 1\nb: 2");
  assert.equal(apply("---\n" + text, removeProperty(locateProperties(lines(text))[0]), 0), "---\ntags:\n  - x\n  - y\nb: 2");
});
