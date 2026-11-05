<script lang="ts">
  /*
   * The note editor: CodeMirror 6 over the text of a note. The document is the
   * text of the file, its line separator and byte order mark included, so
   * saving writes back what was loaded plus what was typed and nothing else.
   * Live Preview comes in through `extensions`; Source mode highlights the
   * syntax instead.
   *
   * `value` is the text of the note. When it becomes something else than the
   * document (another note, or this one reloaded from disk), the document is
   * replaced: only what differs changes, the selection stays where its text is,
   * the history starts anew, and `onchange` is not called. `onchange` reports
   * edits only (typing, commands, undo), with the text as getValue() gives it.
   * The host may pass that text back as `value` at once, which changes nothing,
   * but not an older text later on: that would replace what was typed since.
   * A note with mixed line breaks opens read-only (`onlineseparator` says so).
   */
  import { Compartment, EditorState, type Extension } from "@codemirror/state";
  import { autocompletion } from "@codemirror/autocomplete";
  import { syntaxTree } from "@codemirror/language";
  import { drawSelection, dropCursor, EditorView, keymap } from "@codemirror/view";
  import { onMount, untrack } from "svelte";
  import "../../editor.css";
  import { editorCommands, editorKeymap, type EditorCommand } from "./commands.ts";
  import {
    changedByUser,
    documentExtensions,
    readOnlyExtension,
    replaceDocument,
    toDocument,
    type LineSeparator,
  } from "./document.ts";
  import { sourceHighlight } from "./highlight.ts";
  import { detectLineSeparator } from "./linesep.ts";
  import { previewSelectionActive } from "./livePreview.ts";
  import { notebookMarkdown } from "./markdown.ts";
  import { editorTheme } from "./theme.ts";
  import { NodeName } from "./markdown.ts";
  import { wikilinkSource } from "./wikilink-complete.ts";

  type Props = {
    value: string;
    mode: "live" | "source";
    readOnly?: boolean;
    /** The Live Preview extensions, on in live mode. */
    extensions?: Extension[];
    ariaLabel?: string;
    onchange: (value: string) => void;
    onsave: () => void;
    notes?: readonly string[];
    onwikilink?: (source: string) => void;
    /** The line separator of every document loaded; "mixed" is read-only, and the host says why. */
    onlineseparator?: (separator: LineSeparator) => void;
    /** Whether the text is scrolled away from its top, each time that changes. */
    onscrolled?: (scrolled: boolean) => void;
  };

  let {
    value,
    mode,
    readOnly = false,
    extensions = [],
    ariaLabel = "Note",
    onchange,
    onsave,
    notes = [],
    onwikilink,
    onlineseparator,
    onscrolled,
  }: Props = $props();

  let host: HTMLDivElement;
  let view: EditorView | undefined;
  /** The line separator of the document, and the text it was loaded from. */
  let separator: LineSeparator = "\n";
  let loaded = "";

  const modeCompartment = new Compartment();
  const readOnlyCompartment = new Compartment();
  const labelCompartment = new Compartment();
  const previewFocusCompartment = new Compartment();
  const compartments = { separator: new Compartment(), history: new Compartment() };

  const modeExtension = (): Extension => (mode === "live" ? extensions : sourceHighlight);
  const labelExtension = (label: string): Extension =>
    EditorView.contentAttributes.of({ "aria-label": label });

  onMount(() => {
    separator = detectLineSeparator(value);
    loaded = value;
    const editor = new EditorView({
      parent: host,
      state: EditorState.create({
        doc: toDocument(value, separator),
        extensions: [
          notebookMarkdown(),
          autocompletion({ override: [wikilinkSource(() => notes)] }),
          documentExtensions(separator, compartments),
          drawSelection(),
          // Where dragged text would land.
          dropCursor(),
          keymap.of(editorKeymap(() => onsave())),
          EditorView.lineWrapping,
          editorTheme,
          modeCompartment.of(modeExtension()),
          previewFocusCompartment.of(previewSelectionActive.of(false)),
          EditorView.focusChangeEffect.of((_state, focusing) =>
            previewFocusCompartment.reconfigure(previewSelectionActive.of(focusing)),
          ),
          readOnlyCompartment.of(readOnlyExtension(readOnly)),
          labelCompartment.of(labelExtension(ariaLabel)),
          EditorView.updateListener.of((update) => {
            if (changedByUser(update.transactions)) onchange(update.state.sliceDoc(0));
          }),
        ],
      }),
    });
    editor.scrollDOM.addEventListener("mousedown", clickInMargin);
    editor.scrollDOM.addEventListener("scroll", reportScroll, { passive: true });
    editor.dom.addEventListener("notebook:wikilink", widgetWikilink as EventListener);
    editor.contentDOM.addEventListener("mousedown", modifierClick);
    view = editor;
    onlineseparator?.(separator);
    return () => {
      editor.dom.removeEventListener("notebook:wikilink", widgetWikilink as EventListener);
      editor.contentDOM.removeEventListener("mousedown", modifierClick);
      editor.scrollDOM.removeEventListener("scroll", reportScroll);
      onscrolled?.(false);
      view = undefined;
      editor.destroy();
    };
  });

  /** Puts content in a compartment, unless it is there already. */
  function reconfigure(compartment: Compartment, content: Extension): void {
    if (view && compartment.get(view.state) !== content)
      view.dispatch({ effects: compartment.reconfigure(content) });
  }

  $effect(() => {
    const content = modeExtension();
    untrack(() => reconfigure(modeCompartment, content));
  });

  $effect(() => {
    const content = readOnlyExtension(readOnly);
    untrack(() => reconfigure(readOnlyCompartment, content));
  });

  $effect(() => {
    const label = ariaLabel;
    untrack(() => {
      if (view && view.contentDOM.getAttribute("aria-label") !== label) {
        view.dispatch({ effects: labelCompartment.reconfigure(labelExtension(label)) });
      }
    });
  });

  $effect(() => {
    const text = value;
    untrack(() => {
      if (view && text !== getValue()) load(view, text);
    });
  });

  let scrolled = false;
  /** Tells the host whether the text is scrolled away from its top, when that changes. */
  function reportScroll(): void {
    const now = (view?.scrollDOM.scrollTop ?? 0) > 0;
    if (now === scrolled) return;
    scrolled = now;
    onscrolled?.(now);
  }

  /** Makes text the document, as another version of the note, with its own line separator. */
  function load(editor: EditorView, text: string): void {
    const next = detectLineSeparator(text);
    editor.dispatch(replaceDocument(editor.state, text, next, compartments));
    separator = next;
    loaded = text;
    onlineseparator?.(next);
  }

  /**
   * A press in the margins beside the column lands on the scroller, which
   * CodeMirror does not listen to, and would only take the focus away. It goes
   * to the text at the same height instead: the cursor goes to the nearest
   * point of that line, and a drag selects from there. A press on the
   * scrollbar stays the scrollbar's.
   */
  function clickInMargin(event: MouseEvent): void {
    const scroller = view?.scrollDOM;
    if (!view || event.target !== scroller || event.button !== 0) return;
    if (event.offsetX >= scroller.clientWidth || event.offsetY >= scroller.clientHeight) return;
    event.preventDefault();
    view.contentDOM.dispatchEvent(new MouseEvent("mousedown", event));
  }

  function widgetWikilink(event: CustomEvent<{ source: string }>): void {
    onwikilink?.(event.detail.source);
  }

  function modifierClick(event: MouseEvent): void {
    if (!view || event.button !== 0 || !(event.metaKey || event.ctrlKey)) return;
    if ((event.target as Element).closest("a.nb-wikilink")) return;
    const position = view.posAtCoords({ x: event.clientX, y: event.clientY });
    if (position === null) return;
    let node = syntaxTree(view.state).resolveInner(position, -1);
    while (
      node &&
      node.name !== NodeName.Wikilink &&
      node.name !== "Link" &&
      node.name !== "Autolink" &&
      node.name !== "URL"
    )
      node = node.parent!;
    if (!node) return;
    if (node.name === NodeName.Wikilink) {
      event.preventDefault();
      onwikilink?.(view.state.sliceDoc(node.from, node.to));
      return;
    }
    const url = node.name === "URL" ? node : node.getChild("URL");
    if (!url) return;
    const target = view.state.sliceDoc(url.from, url.to);
    if (!/^https?:\/\//i.test(target)) return;
    event.preventDefault();
    window.open(target, "_blank", "noopener,noreferrer");
  }

  /** Puts the keyboard focus in the editor. */
  export function focus(): void {
    view?.focus();
  }

  export function revealLine(number: number): void {
    if (!view) return;
    const line = view.state.doc.line(Math.max(1, Math.min(number, view.state.doc.lines)));
    view.dispatch({ selection: { anchor: line.from }, scrollIntoView: true });
    view.focus();
  }

  /** Runs a command as its key or a toolbar button would, and gives the editor the focus back. */
  export function run(command: EditorCommand): void {
    if (!view) return;
    editorCommands[command](view);
    view.focus();
  }

  /**
   * The text of the document with its own line breaks: sliceDoc(0), which
   * joins the lines of a CRLF note with CRLF. A note with mixed line breaks,
   * which a state cannot keep and which cannot be edited, gives back the text
   * it was loaded from.
   */
  export function getValue(): string {
    if (!view) return value;
    return separator === "mixed" ? loaded : view.state.sliceDoc(0);
  }
</script>

<div class="nb-editor" bind:this={host}></div>
