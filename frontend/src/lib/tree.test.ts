import test from "node:test";
import assert from "node:assert/strict";
import { ancestors, buildTree, visibleRows, type TreeNode } from "./tree.ts";

const note = (path: string) => ({ path, size: 1, modified: "2026-10-26T12:00:00Z" });
const names = (nodes: readonly TreeNode[]) => nodes.map((node) => node.name);
/** The tree as indented lines; folders end in a slash. */
const outline = (nodes: readonly TreeNode[], depth = 0): string[] =>
  nodes.flatMap((node) => [
    `${"  ".repeat(depth)}${node.name}${node.kind === "folder" ? "/" : ""}`,
    ...(node.kind === "folder" ? outline(node.children, depth + 1) : []),
  ]);

test("folders come before notes, each in natural, case- and accent-insensitive order", () => {
  const tree = buildTree({
    folders: ["Zeta", "alpha", "Écrits", "Beta"],
    notes: [
      "Note 10.md",
      "Note 2.md",
      "Zed.md",
      "note 1.md",
      "Árbol.md",
      "apple.md",
      "Ñandú.md",
      "nube.md",
    ].map(note),
  });
  assert.deepEqual(names(tree), [
    "alpha",
    "Beta",
    "Écrits",
    "Zeta",
    "apple",
    "Árbol",
    "Ñandú",
    "note 1",
    "Note 2",
    "Note 10",
    "nube",
    "Zed",
  ]);
});

test("notes are ordered by title, as they are shown, not by file name", () => {
  // As file names "Note 2.md" sorts before "Note.md", as titles "Note" is first.
  const tree = buildTree({ folders: [], notes: ["Note 2.md", "Note.md", "Note-a.md"].map(note) });
  assert.deepEqual(names(tree), ["Note", "Note 2", "Note-a"]);
});

test("the order does not depend on the order of the listing, and the listing is left alone", () => {
  const folders = ["b", "a", "A"];
  const paths = ["résumé.md", "Resume.md", "resume.md", "Note 02.md", "Note 2.md", "b/x.md", "a/y.md"];
  // Frozen, so that sorting or changing the listing in place throws.
  const build = (folders: string[], paths: string[]) =>
    buildTree(
      Object.freeze({
        folders: Object.freeze([...folders]),
        notes: Object.freeze(paths.map((path) => Object.freeze(note(path)))),
      }),
    );
  assert.deepEqual(build([...folders].reverse(), [...paths].reverse()), build(folders, paths));
});

test("empty folders are listed, and notes land in their folder even when a parent is not in the list", () => {
  const tree = buildTree({
    folders: ["a", "a/b", "empty"],
    notes: [note("a/b/c.md"), note("a/d.md"), note("top.md"), note("x/y/z.md")],
  });
  assert.deepEqual(outline(tree), ["a/", "  b/", "    c", "  d", "empty/", "x/", "  y/", "    z", "top"]);
  assert.deepEqual(buildTree({ folders: [], notes: [] }), []);
});

test("nodes carry what the sidebar needs: a title, the path, and for a note its size and time", () => {
  const modified = "2026-10-26T12:00:00Z";
  const tree = buildTree({ folders: ["a"], notes: [{ path: "a/¿Qué es?.md", size: 42, modified }] });
  assert.deepEqual(tree, [
    {
      kind: "folder",
      name: "a",
      path: "a",
      children: [{ kind: "note", name: "¿Qué es?", path: "a/¿Qué es?.md", size: 42, modified }],
    },
  ]);
});

test("visibleRows lists the children of expanded folders only, with their depth", () => {
  const tree = buildTree({
    folders: ["a", "a/b", "empty"],
    notes: [note("a/b/c.md"), note("a/d.md"), note("top.md")],
  });
  const rows = (open: string[]) =>
    visibleRows(tree, new Set(open)).map(({ node, depth }) => `${depth}:${node.path}`);
  const closed = ["0:a", "0:empty", "0:top.md"];
  assert.deepEqual(rows([]), closed);
  assert.deepEqual(rows(["a"]), ["0:a", "1:a/b", "1:a/d.md", "0:empty", "0:top.md"]);
  assert.deepEqual(rows(["a", "a/b"]), ["0:a", "1:a/b", "2:a/b/c.md", "1:a/d.md", "0:empty", "0:top.md"]);
  assert.deepEqual(rows(["a/b"]), closed, "a folder inside a closed one stays hidden");
  assert.deepEqual(rows(["empty"]), closed, "an empty folder has nothing to show");
  assert.deepEqual(visibleRows([], new Set(["a"])), []);
});

test("ancestors lists the folders above a path, outermost first", () => {
  for (const [path, want] of [
    ["a/b/c.md", ["a", "a/b"]],
    ["a/b.md", ["a"]],
    ["c.md", []],
    ["a/b", ["a"]],
    ["a", []],
    ["", []],
  ] as const) {
    assert.deepEqual(ancestors(path), want, JSON.stringify(path));
  }
});
