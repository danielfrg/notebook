import test from "node:test";
import assert from "node:assert/strict";
import { uniqueNoteName, renameTarget, validateNoteName } from "./entry-names.ts";

test("unique note names ignore .md and case, and fill numeric gaps", () => {
  assert.equal(uniqueNoteName([], "Untitled"), "Untitled");
  assert.equal(uniqueNoteName(["untitled.md"], "Untitled"), "Untitled 2");
  assert.equal(uniqueNoteName(["Untitled.md", "UNTITLED 3.md"], "Untitled"), "Untitled 2");
  assert.equal(uniqueNoteName(["draft", "Draft 2.md"], "Draft"), "Draft 3");
});

test("rename keeps the folder and note extension", () => {
  assert.equal(renameTarget("a/b.md", "c"), "a/c.md");
  assert.equal(renameTarget("a/b.md", "c.md"), "a/c.md");
  assert.equal(renameTarget("a/folder", "new folder"), "a/new folder");
  assert.throws(() => renameTarget("a/b.md", "bad/name"), /cannot contain/);
});

test("note validation counts the final .md bytes", () => {
  assert.equal(validateNoteName("x".repeat(252)), null);
  assert.match(validateNoteName("x".repeat(253)) ?? "", /too long/);
  assert.match(validateNoteName("bad/name") ?? "", /cannot contain/);
  assert.match(validateNoteName(".") ?? "", /cannot start/);
});
