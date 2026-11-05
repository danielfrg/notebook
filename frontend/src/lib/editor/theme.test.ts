import test from "node:test";
import assert from "node:assert/strict";
import { highlightingFor } from "@codemirror/language";
import { EditorState, type Extension } from "@codemirror/state";
import { EditorView } from "@codemirror/view";
import { tags, type Tag } from "@lezer/highlight";
import { sourceHighlight } from "./highlight.ts";
import { NodeTag, notebookMarkdown } from "./markdown.ts";
import { editorTheme } from "./theme.ts";

/*
 * The editor theme and the source highlighting, read back from the CSS that
 * their style modules mount. Their colours have to be theme tokens, so that
 * replacing theme.css restyles the editor as it does the rest of the app, in
 * light and dark alike.
 */

type Rule = { selector: string; declarations: Map<string, string> };

/** The CSS rules of the style modules an extension mounts. A theme's own class reads "&". */
function rules(extension: Extension): Rule[] {
  const modules = EditorState.create({ extensions: extension }).facet(EditorView.styleModule);
  assert.ok(modules.length > 0, "the extension mounts styles");
  const css = modules.map((module) => module.getRules()).join("\n");
  return [...css.matchAll(/([^{}]+)\{([^{}]*)\}/g)].map(([, selector, body]) => ({
    selector: selector.trim(),
    declarations: new Map(
      body
        .split(";")
        .map((declaration) => declaration.trim())
        .filter(Boolean)
        .map((declaration) => {
          const colon = declaration.indexOf(":");
          return [declaration.slice(0, colon).trim(), declaration.slice(colon + 1).trim()] as const;
        }),
    ),
  }));
}

/** The properties that may carry a colour. */
const COLOR_PROPERTY = /color|background|border|outline|shadow|fill|stroke|text-decoration/;
/** What such a declaration may hold besides var() references: sizes and keywords, none a colour. */
const NOT_A_COLOR =
  /^(?:-?\d*\.?\d+(?:px|em|rem|%)?|solid|dashed|dotted|none|line-through|underline|transparent|inherit|currentcolor)$/i;

/** Every declaration that names a colour some other way than through a var() token. */
function colorFindings(found: Rule[]): string[] {
  const findings: string[] = [];
  for (const { selector, declarations } of found) {
    for (const [property, value] of declarations) {
      if (!COLOR_PROPERTY.test(property)) continue;
      const rest = value
        .replace(/var\(--[\w-]+\)/g, " ")
        .split(/\s+/)
        .filter(Boolean);
      if (!rest.every((token) => NOT_A_COLOR.test(token)))
        findings.push(`${selector} { ${property}: ${value} }`);
    }
  }
  return findings;
}

test("the editor theme sets a readable column on a transparent page, with the accent and one focus ring", () => {
  const theme = rules(editorTheme);
  /** The declarations of the theme's rule for a selector, written with "&" for the editor. */
  const rule = (selector: string) => {
    const found = theme.find((each) => each.selector.replace(/\.\u037c\w+/g, "&") === selector);
    assert.ok(
      found,
      `a rule for ${selector} in:\n${theme.map((each) => each.selector).join("\n")}`,
    );
    return found.declarations;
  };
  const editor = rule("&");
  assert.equal(editor.get("background-color"), "transparent");
  assert.equal(editor.get("color"), "var(--text-primary)");
  assert.equal(editor.get("font-size"), "var(--nb-editor-size)");
  const scroller = rule("& .cm-scroller");
  assert.equal(scroller.get("scrollbar-gutter"), "stable");
  assert.equal(scroller.get("font-family"), "var(--nb-editor-font)");
  assert.equal(scroller.get("line-height"), "var(--nb-editor-line)");
  const content = rule("& .cm-content");
  assert.equal(content.get("max-width"), "var(--nb-editor-width)");
  assert.match(content.get("margin") ?? "", /^0 auto$/);
  assert.equal(content.get("caret-color"), "var(--nb-accent)");
  assert.equal(rule("& .cm-cursor, & .cm-dropCursor").get("border-left-color"), "var(--nb-accent)");
  // The drawn selection, focused or not, and the native one.
  const selection = [
    "&.cm-focused > .cm-scroller > .cm-selectionLayer .cm-selectionBackground",
    "& .cm-selectionBackground",
    "& .cm-content ::selection",
  ];
  assert.equal(rule(selection.join(", ")).get("background"), "var(--nb-selection)");
  // One focus treatment: the app's ring on the editor, in place of CodeMirror's dotted outline.
  assert.equal(rule("&.cm-focused").get("outline"), "var(--focus-ring)");
  assert.deepEqual(
    theme
      .filter((each) => each.declarations.has("outline"))
      .map((each) => each.selector.replace(/\.\u037c\w+/g, "&")),
    ["&.cm-focused"],
  );
  assert.deepEqual(colorFindings(theme), []);
});

test("source highlighting styles Markdown and the Obsidian nodes, each of those by its own tag", () => {
  const state = EditorState.create({ extensions: [notebookMarkdown(), sourceHighlight] });
  const css = rules(sourceHighlight);
  /** The declarations that the highlighter gives text with a tag. */
  const style = (tag: Tag, label: string) => {
    const classes = highlightingFor(state, [tag]);
    assert.ok(classes, `${label} is highlighted`);
    const declarations = css.filter((each) => classes.split(" ").includes(each.selector.slice(1)));
    assert.ok(declarations.length > 0, `${label} has a rule`);
    return new Map(declarations.flatMap((each) => [...each.declarations]));
  };
  const cases: [label: string, tag: Tag, property: string][] = [
    ["a heading", tags.heading1, "font-weight"],
    ["a level 3 heading", tags.heading3, "font-weight"],
    ["emphasis", tags.emphasis, "font-style"],
    ["strong", tags.strong, "font-weight"],
    ["strikethrough", tags.strikethrough, "text-decoration"],
    ["inline code", tags.monospace, "font-family"],
    ["a link", tags.link, "color"],
    ["a URL", tags.url, "color"],
    ["a quote", tags.quote, "color"],
    ["a mark (list marks, #, *, `, >)", tags.processingInstruction, "color"],
    ["a task marker", tags.atom, "color"],
    ["frontmatter", NodeTag.Frontmatter, "color"],
    ["a wikilink", NodeTag.Wikilink, "color"],
    ["an embed", NodeTag.Embed, "color"],
    ["a tag", NodeTag.Tag, "color"],
    ["a highlight", NodeTag.Highlight, "background-color"],
  ];
  for (const [label, tag, property] of cases)
    assert.ok(style(tag, label).has(property), `${label} sets ${property}`);
  // The Obsidian nodes are styled through their own tags, not only through the standard tag they extend.
  for (const [label, tag, parent] of [
    ["frontmatter", NodeTag.Frontmatter, tags.meta],
    ["a wikilink", NodeTag.Wikilink, tags.link],
    ["an embed", NodeTag.Embed, tags.link],
    ["a tag", NodeTag.Tag, tags.labelName],
  ] as const) {
    assert.notEqual(highlightingFor(state, [tag]), highlightingFor(state, [parent]), label);
  }
  assert.deepEqual(colorFindings(css), []);
});

test("the colour check catches literals, also when they hide among sizes", () => {
  const fake = (body: string): Rule[] => [
    {
      selector: ".x",
      declarations: new Map(
        body.split(";").map((d) => d.split(":").map((s) => s.trim()) as [string, string]),
      ),
    },
  ];
  assert.deepEqual(
    colorFindings(
      fake("color: var(--a); border-left: 2px solid var(--b); background: transparent"),
    ),
    [],
  );
  for (const literal of [
    "color: red",
    "background: #fff",
    "border: 1px solid rgb(0 0 0)",
    "outline: 2px solid black",
  ]) {
    assert.equal(colorFindings(fake(literal)).length, 1, literal);
  }
});
