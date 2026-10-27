import test from "node:test";
import assert from "node:assert/strict";
import {
  baseName,
  joinPath,
  noteTitle,
  parentFolder,
  validateEntryName,
  withMd,
} from "./paths.ts";

test("baseName is the last segment and noteTitle also drops .md", () => {
  for (const [path, base, title] of [
    ["a/b/¿Qué?.md", "¿Qué?.md", "¿Qué?"],
    ["Note.md", "Note.md", "Note"],
    ["100% #1.md", "100% #1.md", "100% #1"],
    ["a,b [c].md", "a,b [c].md", "a,b [c]"],
    ["Course Z - On love - Lesson 1.md", "Course Z - On love - Lesson 1.md", "Course Z - On love - Lesson 1"],
    ["notes.d/Q1..Q2.md", "Q1..Q2.md", "Q1..Q2"],
    ["a.md.md", "a.md.md", "a.md"], // only the last extension goes
    ["x/y.MD", "y.MD", "y.MD"], // the server only calls a lower-case .md a note
    ["a/folder", "folder", "folder"], // a folder has no extension to drop
  ] as const) {
    assert.equal(baseName(path), base, `baseName(${path})`);
    assert.equal(noteTitle(path), title, `noteTitle(${path})`);
  }
});

test("parentFolder is the folder part and is empty at the root", () => {
  for (const [path, want] of [
    ["a/b.md", "a"],
    ["b.md", ""],
    ["a/b/c.md", "a/b"],
    ["a/b", "a"],
    ["a", ""],
  ] as const) {
    assert.equal(parentFolder(path), want, path);
  }
});

test("joinPath puts a name in a folder and round trips with parentFolder and baseName", () => {
  assert.equal(joinPath("", "x.md"), "x.md");
  assert.equal(joinPath("a", "x.md"), "a/x.md");
  assert.equal(joinPath("a/b", "x.md"), "a/b/x.md");
  for (const folder of ["", "a", "a/b c"]) {
    for (const name of ["¿Qué es el Estoicismo?.md", "100% #1.md", "a,b [c].md"]) {
      const path = joinPath(folder, name);
      assert.equal(parentFolder(path), folder, path);
      assert.equal(baseName(path), name, path);
    }
  }
});

test("withMd appends .md unless the name already ends in it", () => {
  for (const [name, want] of [
    ["x", "x.md"],
    ["x.md", "x.md"],
    ["100% #1", "100% #1.md"],
    ["a.b", "a.b.md"],
    ["a.md.md", "a.md.md"],
    ["x.MD", "x.MD.md"], // not a note to the server, so it is not one here
  ] as const) {
    assert.equal(withMd(name), want, name);
  }
});

test("validateEntryName gives a reason for a name the vault would refuse, and null otherwise", () => {
  const valid = [
    "100% #1",
    "¿Qué es el Estoicismo?",
    "a,b [c]",
    "Course Z - On love - Lesson 1",
    "Q1..Q2",
    "trailing.",
    "日本語",
    "a".repeat(255), // the longest component the server takes
    "é".repeat(127), // 254 bytes: bytes count, not characters
  ];
  const invalid = [
    "",
    "   ",
    "a/b",
    "/a",
    "a/",
    "a\\b",
    ".hidden",
    ".",
    "..",
    "...",
    "a\nb",
    "a\tb",
    "a\x7fb",
    "bad\ud800",
    "a".repeat(256),
    "é".repeat(128), // 256 bytes
  ];
  for (const name of valid) assert.equal(validateEntryName(name), null, JSON.stringify(name));
  for (const name of invalid) {
    const reason = validateEntryName(name);
    assert.equal(typeof reason, "string", JSON.stringify(name.slice(0, 20)));
    assert.notEqual(reason, "", JSON.stringify(name.slice(0, 20)));
  }
});

test("validateEntryName rejects lone surrogates when isWellFormed is unavailable", () => {
  const descriptor = Object.getOwnPropertyDescriptor(String.prototype, "isWellFormed");
  try {
    Object.defineProperty(String.prototype, "isWellFormed", { configurable: true, value: undefined });
    assert.match(validateEntryName("bad\ud800") ?? "", /invalid Unicode/);
    assert.equal(validateEntryName("日本語"), null);
    assert.equal(validateEntryName("📝"), null);
  } finally {
    if (descriptor) Object.defineProperty(String.prototype, "isWellFormed", descriptor);
    else delete (String.prototype as { isWellFormed?: unknown }).isWellFormed;
  }
});
