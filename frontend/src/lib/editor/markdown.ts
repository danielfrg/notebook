import { markdown } from "@codemirror/lang-markdown";
import { Language, LanguageSupport } from "@codemirror/language";
import { Parser, TreeFragment, type Input, type PartialParse } from "@lezer/common";
import { Tag, tags } from "@lezer/highlight";
import { GFM, type BlockParser, type InlineParser, type MarkdownConfig, type MarkdownParser } from "@lezer/markdown";
import { parseWikilink } from "../wikilinks.ts";

/*
 * The Markdown of the editor: CommonMark and GFM as CodeMirror parses them,
 * plus the Obsidian syntax of notes as nodes of their own. The tree is only
 * read, by Live Preview to decorate and by Source mode to color: the document
 * stays the text of the file.
 */

/** The names of the nodes for Obsidian syntax. */
export const NodeName = {
  Frontmatter: "Frontmatter",
  FrontmatterMark: "FrontmatterMark",
  Wikilink: "Wikilink",
  Embed: "WikilinkEmbed",
  Tag: "Tag",
  Highlight: "Highlight",
  HighlightMark: "HighlightMark",
} as const;

/**
 * The highlight tags of those nodes. Each but Highlight extends the standard
 * tag closest to it, so a highlighter that styles only standard tags styles
 * them too. The marks have tags.processingInstruction, as all Markdown marks do.
 */
export const NodeTag = {
  Frontmatter: Tag.define("Frontmatter", tags.meta),
  Wikilink: Tag.define("Wikilink", tags.link),
  Embed: Tag.define("WikilinkEmbed", tags.link),
  Tag: Tag.define("Tag", tags.labelName),
  Highlight: Tag.define("Highlight"),
} as const;

const code = (char: string): number => char.charCodeAt(0);
const BANG = code("!");
const HASH = code("#");
const EQUALS = code("=");
const OPEN = code("[");
const CLOSE = code("]");
const NEWLINE = code("\n");
const RETURN = code("\r");
const BOM = "\uFEFF";

// The fences of frontmatter. The first line of the text opens it: "---", after
// a byte order mark if there is one. A later line closes it: "---" or "...".
// Either may end in white space, and in a CR when a CRLF text is parsed as a
// string (an editor state has none in its lines).
const OPENING_FENCE = /^\uFEFF?---[ \t\r]*$/;
const CLOSING_FENCE = /^(?:---|\.\.\.)[ \t\r]*$/;

/**
 * The frontmatter: from the opening fence to the end of the line of the closing
 * one, with each fence as a mark and the YAML between them left as text, never
 * Markdown. The parser only takes this rule for a text whose fence closes (see
 * NotebookParser), and that cannot be told from the next line alone.
 */
const frontmatter: BlockParser = {
  name: NodeName.Frontmatter,
  before: "LinkReference", // the first rule, so that "---" is no HorizontalRule
  parse(cx, line) {
    if (cx.lineStart !== 0 || !OPENING_FENCE.test(line.text)) return false;
    const from = line.text.startsWith(BOM) ? 1 : 0;
    const marks = [cx.elt(NodeName.FrontmatterMark, from, from + 3)];
    while (cx.nextLine()) {
      if (!CLOSING_FENCE.test(line.text)) continue;
      marks.push(cx.elt(NodeName.FrontmatterMark, cx.lineStart, cx.lineStart + 3));
      const to = cx.lineStart + line.text.length;
      cx.nextLine();
      cx.addElement(cx.elt(NodeName.Frontmatter, from, to, marks));
      return true;
    }
    // Not reached, as the fence closes; if it were, the frontmatter would run
    // to the end, as a code fence that does not close does.
    cx.addElement(cx.elt(NodeName.Frontmatter, from, cx.lineStart, marks));
    return true;
  },
};

/**
 * [[link]] and ![[embed]], the node of an embed starting at its "!": the text
 * from "[[" to the first "]]", with no bracket or line break between, when
 * parseWikilink reads it as one. It goes before Markdown links, which would take
 * the brackets for theirs.
 */
const wikilink: InlineParser = {
  name: NodeName.Wikilink,
  before: "Link",
  parse(cx, next, pos) {
    const open = next === BANG ? pos + 1 : pos;
    if (cx.char(open) !== OPEN || cx.char(open + 1) !== OPEN) return -1;
    for (let i = open + 2; i < cx.end; i++) {
      const char = cx.char(i);
      if (char === CLOSE) {
        const end = i + 2;
        if (cx.char(i + 1) !== CLOSE || parseWikilink(cx.slice(pos, end)) === null) return -1;
        return cx.addElement(cx.elt(open > pos ? NodeName.Embed : NodeName.Wikilink, pos, end));
      }
      if (char === OPEN || char === NEWLINE || char === RETURN) return -1;
    }
    return -1;
  },
};

/**
 * What may come just before the "#" of a tag, besides the start of the text:
 * white space, and also an opening parenthesis, quote or emphasis mark, so
 * that (#tag) and **#tag** are tags. Not a letter or digit (a#b), nor a slash
 * (the fragment of a URL).
 */
const BEFORE_TAG = /[\s("'*]/;
/** The characters of a tag: letters, digits, "_", "-" and "/", which nests one in another. */
const TAG_CHARACTERS = /[\p{L}\p{M}\p{Nd}_\-\/]*/uy;

/**
 * #tag: the "#" and the characters after it but a trailing "/", as the server
 * reads the tags of a note (which takes them after white space only). There
 * is none when the characters start with "/" or are all digits, nor after "#"
 * and a space. Code and URLs are parsed before this gets to them.
 */
const tag: InlineParser = {
  name: NodeName.Tag,
  after: "Emphasis",
  parse(cx, next, pos) {
    if (next !== HASH || (pos > cx.offset && !BEFORE_TAG.test(cx.slice(pos - 1, pos)))) return -1;
    TAG_CHARACTERS.lastIndex = pos + 1 - cx.offset;
    const name = (TAG_CHARACTERS.exec(cx.text)?.[0] ?? "").replace(/\/+$/, "");
    if (name.startsWith("/") || !/\P{Nd}/u.test(name)) return -1;
    return cx.addElement(cx.elt(NodeName.Tag, pos, pos + 1 + name.length));
  },
};

const PUNCTUATION = /[\p{P}\p{S}]/u;
const isSpace = (char: string): boolean => char === "" || /\s/.test(char);
const highlightMark = { resolve: NodeName.Highlight, mark: NodeName.HighlightMark };

/**
 * ==highlight==, whose marks open and close as GFM's "~~" do: not with white
 * space just inside, and punctuation next to them counts as for emphasis.
 * Only a run of exactly two "=" is a mark.
 */
const highlight: InlineParser = {
  name: NodeName.Highlight,
  after: "Emphasis",
  parse(cx, next, pos) {
    if (next !== EQUALS || cx.char(pos + 1) !== EQUALS) return -1;
    if (cx.char(pos - 1) === EQUALS || cx.char(pos + 2) === EQUALS) return -1;
    const before = cx.slice(pos - 1, pos);
    const after = cx.slice(pos + 2, pos + 3);
    const opens = !isSpace(after) && (!PUNCTUATION.test(after) || isSpace(before) || PUNCTUATION.test(before));
    const closes = !isSpace(before) && (!PUNCTUATION.test(before) || isSpace(after) || PUNCTUATION.test(after));
    return cx.addDelimiter(highlightMark, pos, pos + 2, opens, closes);
  },
};

const obsidian: MarkdownConfig = {
  defineNodes: [
    { name: NodeName.Frontmatter, block: true, style: { "Frontmatter/...": NodeTag.Frontmatter } },
    { name: NodeName.FrontmatterMark, style: tags.processingInstruction },
    { name: NodeName.Wikilink, style: NodeTag.Wikilink },
    { name: NodeName.Embed, style: NodeTag.Embed },
    { name: NodeName.Tag, style: NodeTag.Tag },
    { name: NodeName.Highlight, style: { "Highlight/...": NodeTag.Highlight } },
    { name: NodeName.HighlightMark, style: tags.processingInstruction },
  ],
  parseBlock: [frontmatter],
  parseInline: [wikilink, tag, highlight],
};

/** The lines of input before `to`, without their line breaks, read a block at a time. */
function* lines(input: Input, to: number): Generator<string> {
  let line = "";
  for (let pos = 0; pos < to; pos += 4096) {
    const block = input.read(pos, Math.min(to, pos + 4096));
    let start = 0;
    for (let lineBreak = block.indexOf("\n"); lineBreak >= 0; lineBreak = block.indexOf("\n", start)) {
      yield line + block.slice(start, lineBreak);
      line = "";
      start = lineBreak + 1;
    }
    line += block.slice(start);
  }
  yield line;
}

/**
 * Whether input, read up to `to`, starts with frontmatter: null when its first
 * line is no opening fence, else where that line ends and whether a later line
 * closes it. Only a text that starts with "---" is read past its first characters.
 */
function readFrontmatter(input: Input, to: number): { firstLineEnd: number; closed: boolean } | null {
  if (!/^\uFEFF?---/.test(input.read(0, Math.min(to, 4)))) return null;
  let firstLineEnd = -1;
  for (const line of lines(input, to)) {
    if (firstLineEnd < 0) {
      if (!OPENING_FENCE.test(line)) return null;
      firstLineEnd = line.length;
    } else if (CLOSING_FENCE.test(line)) {
      return { firstLineEnd, closed: true };
    }
  }
  return { firstLineEnd, closed: false };
}

/**
 * The parser of the language, which is two Markdown parsers, one with the
 * frontmatter rule. Whether "---" on the first line opens frontmatter depends
 * on a closing fence any number of lines below, which a block rule cannot look
 * ahead to, so each parse reads the text first and takes the rule only when
 * the fence closes. A text that starts with "---" also parses its first line
 * anew every time: incremental parsing would keep the old reading of that line
 * after an edit far below had added or removed the closing fence.
 */
class NotebookParser extends Parser {
  #withFrontmatter: MarkdownParser;
  #withoutFrontmatter: MarkdownParser;

  constructor(withFrontmatter: MarkdownParser, withoutFrontmatter: MarkdownParser) {
    super();
    this.#withFrontmatter = withFrontmatter;
    this.#withoutFrontmatter = withoutFrontmatter;
  }

  createParse(
    input: Input,
    fragments: readonly TreeFragment[],
    ranges: readonly { from: number; to: number }[],
  ): PartialParse {
    // Only a whole text has frontmatter, not one range of it parsed apart.
    const found = ranges.length === 1 && ranges[0].from === 0 ? readFrontmatter(input, ranges[0].to) : null;
    if (found === null) return this.#withoutFrontmatter.createParse(input, fragments, ranges);
    // The old tree may be reused from the end of the first line on. A fragment
    // keeps its openEnd, which says whether a change follows it.
    const end = found.firstLineEnd;
    const reusable = fragments.flatMap((fragment) =>
      fragment.from >= end
        ? [fragment]
        : fragment.to > end
          ? [new TreeFragment(end, fragment.to, fragment.tree, fragment.offset, true, fragment.openEnd)]
          : [],
    );
    const parser = found.closed ? this.#withFrontmatter : this.#withoutFrontmatter;
    return parser.createParse(input, reusable, ranges);
  }
}

/** Markdown for notes: CodeMirror's Markdown support with GFM and Obsidian's syntax. */
export function notebookMarkdown(): LanguageSupport {
  const { language, support } = markdown({ extensions: [GFM, obsidian] });
  const withFrontmatter = language.parser as MarkdownParser;
  const parser = new NotebookParser(withFrontmatter, withFrontmatter.configure({ remove: [frontmatter.name] }));
  return new LanguageSupport(new Language(language.data, parser, [], language.name), support);
}
