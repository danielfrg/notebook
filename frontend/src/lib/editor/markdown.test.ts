import test from "node:test";
import assert from "node:assert/strict";
import { markdown, pasteURLAsLink } from "@codemirror/lang-markdown";
import { ensureSyntaxTree } from "@codemirror/language";
import { EditorState, type ChangeSpec, type Extension } from "@codemirror/state";
import type { Tree } from "@lezer/common";
import { getStyleTags, tagHighlighter, tags, type Tag } from "@lezer/highlight";
import { GFM } from "@lezer/markdown";
import { NodeName, NodeTag, notebookMarkdown, pasteLink, pasteLinkHandler } from "./markdown.ts";

const BOM = "\uFEFF";
const notebook = notebookMarkdown();
const parse = (text: string): Tree => notebook.language.parser.parse(text);
/** CodeMirror's Markdown with GFM and nothing else: what a text without Obsidian syntax must parse to. */
const gfm = markdown({ extensions: GFM }).language.parser;

/** Every node of tree but the document, as "Name from-to", in document order. */
function outline(tree: Tree): string[] {
  const nodes: string[] = [];
  tree.iterate({
    enter(node) {
      if (!node.type.isTop) nodes.push(`${node.name} ${node.from}-${node.to}`);
    },
  });
  return nodes;
}

/** The text of every node of text named name. */
function spans(text: string, name: string): string[] {
  const found: string[] = [];
  parse(text).iterate({
    enter(node) {
      if (node.name === name) found.push(text.slice(node.from, node.to));
    },
  });
  return found;
}

test("frontmatter runs from the fence on the first line to the end of the line that closes it", () => {
  const cases: [text: string, nodes: string[]][] = [
    ["---\ntitle: A\n---\nBody", ["Frontmatter 0-16", "FrontmatterMark 0-3", "FrontmatterMark 13-16", "Paragraph 17-21"]],
    ["---\ntitle: A\n...\nBody", ["Frontmatter 0-16", "FrontmatterMark 0-3", "FrontmatterMark 13-16", "Paragraph 17-21"]],
    // A byte order mark is a character of the first line, before the fence.
    [BOM + "---\ntitle: A\n---\nBody", ["Frontmatter 1-17", "FrontmatterMark 1-4", "FrontmatterMark 14-17", "Paragraph 18-22"]],
    // A CR ends the text of its line, in frontmatter as in any block.
    ["---\r\ntitle: A\r\n---\r\nBody", ["Frontmatter 0-19", "FrontmatterMark 0-3", "FrontmatterMark 15-18", "Paragraph 20-24"]],
    [BOM + "---\r\ntitle: A\r\n---\r\nBody", ["Frontmatter 1-20", "FrontmatterMark 1-4", "FrontmatterMark 16-19", "Paragraph 21-25"]],
    // White space after a fence is part of the line and not of the mark.
    ["--- \ntitle: A\n...\t \nBody", ["Frontmatter 0-19", "FrontmatterMark 0-3", "FrontmatterMark 14-17", "Paragraph 20-24"]],
    ["---\n---\nBody", ["Frontmatter 0-7", "FrontmatterMark 0-3", "FrontmatterMark 4-7", "Paragraph 8-12"]],
    ["---\na: 1\n---", ["Frontmatter 0-12", "FrontmatterMark 0-3", "FrontmatterMark 9-12"]],
    // The first closing fence ends it; a later "---" is a rule again.
    [
      "---\na: 1\n---\nText\n\n---\n",
      ["Frontmatter 0-12", "FrontmatterMark 0-3", "FrontmatterMark 9-12", "Paragraph 13-17", "HorizontalRule 19-22"],
    ],
    // The YAML is no Markdown: no heading, list, emphasis, link, tag, highlight, code, quote or fence.
    [
      "---\n# c\n- i\n*e* [[w]] #t ==h== `c`\n\n> q\n```\n---\n# H",
      ["Frontmatter 0-47", "FrontmatterMark 0-3", "FrontmatterMark 44-47", "ATXHeading1 48-51", "HeaderMark 48-49"],
    ],
  ];
  for (const [text, nodes] of cases) assert.deepEqual(outline(parse(text)), nodes, JSON.stringify(text));
  assert.ok(parse("---\na: 1\n---").topNode.firstChild?.type.is("Block"), "frontmatter is a block");
});

test("a fence that is not the first line, or that nothing closes, is no frontmatter", () => {
  for (const text of [
    "---\ntitle: A\nBody\n", // never closed
    "---",
    "---\n",
    "\n---\na: 1\n---\n", // a blank line first
    "Intro\n---\na: 1\n---\n",
    BOM + "Intro\n---\na: 1\n---\n",
    " ---\na: 1\n---\n", // indented
    "----\na: 1\n---\n",
    "--- title\na: 1\n---\n",
    "---\na: 1\n ---\nBody", // an indented fence closes nothing
    "> ---\n> a: 1\n> ---\n",
  ]) {
    assert.deepEqual(outline(parse(text)), outline(gfm.parse(text)), JSON.stringify(text));
  }
});

test("after any edit the editor's tree has the frontmatter a fresh parse finds", () => {
  // The body is long enough that a parse after an edit at one end reuses the tree at the other.
  const body = "A line of body text, long enough for the parser to reuse its tree.\n".repeat(4);
  let state = EditorState.create({ doc: `---\ntitle: A\ntags: [x]\n---\n# Heading\n\n${body}`, extensions: [notebook] });
  assert.ok(ensureSyntaxTree(state, state.doc.length, 10_000));
  const edits: [label: string, edit: (text: string) => ChangeSpec, frontmatter: boolean][] = [
    ["type in the YAML", (text) => ({ from: text.indexOf("A\n") + 1, insert: "lpha" }), true],
    ["type at the end", (text) => ({ from: text.length, insert: "More text.\n" }), true],
    ["delete the closing fence", (text) => ({ from: text.indexOf("---\n#"), to: text.indexOf("---\n#") + 4 }), false],
    ["close it far below", (text) => ({ from: text.length, insert: "---\n" }), true],
    ["delete that fence", (text) => ({ from: text.length - 4, to: text.length }), false],
    ["close it under the YAML again", (text) => ({ from: text.indexOf("# Heading"), insert: "---\n" }), true],
    ["break the opening fence", () => ({ from: 0, to: 1 }), false],
    ["mend it", () => ({ from: 0, insert: "-" }), true],
    ["add a byte order mark", () => ({ from: 0, insert: BOM }), true],
  ];
  for (const [label, edit, frontmatter] of edits) {
    state = state.update({ changes: edit(state.doc.toString()) }).state;
    const tree = ensureSyntaxTree(state, state.doc.length, 10_000);
    assert.ok(tree, label);
    const nodes = outline(tree);
    assert.deepEqual(nodes, outline(parse(state.doc.toString())), label);
    assert.equal(nodes.some((node) => node.startsWith("Frontmatter ")), frontmatter, label);
  }
});

test("wikilinks and embeds are nodes of their own, not Markdown links", () => {
  const cases: [text: string, links: string[], embeds: string[]][] = [
    ["[[a]]", ["[[a]]"], []],
    ["[[a|b]]", ["[[a|b]]"], []],
    ["[[a#h]]", ["[[a#h]]"], []],
    ["[[folder/a]]", ["[[folder/a]]"], []],
    ["[[#h]]", ["[[#h]]"], []], // a heading of the note itself
    ["[[#^block]]", ["[[#^block]]"], []],
    ["![[img.png|300]]", [], ["![[img.png|300]]"]],
    ["[[a]] and [[b]]", ["[[a]]", "[[b]]"], []],
    ["See [[a]], ![[b.png]].", ["[[a]]"], ["![[b.png]]"]],
    ["- [[a]]", ["[[a]]"], []],
    ["# [[a]]", ["[[a]]"], []],
    ["**[[a]]**", ["[[a]]"], []],
  ];
  for (const [text, links, embeds] of cases) {
    assert.deepEqual(spans(text, NodeName.Wikilink), links, text);
    assert.deepEqual(spans(text, NodeName.Embed), embeds, text);
    assert.deepEqual([...spans(text, "Link"), ...spans(text, "Image")], [], text);
  }
});

test("brackets that are not one whole wikilink on one line make none", () => {
  for (const text of [
    "[[a", // unclosed
    "[[a]",
    "![[a",
    "[[]]", // empty
    "[[ ]]",
    "[[#]]",
    "[[|b]]",
    "[[a\nb]]", // over a line break
    "\\[[a]]", // escaped
    "`[[a]]`", // code
    "``x [[a]] y``",
    "```\n[[a]]\n```",
    "    [[a]]",
  ]) {
    assert.deepEqual([...spans(text, NodeName.Wikilink), ...spans(text, NodeName.Embed)], [], JSON.stringify(text));
  }
});

test("a tag is a # after white space or an opening mark, then letters, digits, _, - and /", () => {
  const cases: [text: string, tags: string[]][] = [
    ["#tag", ["#tag"]],
    ["#a/b", ["#a/b"]],
    ["#título", ["#título"]],
    ["#a_b-c", ["#a_b-c"]],
    ["#y1984", ["#y1984"]],
    ["one #a, two #b.", ["#a", "#b"]],
    ["line\n#a", ["#a"]],
    ["(#tag)", ["#tag"]],
    ["**#tag**", ["#tag"]],
    ["#a/ b", ["#a"]], // a trailing slash is no part of it
    ["- [ ] #todo", ["#todo"]],
    ["## Heading #tag", ["#tag"]],
    // none
    ["# Heading", []],
    ["#123", []], // digits only
    ["a#b", []],
    ["[[a]]#b", []],
    ["https://x.com/#top", []],
    ["x.com/#top", []],
    ["#/a", []],
    ["\\#tag", []],
    ["`#tag`", []],
    ["```\n#tag\n```", []],
    ["[[a#h]]", []],
  ];
  for (const [text, want] of cases) assert.deepEqual(spans(text, NodeName.Tag), want, JSON.stringify(text));
});

test("==text== is a highlight, with no white space just inside its marks", () => {
  const cases: [text: string, highlights: string[]][] = [
    ["==x==", ["==x=="]],
    ["a ==b c== d", ["==b c=="]],
    ["==a== and ==b==", ["==a==", "==b=="]],
    ["==**a**==", ["==**a**=="]],
    ["a == b", []],
    ["====", []],
    ["== x ==", []],
    ["==x ==", []],
    ["== x==", []],
    ["`==x==`", []],
  ];
  for (const [text, want] of cases) assert.deepEqual(spans(text, NodeName.Highlight), want, JSON.stringify(text));
  assert.deepEqual(outline(parse("==x==")), ["Paragraph 0-5", "Highlight 0-5", "HighlightMark 0-2", "HighlightMark 3-5"]);
});

test("every Obsidian node has a highlight tag, and those that extend a standard tag fall back to it", () => {
  const want = new Map<string, Tag>([
    [NodeName.Frontmatter, NodeTag.Frontmatter],
    [NodeName.FrontmatterMark, tags.processingInstruction],
    [NodeName.Wikilink, NodeTag.Wikilink],
    [NodeName.Embed, NodeTag.Embed],
    [NodeName.Tag, NodeTag.Tag],
    [NodeName.Highlight, NodeTag.Highlight],
    [NodeName.HighlightMark, tags.processingInstruction],
  ]);
  const seen = new Set<string>();
  parse("---\na: 1\n---\n[[a]] ![[b.png]] #c ==d==").iterate({
    enter(node) {
      const tag = want.get(node.name);
      if (tag === undefined) return;
      seen.add(node.name);
      assert.ok(getStyleTags(node)?.tags.includes(tag), node.name);
    },
  });
  assert.deepEqual([...seen].sort(), [...want.keys()].sort());

  const standard = tagHighlighter([
    { tag: tags.meta, class: "meta" },
    { tag: tags.link, class: "link" },
    { tag: tags.labelName, class: "label" },
  ]);
  assert.deepEqual(
    [NodeTag.Frontmatter, NodeTag.Wikilink, NodeTag.Embed, NodeTag.Tag].map((tag) => standard.style([tag])),
    ["meta", "link", "link", "label"],
  );
});

test("GFM parses as it does in CodeMirror's Markdown with GFM", () => {
  const cases: [text: string, node: string][] = [
    ["~~x~~", "Strikethrough"],
    ["- [ ] t", "Task"],
    ["| a | b |\n| - | - |\n| 1 | 2 |", "Table"],
    ["see https://example.com/#top ok", "URL"],
    ["www.example.com/a#b", "URL"],
    ["*a* **b** `c` [d](e) ![f](g) <h@i.jk>", "Link"],
    ["# H\n\n> q\n\n1. o\n\n---\n\nT\n=\n\n```js\nx\n```", "FencedCode"],
  ];
  for (const [text, node] of cases) {
    const nodes = outline(parse(text));
    assert.deepEqual(nodes, outline(gfm.parse(text)), text);
    assert.ok(nodes.some((each) => each.startsWith(node + " ")), `${text} has a ${node}`);
  }
});

/** An editor state with the note's Markdown and from..to selected, its line breaks LF or CRLF. */
function pasteState(doc: string, from: number, to: number, crlf: boolean, extensions: Extension = []): EditorState {
  return EditorState.create({
    doc: crlf ? doc.replaceAll("\n", "\r\n") : doc,
    selection: { anchor: from, head: to },
    extensions: [notebook, crlf ? EditorState.lineSeparator.of("\r\n") : [], extensions],
  });
}

test("a URL pasted over plain text makes it a link, and brings no line break", () => {
  for (const crlf of [false, true]) {
    const lineBreak = crlf ? "\r\n" : "\n";
    const state = pasteState("see word here\nnext\n", 4, 8, crlf);
    const paste = (text: string) => {
      const spec = pasteLink(state, text);
      assert.ok(spec, `${JSON.stringify(text)}${crlf ? " in CRLF" : ""}`);
      return state.update(spec).state.sliceDoc(0);
    };
    // Copied with or without white space and line breaks around it, the URL is the same.
    for (const text of ["https://example.com", "https://example.com\n", "https://example.com\r\n", "  https://example.com  ", "\thttps://example.com\n\n"]) {
      const pasted = paste(text);
      assert.equal(pasted, `see [word](https://example.com) here${lineBreak}next${lineBreak}`, JSON.stringify(text));
      // Every line break is the document's: no bare LF in a CRLF note, no CR in an LF one.
      assert.doesNotMatch(crlf ? pasted.replaceAll("\r\n", "") : pasted, crlf ? /[\r\n]/ : /\r/, JSON.stringify(text));
    }
    assert.equal(paste("www.example.com"), `see [word](https://www.example.com) here${lineBreak}next${lineBreak}`);
    assert.equal(paste("mailto:a@b.c"), `see [word](mailto:a@b.c) here${lineBreak}next${lineBreak}`);
    // Plain text inside emphasis is plain text still, as long as no mark touches the selection.
    const emphasis = pasteState("a *some word* b\n", 4, 7, crlf);
    assert.equal(emphasis.update(pasteLink(emphasis, "https://e.com") ?? {}).state.sliceDoc(0), `a *s[ome](https://e.com) word* b${lineBreak}`);
  }
});

test("any other paste is left to CodeMirror's, which gives line breaks the document's", () => {
  for (const crlf of [false, true]) {
    const plain = pasteState("see word here\nnext\n", 4, 8, crlf);
    // Not one URL: two words, two lines, nothing, no link target.
    for (const text of ["https://a.com b", "https://a.com\nhttps://b.com", "https://a.com\r\nnext", "", "  ", "not a url", "example.com", "ftp://a.com"]) {
      assert.equal(pasteLink(plain, text), null, JSON.stringify(text));
    }
    assert.equal(pasteLink(pasteState("see word here\n", 4, 4, crlf), "https://example.com"), null, "no selection");
    assert.equal(
      pasteLink(pasteState("see word here\n", 4, 8, crlf, EditorState.readOnly.of(true)), "https://example.com"),
      null,
      "read-only",
    );
    // A selection that is no plain text, or that a node starts or ends in.
    for (const [doc, selected] of [
      ["a [word](x) b\n", "word"],
      ["a `word` b\n", "word"],
      ["```\nword\n```\n", "word"],
      ["a [[word]] b\n", "word"],
      ["a ![[word.png]] b\n", "word"],
      ["a #word b\n", "word"],
      ["---\ntitle: word\n---\nbody\n", "word"],
      ["a *word* b\n", "word"], // its marks touch it
      ["one **two** three\n", "one **two"],
      ["see https://e.com here\n", "e.com"],
      ["ab\n\ncd\n", "b\n"], // a paragraph ends inside it
      ["ab\n\ncd\n", "\nc"], // a paragraph starts inside it
    ]) {
      const from = doc.indexOf(selected);
      const state = pasteState(doc, from, from + selected.length, crlf);
      assert.equal(pasteLink(state, "https://example.com"), null, JSON.stringify([doc, selected]));
    }
  }
});

test("the note's Markdown pastes links with its own handler, not with lang-markdown's", () => {
  const flat = (extension: Extension): unknown[] => (Array.isArray(extension) ? extension.flatMap(flat) : [extension]);
  const support = flat(notebookMarkdown().support);
  assert.ok(support.includes(pasteLinkHandler));
  assert.ok(!support.includes(pasteURLAsLink));
});
