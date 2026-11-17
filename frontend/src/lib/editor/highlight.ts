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
  { tag: tags.heading, color: "var(--ink)", fontWeight: "700" },
  { tag: tags.quote, color: "var(--nb-ink-2)" },
  { tag: tags.emphasis, fontStyle: "italic" },
  { tag: tags.strong, fontWeight: "700" },
  { tag: tags.strikethrough, color: "var(--muted)", textDecoration: "line-through" },
  { tag: [tags.link, tags.url], color: "var(--nb-accent)" },
  { tag: tags.monospace, color: "var(--accent)", fontFamily: "var(--nb-editor-mono)" },
  { tag: NodeTag.Frontmatter, color: "var(--muted)", fontFamily: "var(--nb-editor-mono)" },
  { tag: [NodeTag.Wikilink, NodeTag.Embed], color: "var(--nb-accent)" },
  { tag: NodeTag.Tag, color: "var(--nb-accent)" },
  { tag: NodeTag.Highlight, backgroundColor: "var(--nb-highlight)" },
  // The [ ] and [x] of a task.
  { tag: tags.atom, color: "var(--nb-accent)" },
  { tag: [tags.processingInstruction, tags.contentSeparator], color: "var(--muted)" },
]);

/**
 * Syntax highlighting for Source mode: headings, emphasis, strong,
 * strikethrough, code, links and URLs, quotes, marks (list markers among
 * them), task markers, frontmatter, wikilinks, embeds, tags and highlights.
 */
export const sourceHighlight: Extension = syntaxHighlighting(sourceStyle);
