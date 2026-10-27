import { HighlightStyle, syntaxHighlighting } from "@codemirror/language";
import type { Extension } from "@codemirror/state";
import { tags } from "@lezer/highlight";
import { NodeTag } from "./markdown.ts";

/*
 * The highlighting of Source mode: the Markdown stays text, and its syntax is
 * picked out with theme tokens only, so that it follows theme.css and the dark
 * class. Text with several tags gets the styles of all of them, and where they
 * disagree the later rule wins: the marks (#, *, `, >, list markers, fences)
 * come last, muted whatever they mark.
 */
const sourceStyle = HighlightStyle.define([
  { tag: tags.heading, color: "var(--text-primary)", fontWeight: "var(--font-weight-bold)" },
  { tag: tags.quote, color: "var(--text-secondary)" },
  { tag: tags.emphasis, fontStyle: "italic" },
  { tag: tags.strong, fontWeight: "var(--font-weight-bold)" },
  { tag: tags.strikethrough, color: "var(--text-muted)", textDecoration: "line-through" },
  { tag: [tags.link, tags.url], color: "var(--nb-accent)" },
  { tag: tags.monospace, color: "var(--accent-teal)", fontFamily: "var(--nb-editor-mono)" },
  { tag: NodeTag.Frontmatter, color: "var(--text-muted)", fontFamily: "var(--nb-editor-mono)" },
  { tag: [NodeTag.Wikilink, NodeTag.Embed], color: "var(--nb-accent)" },
  { tag: NodeTag.Tag, color: "var(--accent-purple)" },
  { tag: NodeTag.Highlight, backgroundColor: "var(--nb-highlight)" },
  // The [ ] and [x] of a task.
  { tag: tags.atom, color: "var(--nb-accent)" },
  { tag: [tags.processingInstruction, tags.contentSeparator], color: "var(--text-muted)" },
]);

/**
 * Syntax highlighting for Source mode: headings, emphasis, strong,
 * strikethrough, code, links and URLs, quotes, marks (list markers among
 * them), task markers, frontmatter, wikilinks, embeds, tags and highlights.
 */
export const sourceHighlight: Extension = syntaxHighlighting(sourceStyle);
