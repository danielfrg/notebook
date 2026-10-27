import type { Extension } from "@codemirror/state";
import { EditorView } from "@codemirror/view";

/*
 * The look of the editor, from theme tokens only: kit-ui's, and the --nb-*
 * ones that app.css defines from them. The tokens change with theme.css and
 * the dark class, so this one theme serves light and dark.
 *
 * The editor fills its parent and scrolls inside, the text in a readable
 * column in the middle (long lines wrap in it with EditorView.lineWrapping).
 * The scrollbar keeps its place, so the column does not move when it comes.
 */
export const editorTheme: Extension = EditorView.theme({
  "&": {
    height: "100%",
    color: "var(--text-primary)",
    backgroundColor: "transparent",
    fontSize: "var(--nb-editor-size)",
  },
  // The one focus treatment, in place of CodeMirror's dotted outline: the
  // app's ring, drawn inside the edge so that a parent that clips keeps it.
  "&.cm-focused": {
    outline: "var(--focus-ring)",
    outlineOffset: "-2px",
  },
  ".cm-scroller": {
    overflow: "auto",
    scrollbarGutter: "stable",
    fontFamily: "var(--nb-editor-font)",
    lineHeight: "var(--nb-editor-line)",
  },
  ".cm-content": {
    maxWidth: "var(--nb-editor-width)",
    margin: "0 auto",
    padding: "var(--space-7) var(--space-6)",
    caretColor: "var(--nb-accent)",
  },
  ".cm-cursor, .cm-dropCursor": {
    borderLeftColor: "var(--nb-accent)",
  },
  // The handles of a selection on iOS.
  ".cm-selectionHandle": {
    backgroundColor: "var(--nb-accent)",
  },
  // As specific as CodeMirror's own rule for a focused editor, so as to win over it.
  "&.cm-focused > .cm-scroller > .cm-selectionLayer .cm-selectionBackground, .cm-selectionBackground, .cm-content ::selection":
    {
      background: "var(--nb-selection)",
    },
});
