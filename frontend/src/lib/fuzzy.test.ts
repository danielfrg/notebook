import assert from "node:assert/strict";
import { test } from "node:test";
import { fuzzyNotes, titleSegments } from "./fuzzy.ts";

test("highlight segments reconstruct the exact visible title", () => {
  const parts = titleSegments("Fuzzy Notes", [
    [0, 3],
    [6, 8],
  ]);
  assert.deepEqual(parts, [
    { text: "Fuz", match: true },
    { text: "zy ", match: false },
    { text: "No", match: true },
    { text: "tes", match: false },
  ]);
  assert.equal(parts.map((part) => part.text).join(""), "Fuzzy Notes");
});

test("subsequence matches a title and exposes title character ranges", () => {
  const [hit] = fuzzyNotes(["Fuzzy Notes.md"], "fuz");
  assert.equal(hit?.path, "Fuzzy Notes.md");
  assert.deepEqual(hit?.ranges, [[0, 3]]);
});

test("matching folds accents and case while ranges point into the original title", () => {
  const [hit] = fuzzyNotes(["¿Qué es el Estoicismo?.md"], "ESTOICO");
  assert.equal(hit?.title, "¿Qué es el Estoicismo?");
  assert.deepEqual(hit?.ranges, [
    [11, 17],
    [20, 21],
  ]);
});

test("decomposed accents stay within their original UTF-16 highlight spans", () => {
  const [hit] = fuzzyNotes(["Pen\u0303a Notes.md"], "PEÑA");
  assert.equal(hit?.title, "Pen\u0303a Notes");
  assert.deepEqual(hit?.ranges, [[0, 5]]);
});

test("a decomposed accent does not make the following letter a word start", () => {
  const hits = fuzzyNotes(["Pena.md", "Pen\u0303a.md"], "a");
  assert.deepEqual(
    hits.map((hit) => hit.path),
    ["Pena.md", "Pen\u0303a.md"],
  );
});

test("contiguous and word-start matches rank before scattered matches", () => {
  const hits = fuzzyNotes(["aXbYcZ.md", "Alpha Beta Charlie.md", "abc.md"], "abc");
  assert.deepEqual(
    hits.map((hit) => hit.path),
    ["abc.md", "Alpha Beta Charlie.md", "aXbYcZ.md"],
  );
});

test("path matching works when the title does not and leaves title ranges empty", () => {
  const [hit] = fuzzyNotes(["Projects/Fuzzy.md"], "projects");
  assert.equal(hit?.path, "Projects/Fuzzy.md");
  assert.deepEqual(hit?.ranges, []);
});

test("a long contiguous folder match never outranks a title subsequence", () => {
  const query = "a".repeat(120);
  const hits = fuzzyNotes([`${query}/Other.md`, `${"aX".repeat(120)}.md`], query);
  assert.equal(hits[0]?.path, `${"aX".repeat(120)}.md`);
});

test("limit and empty query preserve input recency order", () => {
  assert.deepEqual(
    fuzzyNotes(["Recent.md", "Older.md"], "", 1).map((hit) => hit.path),
    ["Recent.md"],
  );
  assert.equal(fuzzyNotes(["One.md", "Two.md"], "o", 1).length, 1);
});
