import test from "node:test";
import assert from "node:assert/strict";
import { formatRoute, parseRoute } from "./router.ts";

test("a path survives the trip through the hash, whatever its name", () => {
  for (const path of [
    "¿Qué es el Estoicismo?.md",
    "100% #1.md",
    "a/b c/d.md",
    "a,b [c].md",
    "Course Z - On love - Lesson 1.md",
    "a+b.md",
    "Q1..Q2.md",
    "日本語/ノート.md",
    "50%2F50.md", // a name with a percent escape in it is not a folder separator
    "a%.md",
  ]) {
    assert.deepEqual(parseRoute(formatRoute("notes", path)), { vault: "notes", path }, path);
  }
});

test("formatRoute encodes each segment and keeps the slashes between them", () => {
  for (const [path, want] of [
    ["a/b c/d.md", "#/notes/a/b%20c/d.md"],
    ["¿Qué es el Estoicismo?.md", "#/notes/%C2%BFQu%C3%A9%20es%20el%20Estoicismo%3F.md"],
    ["100% #1.md", "#/notes/100%25%20%231.md"],
    ["a,b [c].md", "#/notes/a%2Cb%20%5Bc%5D.md"],
    ["50%2F50.md", "#/notes/50%252F50.md"],
    [null, "#/notes"],
    [undefined, "#/notes"],
    ["", "#/notes"],
  ] as const) {
    assert.equal(formatRoute("notes", path), want, String(path));
  }
  assert.equal(formatRoute("notes"), "#/notes");
});

test("parseRoute reads the hashes a browser can hand it", () => {
  const none = { vault: null, path: null };
  for (const [hash, want] of [
    ["", none],
    ["#", none],
    ["#/", none],
    ["#foo", none], // an anchor, not a route
    ["#/notes", { vault: "notes", path: null }],
    ["#/notes/", { vault: "notes", path: null }],
    ["/notes/a.md", { vault: "notes", path: "a.md" }], // the # is optional
    ["#/notes/a/b.md", { vault: "notes", path: "a/b.md" }],
    ["#/notes/a%20b/%E2%82%AC.md", { vault: "notes", path: "a b/€.md" }],
    ["#/notes/a//b.md", { vault: "notes", path: null }], // an empty segment is no path
    ["#/notes/a/b.md/", { vault: "notes", path: null }],
    ["#//a.md", none],
  ] as const) {
    assert.deepEqual(parseRoute(hash), want, JSON.stringify(hash));
  }
});

test("a malformed percent escape does not throw: the path (or the whole route) is dropped", () => {
  for (const [hash, want] of [
    ["#/notes/100%.md", { vault: "notes", path: null }],
    ["#/notes/a/%E0%A4%A.md", { vault: "notes", path: null }],
    ["#/notes/%", { vault: "notes", path: null }],
    ["#/%E0%A4%A/a.md", { vault: null, path: null }],
    ["#/%", { vault: null, path: null }],
    ["#/%%/a.md", { vault: null, path: null }],
  ] as const) {
    assert.deepEqual(parseRoute(hash), want, hash);
  }
});

test("decoded routes reject unsafe vaults and path components", () => {
  assert.deepEqual(parseRoute("#/BAD/a.md"), { vault: null, path: null });
  assert.deepEqual(parseRoute("#/notes/%2Ehidden/a.md"), { vault: "notes", path: null });
  assert.deepEqual(parseRoute("#/notes/a%2Fb.md"), { vault: "notes", path: null });
  assert.deepEqual(parseRoute(formatRoute("notes", ["é".repeat(120), "é".repeat(120), "é".repeat(20) + ".md"].join("/"))), { vault: "notes", path: null });
});
