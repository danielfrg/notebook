import { ensureSyntaxTree, syntaxTree } from "@codemirror/language";
import { Facet, StateField, type EditorState, type Extension, type Range } from "@codemirror/state";
import { Decoration, EditorView, type DecorationSet } from "@codemirror/view";
import type { SyntaxNode } from "@lezer/common";
import { parseWikilink, resolveAsset, resolveNote, type Wikilink } from "../wikilinks.ts";
import { NodeName } from "./markdown.ts";
import { LivePreviewWidget, taskToggleExtension } from "./widgets.ts";

/*
 * Live Preview, as Obsidian has it: the Markdown of a note stays its text, and
 * decorations style that text and hide its markers, except on the element or
 * the line that the selection touches, where the markers show so that they can
 * be edited. Decorations only change how the text looks: nothing here changes
 * the document.
 *
 * collectDecorations reads the syntax tree into plain specs (DecoSpec), which
 * tests can compare; livePreview turns them into CodeMirror decorations.
 */

/** What the widgets of Live Preview show. */
export type WidgetKind =
  | { kind: "bullet" }
  | { kind: "task"; checked: boolean }
  | { kind: "rule" }
  | { kind: "image"; src: string; alt: string }
  | { kind: "properties"; yaml: string }
  | { kind: "wikilink"; label: string; resolved: boolean };

/**
 * One decoration, as positions in the document (where a line break is one
 * position, CRLF or not): a class over a range, a class on a line (from is the
 * line's start; indent is the columns before the text of a list item), a range
 * hidden, or a range shown as a widget.
 */
export type DecoSpec =
  | { type: "mark"; from: number; to: number; class: string }
  | { type: "line"; from: number; class: string; indent?: number }
  | { type: "replace"; from: number; to: number }
  | { type: "widget"; from: number; to: number; widget: WidgetKind };

/** What the decorations know of the vault: its notes and images, the path of this note, and the URL of an image. */
export type PreviewContext = {
  notes: readonly string[];
  assets: readonly string[];
  path: string;
  assetUrl(path: string): string;
};

const EMPTY_CONTEXT: PreviewContext = { notes: [], assets: [], path: "", assetUrl: () => "" };

/** The context of the open note, which the host provides; without one, no link resolves. */
export const previewContext = Facet.define<PreviewContext, PreviewContext>({
  combine: (values) => values[0] ?? EMPTY_CONTEXT,
});

/**
 * Over this many characters a document gets no decorations: they are rebuilt
 * over the whole document on every change and every move of the cursor.
 */
export const MAX_DECORATED_LENGTH = 500_000;

/** How long collectDecorations waits for the parser, in milliseconds, before it takes the tree parsed so far. */
const PARSE_TIMEOUT = 100;

const HEADING = /^(?:ATX|Setext)Heading([1-6])$/;

/** The inline elements that wrap text in a mark at either end, with the name of their marks and the class of their text. */
const WRAPPED: Record<string, { mark: string; class: string }> = {
  Emphasis: { mark: "EmphasisMark", class: "nb-em" },
  StrongEmphasis: { mark: "EmphasisMark", class: "nb-strong" },
  Strikethrough: { mark: "StrikethroughMark", class: "nb-strike" },
  [NodeName.Highlight]: { mark: NodeName.HighlightMark, class: "nb-highlight" },
  InlineCode: { mark: "CodeMark", class: "nb-code" },
};

/** The nodes whose URL is part of their own markup, and no bare URL. */
const URL_OWNERS = new Set(["Link", "Image", "Autolink"]);

/** The blocks whose text is never Markdown, and holds no Obsidian comment. */
const RAW_BLOCKS = new Set(["FencedCode", "CodeBlock", NodeName.Frontmatter]);

const COMMENT = "%%";

/** The columns that text takes, a tab going to the next stop of 4, as CommonMark counts them. */
function columns(text: string): number {
  let column = 0;
  for (const char of text) column = char === "\t" ? column + 4 - (column % 4) : column + 1;
  return column;
}

const children = (node: SyntaxNode, name: string): SyntaxNode[] => node.getChildren(name);

/**
 * The text a wikilink shows: its alias; else its target, with the heading
 * after it as Obsidian shows "Note > Heading"; else, for a link inside the
 * note itself, the heading alone. The alias of an embed is a size, not a label.
 */
function wikilinkLabel(link: Wikilink): string {
  if (link.alias !== null && !link.embed) return link.alias;
  if (link.target === "") return link.heading ?? "";
  return link.heading === null ? link.target : `${link.target} > ${link.heading}`;
}

/** Collects the specs of one state: one walk of the tree, then the comments. */
class Collector {
  readonly specs: DecoSpec[] = [];
  /** The ranges of code and frontmatter, in document order. */
  readonly rawBlocks: { from: number; to: number }[] = [];
  readonly #state: EditorState;
  readonly #context: PreviewContext;
  /** Whether each wikilink target resolves, for a note that links one target many times. */
  readonly #resolved = new Map<string, boolean>();
  /** The starts of the lines that have a line class already, by class, as nested quotes share their lines. */
  readonly #lines = new Map<string, Set<number>>();

  constructor(state: EditorState) {
    this.#state = state;
    this.#context = state.facet(previewContext);
  }

  /** Whether a selection range overlaps from..to, its ends included. */
  touched(from: number, to: number): boolean {
    return this.#state.selection.ranges.some((range) => range.from <= to && range.to >= from);
  }

  /** Whether a selection range lies on a line of from..to. */
  linesTouched(from: number, to: number): boolean {
    const { doc } = this.#state;
    return this.touched(doc.lineAt(from).from, doc.lineAt(to).to);
  }

  mark(from: number, to: number, cls: string): void {
    if (from < to) this.specs.push({ type: "mark", from, to, class: cls });
  }

  hide(from: number, to: number): void {
    if (from < to) this.specs.push({ type: "replace", from, to });
  }

  widget(from: number, to: number, widget: WidgetKind): void {
    this.specs.push({ type: "widget", from, to, widget });
  }

  /** Gives the line at pos a class, once. */
  line(pos: number, cls: string, indent?: number): void {
    const from = this.#state.doc.lineAt(pos).from;
    let starts = this.#lines.get(cls);
    if (starts === undefined) this.#lines.set(cls, (starts = new Set()));
    if (starts.has(from)) return;
    starts.add(from);
    this.specs.push(indent === undefined ? { type: "line", from, class: cls } : { type: "line", from, class: cls, indent });
  }

  /** Gives every line of from..to a class, the first and last ones with their own as well when asked. */
  lines(from: number, to: number, cls: string, edges = false): void {
    const { doc } = this.#state;
    const first = doc.lineAt(from).number;
    const last = doc.lineAt(to).number;
    for (let number = first; number <= last; number++) {
      const extra = edges ? `${number === first ? ` ${cls}-first` : ""}${number === last ? ` ${cls}-last` : ""}` : "";
      this.line(doc.line(number).from, cls + extra);
    }
  }

  /** Decorates a node; false when nothing inside it is to be decorated. */
  enter(node: SyntaxNode): boolean {
    const { name } = node;
    const heading = HEADING.exec(name);
    if (heading) return this.heading(node, Number(heading[1]));
    const wrapped = WRAPPED[name];
    if (wrapped) return this.wrapped(node, wrapped.mark, wrapped.class);
    switch (name) {
      case "Link":
        return this.link(node);
      case "Image":
        this.image(node);
        return false;
      case "Autolink":
        return this.autolink(node);
      case "URL":
        if (!URL_OWNERS.has(node.parent?.name ?? "")) this.mark(node.from, node.to, "nb-url");
        return false;
      case NodeName.Wikilink:
      case NodeName.Embed:
        this.wikilink(node);
        return false;
      case NodeName.Tag:
        this.mark(node.from, node.to, "nb-tag");
        return false;
      case "Blockquote":
        this.lines(node.from, node.to, "nb-quote");
        return true;
      case "QuoteMark":
        this.quoteMark(node);
        return false;
      case "ListMark":
        this.listMark(node);
        return false;
      case "Task":
        this.task(node);
        return false;
      case "HorizontalRule":
        if (!this.linesTouched(node.from, node.to)) this.widget(node.from, node.to, { kind: "rule" });
        return false;
      case "FencedCode":
      case "CodeBlock":
        this.rawBlocks.push({ from: node.from, to: node.to });
        this.lines(node.from, node.to, "nb-codeblock", true);
        return false;
      case "Table":
        // Tables stay raw text in v1: nothing in them is decorated.
        this.lines(node.from, node.to, "nb-table");
        return false;
      case NodeName.Frontmatter:
        this.rawBlocks.push({ from: node.from, to: node.to });
        if (this.touched(node.from, node.to)) this.lines(node.from, node.to, "nb-frontmatter");
        else {
          const first = this.#state.doc.lineAt(node.from);
          const last = this.#state.doc.lineAt(node.to);
          const yaml = this.#state.sliceDoc(first.to + 1, last.from - 1);
          this.widget(node.from, node.to, { kind: "properties", yaml });
        }
        return false;
      default:
        return true;
    }
  }

  /**
   * A heading: a class on its lines. Off its lines its markers are hidden: the
   * opening #s with the white space after them, a closing run of #s with the
   * white space before it, the underline of a setext heading.
   */
  heading(node: SyntaxNode, level: number): boolean {
    const cls = `nb-h${level}`;
    const marks = children(node, "HeaderMark");
    const setext = node.name.startsWith("Setext");
    const { doc } = this.#state;
    if (setext && marks.length > 0) this.lines(node.from, Math.max(node.from, marks[0].from - 1), cls);
    else this.line(node.from, cls);
    if (this.linesTouched(node.from, node.to)) return true;
    const text = doc.sliceString(node.from, node.to);
    for (const mark of marks) {
      if (setext) {
        this.hide(mark.from, mark.to);
      } else if (mark.from === node.from) {
        let end = mark.to;
        while (end < node.to && /[ \t]/.test(text[end - node.from])) end++;
        this.hide(mark.from, end);
      } else {
        let start = mark.from;
        while (start > node.from && /[ \t]/.test(text[start - 1 - node.from])) start--;
        this.hide(start, mark.to);
      }
    }
    return true;
  }

  /** Emphasis, strong, strikethrough, highlight and code: a class on the text between the marks, which hide when untouched. */
  wrapped(node: SyntaxNode, markName: string, cls: string): boolean {
    const marks = children(node, markName);
    const open = marks[0];
    const close = marks[marks.length - 1];
    if (marks.length < 2 || open.from !== node.from || close.to !== node.to) return true;
    this.mark(open.to, close.from, cls);
    if (!this.touched(node.from, node.to)) {
      this.hide(open.from, open.to);
      this.hide(close.from, close.to);
    }
    return node.name !== "InlineCode";
  }

  /**
   * A link: a class on its text, and the rest ("[", and "](url)" or "][ref]")
   * hidden when untouched. A link without text keeps its markup in sight.
   */
  link(node: SyntaxNode): boolean {
    const marks = children(node, "LinkMark");
    if (marks.length < 2 || marks[0].from !== node.from) return true;
    const from = marks[0].to;
    const to = marks[1].from;
    if (from >= to) return false;
    this.mark(from, to, "nb-link");
    if (!this.touched(node.from, node.to)) {
      this.hide(node.from, from);
      this.hide(to, node.to);
    }
    return true;
  }

  /** <url>: the URL as a URL, its brackets hidden when untouched. */
  autolink(node: SyntaxNode): boolean {
    for (const url of children(node, "URL")) this.mark(url.from, url.to, "nb-url");
    if (!this.touched(node.from, node.to)) for (const mark of children(node, "LinkMark")) this.hide(mark.from, mark.to);
    return false;
  }

  image(node: SyntaxNode): void {
    const url = children(node, "URL")[0];
    const target = url && this.#state.sliceDoc(url.from, url.to);
    const path = target && this.imageAsset(target);
    if (path && !this.touched(node.from, node.to)) {
      const open = children(node, "LinkMark")[0];
      const alt = open ? this.#state.sliceDoc(open.to, children(node, "LinkMark")[1]?.from ?? open.to) : "";
      this.widget(node.from, node.to, { kind: "image", src: this.#context.assetUrl(path), alt });
    } else this.mark(node.from, node.to, "nb-link");
  }

  imageAsset(target: string): string | null {
    if (!/^[^:#?]+\.(?:png|jpe?g|gif|webp|avif|bmp|svg)$/i.test(target) || target.startsWith("//")) return null;
    const path = resolveAsset(this.#context.assets, this.#context.path, target);
    if (path === null) return null;
    const src = this.#context.assetUrl(path);
    return /^(?:\/|\.\/|\.\.\/|https?:\/\/)/i.test(src) ? path : null;
  }

  /**
   * [[link]] and ![[embed]]: untouched, a widget with the label; touched, the
   * raw text with the class. A link inside the note itself resolves (to this
   * note); an embed resolves to a note or an image.
   */
  wikilink(node: SyntaxNode): void {
    const link = parseWikilink(this.#state.sliceDoc(node.from, node.to));
    if (link === null) return;
    if (link.embed) {
      const path = this.imageAsset(link.target);
      if (path && !this.touched(node.from, node.to)) this.widget(node.from, node.to, { kind: "image", src: this.#context.assetUrl(path), alt: link.target });
      else this.mark(node.from, node.to, "nb-link");
      return;
    }
    const resolved = this.resolves(link);
    if (this.touched(node.from, node.to)) {
      this.mark(node.from, node.to, resolved ? "nb-wikilink" : "nb-wikilink nb-wikilink-unresolved");
    } else {
      this.widget(node.from, node.to, { kind: "wikilink", label: wikilinkLabel(link), resolved });
    }
  }

  resolves(link: Wikilink): boolean {
    if (link.target === "") return true;
    const key = `${link.embed ? "!" : ""}${link.target}`;
    let resolved = this.#resolved.get(key);
    if (resolved === undefined) {
      const { notes, assets, path } = this.#context;
      resolved =
        resolveNote(notes, path, link.target) !== null || (link.embed && resolveAsset(assets, path, link.target) !== null);
      this.#resolved.set(key, resolved);
    }
    return resolved;
  }

  /** The ">" of a quote, with one space after it, hidden on a line without the selection. */
  quoteMark(node: SyntaxNode): void {
    if (this.linesTouched(node.from, node.to)) return;
    const after = this.#state.sliceDoc(node.to, node.to + 1);
    const line = this.#state.doc.lineAt(node.from);
    this.hide(node.from, after === " " || after === "\t" ? Math.min(node.to + 1, line.to) : node.to);
  }

  /** The line of a list item's marker: a class, with the columns before the item's text as its indent. */
  listMark(node: SyntaxNode): void {
    if (node.parent?.name !== "ListItem") return;
    const line = this.#state.doc.lineAt(node.from);
    const rest = line.text.slice(node.to - line.from);
    const space = /^[ \t]*/.exec(rest)?.[0] ?? "";
    const taskMarker = node.parent.getChild("Task")?.getChild("TaskMarker");
    if (taskMarker) {
      const taskRest = line.text.slice(taskMarker.to - line.from);
      const taskSpace = /^[ \t]*/.exec(taskRest)?.[0] ?? "";
      this.line(line.from, "nb-list", columns(line.text.slice(0, taskMarker.to - line.from) + taskSpace));
      return;
    }
    this.line(line.from, "nb-list", columns(line.text.slice(0, node.to - line.from) + space));
    const mark = this.#state.sliceDoc(node.from, node.to);
    if (/^[-*+]$/.test(mark)) {
      if (!this.touched(node.from, node.to)) this.widget(node.from, node.to, { kind: "bullet" });
    } else this.mark(node.from, node.to, "nb-list-number");
  }

  task(node: SyntaxNode): void {
    const marker = children(node, "TaskMarker")[0];
    const listMark = node.parent?.getChild("ListMark");
    if (!marker || !listMark) return;
    const checked = /\[[xX]\]/.test(this.#state.sliceDoc(marker.from, marker.to));
    if (checked) this.mark(marker.to + 1, Math.min(node.to, this.#state.doc.lineAt(marker.from).to), "nb-task-done");
    if (!this.touched(listMark.from, marker.to)) this.widget(listMark.from, marker.to, { kind: "task", checked });
  }

  /**
   * The lines of Obsidian comments, outside code and frontmatter: a line whose
   * text starts and ends with "%%" ("%% note %%"), and the lines from one that
   * starts with "%%" to the next that ends with it. A comment that never
   * closes is none. These lines get their class and lose every other
   * decoration that starts on them.
   */
  comments(): void {
    const { doc } = this.#state;
    const commented = new Set<number>();
    let open: number[] | null = null;
    let block = 0;
    for (let number = 1; number <= doc.lines; number++) {
      const line = doc.line(number);
      while (block < this.rawBlocks.length && this.rawBlocks[block].to < line.from) block++;
      if (block < this.rawBlocks.length && this.rawBlocks[block].from <= line.to) {
        open = null;
        continue;
      }
      const text = line.text.trim();
      if (open !== null) {
        open.push(line.from);
        if (text.endsWith(COMMENT)) {
          for (const from of open) commented.add(from);
          open = null;
        }
      } else if (text.startsWith(COMMENT)) {
        if (text.length >= 2 * COMMENT.length && text.endsWith(COMMENT)) commented.add(line.from);
        else open = [line.from];
      }
    }
    if (commented.size === 0) return;
    let kept = 0;
    for (const spec of this.specs) {
      if (!commented.has(doc.lineAt(spec.from).from)) this.specs[kept++] = spec;
    }
    this.specs.length = kept;
    for (const from of commented) this.specs.push({ type: "line", from, class: "nb-comment-line" });
  }
}

/**
 * The decorations of a state, in document order. Pure: it reads the state and
 * its syntax tree, parsed to the end if that takes no more than 100 ms (else
 * as far as it got), and changes nothing. A document over
 * MAX_DECORATED_LENGTH characters has none.
 */
export function collectDecorations(state: EditorState): DecoSpec[] {
  if (state.doc.length > MAX_DECORATED_LENGTH) return [];
  const tree = ensureSyntaxTree(state, state.doc.length, PARSE_TIMEOUT) ?? syntaxTree(state);
  const collector = new Collector(state);
  tree.iterate({ enter: (node) => collector.enter(node.node) });
  collector.comments();
  return collector.specs.sort((a, b) => a.from - b.from);
}

/** CodeMirror decorations for specs. A list item's indent goes to CSS as --nb-list-indent. */
function toRangeSet(specs: readonly DecoSpec[]): DecorationSet {
  const ranges: Range<Decoration>[] = specs.map((spec) => {
    switch (spec.type) {
      case "mark":
        return Decoration.mark({ class: spec.class }).range(spec.from, spec.to);
      case "line":
        return Decoration.line(
          spec.indent === undefined
            ? { class: spec.class }
            : { class: spec.class, attributes: { style: `--nb-list-indent: ${spec.indent}` } },
        ).range(spec.from);
      case "replace":
        return Decoration.replace({}).range(spec.from, spec.to);
      case "widget":
        return Decoration.replace({ widget: new LivePreviewWidget(spec.widget, spec.from, spec.to), block: spec.widget.kind === "properties" }).range(spec.from, spec.to);
    }
  });
  // A set takes its ranges by from, then by startSide (a line before a mark at the same position).
  ranges.sort((a, b) => a.from - b.from || a.value.startSide - b.value.startSide);
  return Decoration.set(ranges);
}

/**
 * The decorations, rebuilt when what they are made of changes: the document,
 * the selection, the syntax tree (a parse that finished in the background),
 * or the configuration (the preview context among it).
 */
const decorations = StateField.define<DecorationSet>({
  create: (state) => toRangeSet(collectDecorations(state)),
  update(value, tr) {
    const stale =
      tr.docChanged ||
      tr.selection !== undefined ||
      tr.reconfigured ||
      syntaxTree(tr.startState) !== syntaxTree(tr.state) ||
      tr.startState.facet(previewContext) !== tr.state.facet(previewContext);
    return stale ? toRangeSet(collectDecorations(tr.state)) : value;
  },
  provide: (field) => EditorView.decorations.from(field),
});

/** Live Preview: the decorations of collectDecorations on the editor, for documents up to MAX_DECORATED_LENGTH. */
export function livePreview(): Extension {
  return [decorations, taskToggleExtension()];
}
