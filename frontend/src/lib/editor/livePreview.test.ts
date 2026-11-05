import test from "node:test";
import assert from "node:assert/strict";
import { EditorSelection, EditorState, StateEffect, type Extension } from "@codemirror/state";
import { EditorView, type DecorationSet } from "@codemirror/view";
import {
  MAX_DECORATED_LENGTH,
  collectDecorations,
  livePreview,
  pointerHeld,
  propertiesAbove,
  previewContext,
  type DecoSpec,
  type PreviewContext,
} from "./livePreview.ts";
import { notebookMarkdown } from "./markdown.ts";
import { toggleTaskAt, LivePreviewWidget } from "./widgets.ts";

const BOM = "\uFEFF";

const context: PreviewContext = {
  notes: ["Known.md", "folder/Other.md"],
  assets: ["img/pic.png"],
  path: "Here.md",
  assetUrl: (path) => `/raw/${path}`,
};

/** Where the selection is: a cursor, a range, or several ranges. */
type Where = number | { anchor: number; head: number } | { anchor: number; head?: number }[];

const selection = (where: Where): EditorSelection =>
  typeof where === "number"
    ? EditorSelection.single(where)
    : Array.isArray(where)
      ? EditorSelection.create(
          where.map(({ anchor, head }) => EditorSelection.range(anchor, head ?? anchor)),
        )
      : EditorSelection.single(where.anchor, where.head);

/** An editor state over doc as the Live Preview tests build one; crlf gives it CRLF line breaks. */
function stateOf(
  doc: string,
  where: Where,
  options: { ctx?: PreviewContext; crlf?: boolean; extensions?: Extension } = {},
) {
  return EditorState.create({
    doc,
    selection: selection(where),
    extensions: [
      notebookMarkdown(),
      previewContext.of(options.ctx ?? context),
      options.crlf ? EditorState.lineSeparator.of("\r\n") : [],
      EditorState.allowMultipleSelections.of(true),
      options.extensions ?? [],
    ],
  });
}

/** A spec as one line of text: "mark nb-strong 4-5", "line nb-h1 0", "replace 0-2", "widget 0-9 {...}". */
function show(spec: DecoSpec): string {
  switch (spec.type) {
    case "mark":
      return `mark ${spec.class} ${spec.from}-${spec.to}`;
    case "line":
      return `line ${spec.class} ${spec.from}${spec.indent === undefined ? "" : ` indent=${spec.indent}`}`;
    case "replace":
      return `replace ${spec.from}-${spec.to}`;
    case "widget":
      return `widget ${spec.from}-${spec.to} ${JSON.stringify(spec.widget)}`;
  }
}

/** The decorations of doc with the selection at where, each shown, sorted. */
function decorate(
  doc: string,
  where: Where,
  options: { ctx?: PreviewContext; crlf?: boolean } = {},
): string[] {
  return collectDecorations(stateOf(doc, where, options))
    .map(show)
    .sort();
}

const sorted = (specs: string[]): string[] => [...specs].sort();

test("an ATX heading gets its line class, and its marker is hidden while the cursor is on another line", () => {
  assert.deepEqual(decorate("# Title\nnext", 9), sorted(["line nb-h1 0", "replace 0-2"]));
  assert.deepEqual(decorate("# Title\nnext", 3), ["line nb-h1 0"]);
  // The cursor at either end of the line touches it.
  assert.deepEqual(decorate("# Title\nnext", 0), ["line nb-h1 0"]);
  assert.deepEqual(decorate("# Title\nnext", 7), ["line nb-h1 0"]);
  for (let level = 1; level <= 6; level++) {
    const marker = "#".repeat(level);
    assert.deepEqual(
      decorate(`x\n${marker} H`, 0),
      sorted([`line nb-h${level} 2`, `replace 2-${3 + level}`]),
      marker,
    );
  }
  // A closing sequence is hidden with the space before it.
  assert.deepEqual(
    decorate("## Title ##\nx", 12),
    sorted(["line nb-h2 0", "replace 0-3", "replace 8-11"]),
  );
  // A setext heading: its text lines get the class, the underline is hidden when untouched.
  assert.deepEqual(decorate("Title\n===\n\nx", 11), sorted(["line nb-h1 0", "replace 6-9"]));
  assert.deepEqual(decorate("Title\n---\n\nx", 1), ["line nb-h2 0"]);
});

test("inline marks: the text gets its class, the markers are hidden unless the selection touches the element", () => {
  const cases: [
    doc: string,
    cls: string,
    content: [number, number],
    markers: [number, number][],
  ][] = [
    [
      "a **b** c",
      "nb-strong",
      [4, 5],
      [
        [2, 4],
        [5, 7],
      ],
    ],
    [
      "a *x* c",
      "nb-em",
      [3, 4],
      [
        [2, 3],
        [4, 5],
      ],
    ],
    [
      "a _x_ c",
      "nb-em",
      [3, 4],
      [
        [2, 3],
        [4, 5],
      ],
    ],
    [
      "a ~~x~~ c",
      "nb-strike",
      [4, 5],
      [
        [2, 4],
        [5, 7],
      ],
    ],
    [
      "a ==x== c",
      "nb-highlight",
      [4, 5],
      [
        [2, 4],
        [5, 7],
      ],
    ],
    [
      "a `c` c",
      "nb-code",
      [3, 4],
      [
        [2, 3],
        [4, 5],
      ],
    ],
    [
      "a ``c`d`` c",
      "nb-code",
      [4, 7],
      [
        [2, 4],
        [7, 9],
      ],
    ],
  ];
  for (const [doc, cls, [from, to], markers] of cases) {
    const mark = `mark ${cls} ${from}-${to}`;
    const end = markers[1][1];
    assert.deepEqual(
      decorate(doc, 0),
      sorted([mark, ...markers.map(([a, b]) => `replace ${a}-${b}`)]),
      doc,
    );
    // Inside, and at either edge of the element, it is touched.
    for (const cursor of [from, 2, end])
      assert.deepEqual(decorate(doc, cursor), [mark], `${doc} at ${cursor}`);
    // Just outside it is not.
    assert.equal(decorate(doc, end + 1).length, 3, `${doc} after it`);
    assert.equal(decorate(doc, 1).length, 3, `${doc} before it`);
  }
});

test("a selection touches what it overlaps, in any direction and in any of its ranges", () => {
  const doc = "**a** and **b**";
  const hidden = (specs: string[]) => specs.filter((spec) => spec.startsWith("replace")).length;
  assert.equal(hidden(decorate(doc, 7)), 4);
  assert.equal(hidden(decorate(doc, { anchor: 3, head: 12 })), 0, "a range over both");
  assert.equal(hidden(decorate(doc, { anchor: 12, head: 3 })), 0, "the same range reversed");
  assert.equal(hidden(decorate(doc, { anchor: 6, head: 9 })), 4, "a range between them");
  assert.equal(hidden(decorate(doc, { anchor: 9, head: 6 })), 4);
  assert.equal(hidden(decorate(doc, [{ anchor: 1 }, { anchor: 14 }])), 0, "a cursor in each");
  assert.equal(
    hidden(decorate(doc, [{ anchor: 1 }, { anchor: 7 }])),
    2,
    "a cursor in the first only",
  );
  assert.deepEqual(
    decorate(doc, [{ anchor: 7 }, { anchor: 12, head: 11 }]),
    sorted(["mark nb-strong 2-3", "mark nb-strong 12-13", "replace 0-2", "replace 3-5"]),
  );
});

test("nested inline elements are decorated each by its own rule", () => {
  // ***x***, **a *b* c**, and a highlight around code.
  assert.deepEqual(
    decorate("**a *b* c** z", 13),
    sorted([
      "mark nb-strong 2-9",
      "mark nb-em 5-6",
      "replace 0-2",
      "replace 4-5",
      "replace 6-7",
      "replace 9-11",
    ]),
  );
  // The cursor in the inner element touches the outer one too.
  assert.deepEqual(decorate("**a *b* c** z", 5), sorted(["mark nb-strong 2-9", "mark nb-em 5-6"]));
  // In the outer one only, the inner one stays hidden.
  assert.deepEqual(
    decorate("**a *b* c** z", 2),
    sorted(["mark nb-strong 2-9", "mark nb-em 5-6", "replace 4-5", "replace 6-7"]),
  );
  // Markup in a heading.
  assert.deepEqual(
    decorate("# a **b**\nx", 11),
    sorted(["line nb-h1 0", "replace 0-2", "mark nb-strong 6-7", "replace 4-6", "replace 7-9"]),
  );
});

test("links show their text as a link, and hide the rest unless touched; bare URLs are URLs", () => {
  const doc = "[t](https://e.com) z";
  assert.deepEqual(decorate(doc, 20), sorted(["mark nb-link 1-2", "replace 0-1", "replace 2-18"]));
  for (const cursor of [0, 1, 10, 18])
    assert.deepEqual(decorate(doc, cursor), ["mark nb-link 1-2"], `at ${cursor}`);
  // Emphasis in the text of a link.
  assert.deepEqual(
    decorate("[*t*](u) z", 10),
    sorted([
      "mark nb-link 1-4",
      "mark nb-em 2-3",
      "replace 0-1",
      "replace 1-2",
      "replace 3-4",
      "replace 4-8",
    ]),
  );
  // A link without text keeps its markup in sight, or nothing would be left of it.
  assert.deepEqual(decorate("[](u) z", 7), []);
  // A reference link.
  assert.deepEqual(
    decorate("[t][r] z\n\n[r]: u", 8),
    sorted(["mark nb-link 1-2", "replace 0-1", "replace 2-6", "mark nb-url 15-16"]),
  );
  // Bare URLs, and autolinks whose brackets are hidden.
  assert.deepEqual(decorate("see https://e.com", 0), ["mark nb-url 4-17"]);
  assert.deepEqual(decorate("see https://e.com", 10), ["mark nb-url 4-17"]);
  assert.deepEqual(
    decorate("a <https://e.com> b", 19),
    sorted(["mark nb-url 3-16", "replace 2-3", "replace 16-17"]),
  );
  assert.deepEqual(decorate("a <https://e.com> b", 5), ["mark nb-url 3-16"]);
  assert.deepEqual(decorate("![a](https://e.com/a.png) z", 27), ["mark nb-link 0-25"]);
});

test("wikilinks collapse to a widget with their label unless touched, then they show raw", () => {
  const widget = (from: number, to: number, label: string, resolved: boolean) =>
    `widget ${from}-${to} ${JSON.stringify({ kind: "wikilink", label, resolved })}`;
  assert.deepEqual(decorate("[[Known]] z", 11), [widget(0, 9, "Known", true)]);
  assert.deepEqual(decorate("[[Nope|Alias]] z", 16), [widget(0, 14, "Alias", false)]);
  assert.deepEqual(
    decorate("[[known]] z", 11),
    [widget(0, 9, "known", true)],
    "case does not matter",
  );
  assert.deepEqual(
    decorate("[[Other]] z", 11),
    [widget(0, 9, "Other", true)],
    "a note in a folder",
  );
  assert.deepEqual(decorate("[[Known#Part]] z", 16), [widget(0, 14, "Known > Part", true)]);
  assert.deepEqual(decorate("[[Known#Part|Here]] z", 21), [widget(0, 19, "Here", true)]);
  // A link inside the note itself is resolved, and shows its heading.
  assert.deepEqual(decorate("[[#Part]] z", 11), [widget(0, 9, "Part", true)]);
  assert.deepEqual(decorate("[[#^block]] z", 13), [widget(0, 11, "^block", true)]);
  // Touched: the raw text with the class.
  assert.deepEqual(decorate("[[Known]] z", 9), ["mark nb-wikilink 0-9"]);
  assert.deepEqual(decorate("[[Nope|Alias]] z", 3), [
    "mark nb-wikilink nb-wikilink-unresolved 0-14",
  ]);
  // An embed shows a local image, or stays readable as text when it cannot show one.
  assert.deepEqual(decorate("![[pic.png|300]] z", 18), [
    'widget 0-16 {"kind":"image","src":"/raw/img/pic.png","alt":"pic.png"}',
  ]);
  assert.deepEqual(decorate("![[Nope]] z", 11), ["mark nb-link 0-9"]);
  // The context decides what resolves.
  const empty: PreviewContext = { notes: [], assets: [], path: "", assetUrl: () => "" };
  assert.deepEqual(decorate("[[Known]] z", 11, { ctx: empty }), [widget(0, 9, "Known", false)]);
  // Inside bold.
  assert.deepEqual(
    decorate("**[[Known]]** z", 15),
    sorted(["mark nb-strong 2-11", "replace 0-2", "replace 11-13", widget(2, 11, "Known", true)]),
  );
});

test("a document that links one note many times resolves it the same each time", () => {
  const doc =
    Array.from({ length: 50 }, (_, i) => (i % 2 ? "[[Known]]" : "[[Nope]]")).join(" ") + " z";
  const widgets = collectDecorations(stateOf(doc, doc.length)).filter(
    (spec) => spec.type === "widget",
  );
  assert.equal(widgets.length, 50);
  widgets.forEach((spec, i) =>
    assert.deepEqual(spec.widget, {
      kind: "wikilink",
      label: i % 2 ? "Known" : "Nope",
      resolved: i % 2 === 1,
    }),
  );
});

test("tags get their class wherever the cursor is, and a heading is no tag", () => {
  assert.deepEqual(decorate("a #tag b", 0), ["mark nb-tag 2-6"]);
  assert.deepEqual(decorate("a #tag b", 4), ["mark nb-tag 2-6"]);
  assert.deepEqual(decorate("# Heading\nx", 11), sorted(["line nb-h1 0", "replace 0-2"]));
  assert.deepEqual(
    decorate("## Heading #t\nx", 15),
    sorted(["line nb-h2 0", "replace 0-3", "mark nb-tag 11-13"]),
  );
});

test("quotes get a line class on each line, and their marker is hidden on lines without the cursor", () => {
  const doc = "> one\n> two\n\nx";
  assert.deepEqual(
    decorate(doc, 13),
    sorted(["line nb-quote 0", "line nb-quote 6", "replace 0-2", "replace 6-8"]),
  );
  assert.deepEqual(decorate(doc, 3), sorted(["line nb-quote 0", "line nb-quote 6", "replace 6-8"]));
  assert.deepEqual(
    decorate(doc, 11),
    sorted(["line nb-quote 0", "line nb-quote 6", "replace 0-2"]),
  );
  // A lazy line belongs to the quote; a nested quote hides both markers, once per line.
  assert.deepEqual(
    decorate("> one\ntwo\n\nx", 11),
    sorted(["line nb-quote 0", "line nb-quote 6", "replace 0-2"]),
  );
  assert.deepEqual(
    decorate("> > a\n\nx", 7),
    sorted(["line nb-quote 0", "replace 0-2", "replace 2-4"]),
  );
  // A marker without a space after it.
  assert.deepEqual(decorate(">a\n\nx", 4), sorted(["line nb-quote 0", "replace 0-1"]));
});

test("list items get a line class with the columns before their text, more when nested", () => {
  assert.deepEqual(decorate("- item", 0), ["line nb-list 0 indent=2"]);
  assert.deepEqual(
    decorate("- item\n    - x", 0),
    sorted([
      "line nb-list 0 indent=2",
      "line nb-list 7 indent=6",
      'widget 11-12 {"kind":"bullet"}',
    ]),
  );
  assert.deepEqual(
    decorate("1. one\n2. two", 0),
    sorted([
      "line nb-list 0 indent=3",
      "line nb-list 7 indent=3",
      "mark nb-list-number 0-2",
      "mark nb-list-number 7-9",
    ]),
  );
  assert.deepEqual(
    decorate("10)  ten", 0),
    sorted(["line nb-list 0 indent=5", "mark nb-list-number 0-3"]),
  );
  assert.deepEqual(
    decorate("- a\n\t- tab", 0),
    sorted(["line nb-list 0 indent=2", "line nb-list 4 indent=6", 'widget 5-6 {"kind":"bullet"}']),
    "a tab goes to the next stop of 4",
  );
  assert.deepEqual(decorate("- [ ] task", 0), ["line nb-list 0 indent=6"]);
  // In a quote, the columns count from the start of the line.
  assert.deepEqual(
    decorate("> - a\n\nx", 7),
    sorted([
      "line nb-quote 0",
      "line nb-list 0 indent=4",
      "replace 0-2",
      'widget 2-3 {"kind":"bullet"}',
    ]),
  );
});

test("a fenced code block gets a class on every line, its markers stay, and nothing inside is decorated", () => {
  const doc = "```js\nx *y* [[Known]] #t\n```\nz";
  assert.deepEqual(
    decorate(doc, doc.length),
    sorted([
      "line nb-codeblock nb-codeblock-first 0",
      "line nb-codeblock 6",
      "line nb-codeblock nb-codeblock-last 25",
    ]),
  );
  // The same with the cursor inside.
  assert.equal(decorate(doc, 8).length, 3);
  // One that never closes runs to the end; a one-line block is first and last.
  assert.deepEqual(
    decorate("```\na", 0),
    sorted(["line nb-codeblock nb-codeblock-first 0", "line nb-codeblock nb-codeblock-last 4"]),
  );
  assert.deepEqual(decorate("```", 0), [
    "line nb-codeblock nb-codeblock-first nb-codeblock-last 0",
  ]);
  // In a list item the lines start at the start of the line.
  assert.deepEqual(
    decorate("- a\n  ```\n  b\n  ```", 0),
    sorted([
      "line nb-list 0 indent=2",
      "line nb-codeblock nb-codeblock-first 4",
      "line nb-codeblock 10",
      "line nb-codeblock nb-codeblock-last 14",
    ]),
  );
});

test("a table gets a line class on each line and stays raw text", () => {
  const doc = "| **a** | b |\n| - | - |\n| [[Known]] | 2 |\n\nz";
  assert.deepEqual(
    decorate(doc, doc.length),
    sorted(["line nb-table 0", "line nb-table 14", "line nb-table 24"]),
  );
});

test("with the cursor in the frontmatter its lines get their class, and nothing in them is Markdown", () => {
  const doc = "---\ntitle: *a* [[Known]]\ntags: [x]\n---\n# H";
  assert.deepEqual(
    decorate(doc, 6),
    sorted([
      "line nb-frontmatter 0",
      "line nb-frontmatter 4",
      "line nb-frontmatter 25",
      "line nb-frontmatter 35",
      "line nb-h1 39",
      "replace 39-41",
    ]),
  );
});

test("with a byte order mark and CRLF line breaks every decoration is at its offset in the document", () => {
  // The document holds the BOM as its first character, and a line break is one position.
  const doc = `${BOM}---\r\ntitle: A\r\n---\r\n# Head\r\nSome **bold**\r\n`;
  const state = stateOf(doc, 2, { crlf: true });
  assert.equal(state.sliceDoc(0), doc, "the text stays the file's");
  assert.equal(state.doc.line(4).from, 18);
  assert.deepEqual(
    collectDecorations(state).map(show).sort(),
    sorted([
      "line nb-frontmatter 0",
      "line nb-frontmatter 5",
      "line nb-frontmatter 14",
      "line nb-h1 18",
      "replace 18-20",
      "mark nb-strong 32-36",
      "replace 30-32",
      "replace 36-38",
    ]),
  );
  // The same text with LF line breaks has the same offsets.
  assert.deepEqual(
    decorate(doc.replaceAll("\r\n", "\n"), 2),
    collectDecorations(state).map(show).sort(),
  );
  // The cursor on the heading line, which starts after the frontmatter's three lines.
  assert.ok(!decorate(doc, 19, { crlf: true }).includes("replace 18-20"));
});

test("Obsidian comments: their lines get a class of their own and nothing else", () => {
  assert.deepEqual(decorate("%% hidden **x** %%\nz", 20), ["line nb-comment-line 0"]);
  assert.deepEqual(decorate("  %%x%%", 0), ["line nb-comment-line 0"]);
  // A block: from a line that opens one to the line that closes it, both included.
  assert.deepEqual(
    decorate("a\n\n%%\n# not a heading\n#tag\n%%\n\nb", 0),
    sorted([
      "line nb-comment-line 3",
      "line nb-comment-line 6",
      "line nb-comment-line 22",
      "line nb-comment-line 27",
    ]),
  );
  assert.deepEqual(
    decorate("%% starts here\nmiddle\nends here %%\n*e*", 0),
    sorted([
      "line nb-comment-line 0",
      "line nb-comment-line 15",
      "line nb-comment-line 22",
      "mark nb-em 36-37",
      "replace 35-36",
      "replace 37-38",
    ]),
  );
  // A block that never closes is no comment; a %% in the middle of a line is none either.
  assert.deepEqual(
    decorate("%%\n**a**", 0),
    sorted(["mark nb-strong 5-6", "replace 3-5", "replace 6-8"]),
  );
  assert.deepEqual(decorate("a %% b %%", 0), []);
  assert.deepEqual(decorate("%", 0), []);
  // In code and frontmatter, %% is their text.
  assert.deepEqual(
    decorate("```\n%% a %%\n```", 0).filter((spec) => spec.includes("comment")),
    [],
  );
  assert.deepEqual(
    decorate("---\nk: '%% a %%'\n---\n", 5).filter((spec) => spec.includes("comment")),
    [],
  );
});

test("a dense note with a comment keeps its Markdown decorations below the size limit", () => {
  const doc = "%% hidden %%\n" + "*x* ".repeat(100_000);
  assert.ok(doc.length < MAX_DECORATED_LENGTH);

  const specs = collectDecorations(stateOf(doc, 0));
  assert.ok(specs.length > 125_000, "enough decorations to exercise a large comment filter");
  assert.deepEqual(specs[0], { type: "line", from: 0, class: "nb-comment-line" });
  assert.ok(specs.some((spec) => spec.type === "mark" && spec.class === "nb-em"));
});

test("a document over the size limit gets no decorations", () => {
  assert.equal(MAX_DECORATED_LENGTH, 500_000);
  const line = "# Heading with **bold** and [[Known]]\n";
  const big = line
    .repeat(Math.ceil((MAX_DECORATED_LENGTH + 1) / line.length))
    .slice(0, MAX_DECORATED_LENGTH + 1);
  assert.equal(big.length, 500_001);
  assert.deepEqual(collectDecorations(stateOf(big, 0)), []);
  // At the limit it is decorated.
  assert.ok(collectDecorations(stateOf(big.slice(0, MAX_DECORATED_LENGTH), 0)).length > 0);
});

test("collectDecorations is pure: the same output twice, and the state is left as it was", () => {
  const doc =
    "---\na: 1\n---\n# T\n\n**b** [[Known]] `c` > x\n\n> q\n- [ ] t\n```\nc\n```\n%% c %%";
  const state = stateOf(doc, [{ anchor: 20 }, { anchor: 40, head: 35 }]);
  const before = { doc: state.sliceDoc(0), selection: state.selection.toJSON() };
  const first = collectDecorations(state);
  const second = collectDecorations(state);
  assert.deepEqual(second, first);
  assert.ok(first.length > 10);
  assert.deepEqual({ doc: state.sliceDoc(0), selection: state.selection.toJSON() }, before);
  // The specs come in document order.
  for (let i = 1; i < first.length; i++)
    assert.ok(first[i - 1].from <= first[i].from, `order at ${i}`);
});

/** The decorations the Live Preview field gives a state, as "from-to" ranges with their kind. */
function rendered(state: EditorState): { set: DecorationSet; ranges: string[] } {
  const sets = state
    .facet(EditorView.decorations)
    .filter((each): each is DecorationSet => typeof each !== "function");
  assert.equal(sets.length, 1, "one decoration set");
  const ranges: string[] = [];
  sets[0].between(0, state.doc.length, (from, to, deco) => {
    const spec = deco.spec as { class?: string; widget?: unknown; attributes?: { style?: string } };
    const kind = spec.widget
      ? "widget"
      : spec.class
        ? `${deco.point && from === to ? "line" : "mark"} ${spec.class}`
        : "replace";
    ranges.push(
      `${kind} ${from}-${to}${spec.attributes?.style ? ` ${spec.attributes.style}` : ""}`,
    );
  });
  return { set: sets[0], ranges };
}

test("livePreview decorates the editor with the collected specs, and follows the selection", () => {
  const doc = "# T\n\na **b** [[Known]]\n- x";
  let state = stateOf(doc, 0, { extensions: livePreview() });
  assert.deepEqual(
    rendered(state).ranges.sort(),
    sorted([
      "line nb-h1 0-0",
      "mark nb-strong 9-10",
      "replace 7-9",
      "replace 10-12",
      "widget 13-22",
      "line nb-list 23-23 --nb-list-indent: 2",
      "widget 23-24",
    ]),
  );
  state = state.update({ selection: { anchor: 9 } }).state;
  assert.deepEqual(
    rendered(state).ranges.sort(),
    sorted([
      "line nb-h1 0-0",
      "replace 0-2",
      "mark nb-strong 9-10",
      "widget 13-22",
      "line nb-list 23-23 --nb-list-indent: 2",
      "widget 23-24",
    ]),
  );
  // An edit: the text moves and the decorations with it, from the new tree.
  state = state.update({ changes: { from: 5, insert: "zz " }, selection: { anchor: 0 } }).state;
  assert.ok(rendered(state).ranges.includes("mark nb-strong 12-13"));
  // The widget shows the label, and a wikilink widget compares by its kind.
  const widgets: { toDOM?: unknown; eq(other: unknown): boolean }[] = [];
  rendered(state).set.between(0, state.doc.length, (_from, _to, deco) => {
    if (deco.spec.widget) widgets.push(deco.spec.widget);
  });
  assert.equal(widgets.length, 2);
  assert.ok(widgets.every((widget) => widget.eq(widget)));
});

test("livePreview recomputes on a new context or a new tree, and keeps its set otherwise", () => {
  const doc =
    "[[Known]] z\n" + "Filler text, long enough that the first parse stops early.\n".repeat(200);
  const marker = StateEffect.define<null>();
  let state = stateOf(doc, doc.length, { extensions: livePreview() });
  const resolved = (s: EditorState) => {
    let found: unknown;
    rendered(s).set.between(0, 9, (_f, _t, deco) => {
      found = (deco.spec.widget as { kind?: { resolved: boolean } } | undefined)?.kind?.resolved;
    });
    return found;
  };
  assert.equal(resolved(state), true);
  // The tree the first parse left behind is finished on the way: the next transaction carries a new one.
  const next = state.update({ effects: marker.of(null) }).state;
  assert.notEqual(rendered(next).set, rendered(state).set, "a new tree recomputes");
  state = next;
  const same = state.update({ effects: marker.of(null) }).state;
  assert.equal(rendered(same).set, rendered(state).set, "nothing new keeps the set");
  // Another context, as when the list of notes changes, by reconfiguration.
  const reconfigured = state.update({
    effects: StateEffect.reconfigure.of([
      notebookMarkdown(),
      previewContext.of({ ...context, notes: [] }),
      livePreview(),
    ]),
  }).state;
  assert.equal(resolved(reconfigured), false);
});

test("livePreview gives no decorations to a document over the limit", () => {
  const big = "**b** ".repeat(Math.ceil((MAX_DECORATED_LENGTH + 1) / 6));
  const state = stateOf(big, 0, { extensions: livePreview() });
  assert.deepEqual(rendered(state).ranges, []);
});

test("the default context resolves nothing", () => {
  const state = EditorState.create({
    doc: "[[Known]] z",
    selection: { anchor: 11 },
    extensions: [notebookMarkdown()],
  });
  assert.deepEqual(collectDecorations(state).map(show), [
    `widget 0-9 ${JSON.stringify({ kind: "wikilink", label: "Known", resolved: false })}`,
  ]);
});

test("off-cursor bullets, task markers, and rules become widgets", () => {
  assert.deepEqual(
    decorate("- item\nnext", 7),
    sorted(["line nb-list 0 indent=2", 'widget 0-1 {"kind":"bullet"}']),
  );
  assert.deepEqual(decorate("- item", 0), ["line nb-list 0 indent=2"]);
  assert.deepEqual(
    decorate("1. item\nnext", 9),
    sorted(["line nb-list 0 indent=3", "mark nb-list-number 0-2"]),
  );
  assert.deepEqual(
    decorate("- [ ] task\n- [x] done\nnext", 22),
    sorted([
      "line nb-list 0 indent=6",
      "line nb-list 11 indent=6",
      'widget 0-5 {"kind":"task","checked":false}',
      'widget 11-16 {"kind":"task","checked":true}',
      "mark nb-task-done 17-26",
    ]),
  );
  assert.deepEqual(decorate("---\n\nx", 6), ['widget 0-3 {"kind":"rule"}']);
  assert.deepEqual(decorate("---\n\nx", 1), []);
});

test("task text keeps inline decorations", () => {
  const doc = "- [x] **bold** #tag [[Known]]\n  continuation\n\nnext";
  const specs = decorate(doc, doc.length);
  assert.ok(specs.includes("mark nb-strong 8-12"), "bold text inside a task");
  assert.ok(specs.includes("mark nb-tag 15-19"), "tag inside a task");
  assert.ok(
    specs.includes('widget 20-29 {"kind":"wikilink","label":"Known","resolved":true}'),
    "wikilink inside a task",
  );
});

test("checked task text styling reaches continuation lines", () => {
  const doc = "- [x] **bold** #tag [[Known]]\n  continuation\n\nnext";
  const specs = decorate(doc, doc.length);
  assert.ok(
    specs.includes("mark nb-task-done 6-44"),
    "checked styling reaches the continuation line",
  );
});

test("local images render; remote, unresolved, and non-image targets stay as links", () => {
  assert.deepEqual(decorate("![[pic.png]] z", 14), [
    'widget 0-12 {"kind":"image","src":"/raw/img/pic.png","alt":"pic.png"}',
  ]);
  assert.deepEqual(decorate("![x](img/pic.png) z", 19), [
    'widget 0-17 {"kind":"image","src":"/raw/img/pic.png","alt":"x"}',
  ]);
  for (const source of [
    "![x](https://e.com/a.png)",
    "![x](missing.png)",
    "![[Known]]",
    "![[missing.txt]]",
  ]) {
    const specs = decorate(`${source} z`, source.length + 2);
    assert.ok(!specs.some((spec) => spec.includes('"kind":"image"')), source);
    assert.ok(
      specs.some((spec) => spec.startsWith("mark nb-link")),
      source,
    );
  }
});

test("local markdown image targets with spaces resolve without allowing remote images", () => {
  const ctx = { ...context, assets: ["img/my pic.png"] };
  assert.deepEqual(decorate("![x](<img/my pic.png>) z", 24, { ctx }), [
    'widget 0-22 {"kind":"image","src":"/raw/img/my pic.png","alt":"x"}',
  ]);
  assert.deepEqual(decorate("![x](img/my%20pic.png) z", 24, { ctx }), [
    'widget 0-22 {"kind":"image","src":"/raw/img/my pic.png","alt":"x"}',
  ]);
  assert.deepEqual(decorate("![x](<https://example.com/a.png>) z", 35, { ctx }), [
    "mark nb-link 0-33",
  ]);
});

test("frontmatter collapses only when the selection is elsewhere", () => {
  const doc = "---\ntags: [a, b]\ntitle: Hi\n---\nbody";
  assert.deepEqual(decorate(doc, doc.length), [
    'widget 0-30 {"kind":"properties","yaml":"tags: [a, b]\\ntitle: Hi"}',
  ]);
  assert.deepEqual(
    decorate(doc, 7),
    sorted([
      "line nb-frontmatter 0",
      "line nb-frontmatter 4",
      "line nb-frontmatter 17",
      "line nb-frontmatter 27",
    ]),
  );
  const state = stateOf(doc, doc.length, { extensions: livePreview() });
  let block = false;
  rendered(state).set.between(0, doc.length, (_from, _to, deco) => {
    if ((deco.spec.widget as { kind?: { kind?: string } } | undefined)?.kind?.kind === "properties")
      block = deco.spec.block;
  });
  assert.equal(block, true, "properties is a block replacement from the StateField");
});

test("empty frontmatter stays editable without an empty properties panel", () => {
  assert.deepEqual(
    decorate("---\n---\nbody", 11),
    sorted(["line nb-frontmatter 0", "line nb-frontmatter 4"]),
  );
  assert.deepEqual(
    decorate("---\n# comment\n---\nbody", 22),
    sorted(["line nb-frontmatter 0", "line nb-frontmatter 4", "line nb-frontmatter 14"]),
  );
});

test("toggleTaskAt changes only the checkbox character, including CRLF documents", () => {
  for (const [input, expected] of [
    ["- [ ] a\nother", "- [x] a\nother"],
    ["- [x] a\nother", "- [ ] a\nother"],
  ]) {
    const state = stateOf(input, input.length);
    const change = toggleTaskAt(state, 3);
    assert.ok(change);
    assert.equal(change.userEvent, "input.toggle");
    assert.equal(state.update(change).state.sliceDoc(0), expected);
  }
  const crlf = stateOf("- [ ] a\r\nnext", 0, { crlf: true });
  assert.equal(crlf.update(toggleTaskAt(crlf, 3)!).state.sliceDoc(0), "- [x] a\r\nnext");
  assert.equal(toggleTaskAt(crlf, 10), null);
});

test("widgets compare payloads and expose interactive task and wikilink events", () => {
  assert.ok(
    new LivePreviewWidget({ kind: "bullet" }).eq(new LivePreviewWidget({ kind: "bullet" })),
  );
  assert.ok(
    !new LivePreviewWidget({ kind: "task", checked: false }).eq(
      new LivePreviewWidget({ kind: "task", checked: true }),
    ),
  );
  assert.ok(
    !new LivePreviewWidget({ kind: "task", checked: false }, 0, 5).eq(
      new LivePreviewWidget({ kind: "task", checked: false }, 6, 11),
    ),
    "task positions participate in DOM reuse",
  );
  assert.ok(
    !new LivePreviewWidget({ kind: "wikilink", label: "Known", resolved: true }, 0, 9).eq(
      new LivePreviewWidget({ kind: "wikilink", label: "Known", resolved: true }, 4, 13),
    ),
    "wikilink positions participate in DOM reuse",
  );
  assert.equal(
    new LivePreviewWidget({ kind: "task", checked: false }).ignoreEvent(new Event("mousedown")),
    false,
  );
  assert.equal(
    new LivePreviewWidget({ kind: "wikilink", label: "Known", resolved: true }).ignoreEvent(
      new Event("mousedown"),
    ),
    false,
  );
});

test("a task widget after an edit above it toggles the shifted source marker", () => {
  const doc = "- [ ] task\n[[Known]]\nend";
  let state = stateOf(doc, doc.length, { extensions: livePreview() });
  state = state.update({
    changes: { from: 0, insert: "intro\n" },
    selection: { anchor: doc.length + 6 },
  }).state;
  const tasks: { from: number; to: number }[] = [];
  rendered(state).set.between(0, state.doc.length, (from, to, deco) => {
    if ((deco.spec.widget as LivePreviewWidget | undefined)?.kind.kind === "task")
      tasks.push({ from, to });
  });
  assert.deepEqual(tasks, [{ from: 6, to: 11 }]);
  state = state.update(toggleTaskAt(state, tasks[0].to - 2)!).state;
  assert.equal(state.doc.toString(), "intro\n- [x] task\n[[Known]]\nend");
});

test("a held mouse keeps the decorations through its selections, and the release or a key rebuilds them", () => {
  const doc = "> quote\n\n# Title";
  const title = doc.indexOf("Title");
  let state = stateOf(doc, title, { extensions: livePreview() });
  const before = rendered(state).set;
  state = state.update({ effects: pointerHeld.of(true) }).state;
  state = state.update({ selection: { anchor: 3 }, userEvent: "select.pointer" }).state;
  assert.equal(
    rendered(state).set,
    before,
    "the markers stay as they were while the mouse is held",
  );
  state = state.update({ effects: pointerHeld.of(false) }).state;
  assert.ok(
    rendered(state).ranges.includes(`replace ${doc.indexOf("#")}-${title}`),
    "the release hides the heading marker",
  );
  assert.ok(!rendered(state).ranges.includes("replace 0-2"), "and shows the quote marker");

  state = stateOf(doc, title, { extensions: livePreview() });
  state = state.update({ effects: pointerHeld.of(true) }).state;
  state = state.update({ selection: { anchor: 3 }, userEvent: "select" }).state;
  assert.notEqual(rendered(state).set, before, "a move by the keyboard rebuilds them");
});

test("propertiesAbove finds the closing line of a shown properties panel from the line just below it", () => {
  const doc = "---\ntags: [a]\n---\n# Title\n\nText";
  const body = doc.indexOf("# Title");
  let state = stateOf(doc, body + 2, { extensions: livePreview() });
  assert.deepEqual(propertiesAbove(state, body + 2), { from: doc.indexOf("---\n#"), to: body - 1 });
  assert.equal(propertiesAbove(state, doc.indexOf("Text")), null);
  // With the selection in the frontmatter the panel is raw text, and there is nothing to step over.
  state = stateOf(doc, 5, { extensions: livePreview() });
  assert.equal(propertiesAbove(state, body + 2), null);
});
