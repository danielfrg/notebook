import { syntaxTree } from "@codemirror/language";
import type { EditorState, Extension, TransactionSpec } from "@codemirror/state";
import { EditorView, WidgetType } from "@codemirror/view";
import {
  addItem,
  addProperty,
  locateProperties,
  removeItem,
  removeProperty,
  renameKey,
  setValue,
  toggleValue,
  validKey,
  type EditableProperty,
  type Line,
  type PropertyEdit,
} from "../property-edit.ts";
import type { WidgetKind } from "./livePreview.ts";
import { NodeName } from "./markdown.ts";

/** A task marker's one mutable character, or null outside a parsed task. */
export function toggleTaskAt(state: EditorState, pos: number): TransactionSpec | null {
  if (state.readOnly) return null;
  const marker = syntaxTree(state).resolveInner(pos, 1);
  const taskMarker =
    marker.name === "TaskMarker"
      ? marker
      : marker.parent?.name === "TaskMarker"
        ? marker.parent
        : null;
  if (!taskMarker || pos < taskMarker.from || pos > taskMarker.to) return null;
  const char = state.sliceDoc(taskMarker.from + 1, taskMarker.from + 2);
  if (char !== " " && char !== "x" && char !== "X") return null;
  return {
    changes: {
      from: taskMarker.from + 1,
      to: taskMarker.from + 2,
      insert: char === " " ? "x" : " ",
    },
    userEvent: "input.toggle",
  };
}

/** Activate source-backed widgets before CodeMirror can move the selection into their replacements. */
export function taskToggleExtension(): Extension {
  const activate = (target: EventTarget | null, view: EditorView, modifier = false): boolean => {
    if (!(target instanceof Element)) return false;
    const checkbox = target.closest<HTMLElement>(".nb-task-checkbox");
    if (checkbox && view.dom.contains(checkbox)) {
      const pos = view.posAtDOM(checkbox);
      const listItem = syntaxTree(view.state).resolveInner(pos, 1).parent;
      const marker =
        listItem?.name === "ListItem" ? listItem.getChild("Task")?.getChild("TaskMarker") : null;
      const change = marker && toggleTaskAt(view.state, marker.from + 1);
      if (change) view.dispatch(change);
      return true;
    }
    const link = target.closest<HTMLAnchorElement>("a.nb-wikilink");
    if (!link || !view.dom.contains(link)) return false;
    if (!modifier) return false;
    const node = syntaxTree(view.state).resolveInner(view.posAtDOM(link), 1);
    if (node.name !== NodeName.Wikilink) return true;
    view.dom.dispatchEvent(
      new CustomEvent("notebook:wikilink", {
        bubbles: true,
        detail: {
          source: view.state.sliceDoc(node.from, node.to),
          from: node.from,
          to: node.to,
          resolved: !link.classList.contains("nb-wikilink-unresolved"),
        },
      }),
    );
    return true;
  };
  return EditorView.domEventHandlers({
    mousedown(event, view) {
      if (event.button !== 0) return false;
      if ((event.metaKey || event.ctrlKey) && (event.target as Element).closest?.("a.nb-wikilink"))
        event.preventDefault();
      return activate(event.target, view, event.metaKey || event.ctrlKey);
    },
    click(event, view) {
      const target = event.target;
      if (!(target instanceof Element) || !target.closest(".nb-task-checkbox, a.nb-wikilink"))
        return false;
      if (target.closest("a.nb-wikilink")) event.preventDefault();
      // Pointer activation already ran on mousedown. Keyboard-generated clicks have detail 0.
      if (event.detail === 0) activate(target, view, true);
      return true;
    },
  });
}

/** DOM for the visible, source-backed Live Preview replacements. */
export class LivePreviewWidget extends WidgetType {
  readonly kind: WidgetKind;
  readonly from: number;
  readonly to: number;

  constructor(kind: WidgetKind, from = 0, to = 0) {
    super();
    this.kind = kind;
    this.from = from;
    this.to = to;
  }

  eq(other: LivePreviewWidget): boolean {
    return (
      this.from === other.from &&
      this.to === other.to &&
      JSON.stringify(this.kind) === JSON.stringify(other.kind)
    );
  }

  ignoreEvent(event: Event): boolean {
    // The properties panel handles its own events: a click in it never moves the cursor into the frontmatter.
    if (this.kind.kind === "properties") return true;
    void event;
    return this.kind.kind !== "task" && this.kind.kind !== "wikilink";
  }

  toDOM(view: EditorView): HTMLElement {
    switch (this.kind.kind) {
      case "bullet": {
        const bullet = document.createElement("span");
        bullet.className = "nb-bullet";
        bullet.setAttribute("aria-hidden", "true");
        return bullet;
      }
      case "task": {
        const checkbox = document.createElement("button");
        checkbox.type = "button";
        checkbox.className = "nb-task-checkbox";
        checkbox.setAttribute("role", "checkbox");
        checkbox.setAttribute("aria-checked", String(this.kind.checked));
        checkbox.setAttribute(
          "aria-label",
          this.kind.checked ? "Mark task incomplete" : "Mark task complete",
        );
        return checkbox;
      }
      case "rule": {
        const rule = document.createElement("span");
        rule.className = "nb-rule";
        rule.setAttribute("aria-hidden", "true");
        return rule;
      }
      case "image": {
        const image = document.createElement("img");
        image.className = "nb-image";
        image.src = this.kind.src;
        image.alt = this.kind.alt;
        image.loading = "lazy";
        return image;
      }
      case "wikilink": {
        const link = document.createElement("a");
        link.className = this.kind.resolved ? "nb-wikilink" : "nb-wikilink nb-wikilink-unresolved";
        link.href = "#";
        link.textContent = this.kind.label;
        link.addEventListener("keydown", (event) => {
          if (event.key !== "Enter" && event.key !== " ") return;
          event.preventDefault();
          event.stopPropagation();
          link.click();
        });
        return link;
      }
      case "properties":
        return propertiesPanel(view, this.from, this.to);
    }
  }
}

/** The property to put the focus on after the panel is drawn anew from an edit. */
let focusAfterEdit: { key: string; control: "add" | "value" } | null = null;

/** The lines between the fences of the frontmatter that is shown at a position, or null if it is no longer there. */
function frontmatterAt(
  view: EditorView,
  panel: HTMLElement,
  fallback: { from: number; to: number },
): { lines: Line[]; end: number } | null {
  let from = fallback.from;
  let to = fallback.to;
  try {
    const node = syntaxTree(view.state).resolveInner(view.posAtDOM(panel), 1);
    if (node.name === NodeName.Frontmatter) ({ from, to } = node);
  } catch {
    /* The panel is not in the view any more; the position it was drawn at will do. */
  }
  const { doc } = view.state;
  if (from < 0 || to > doc.length) return null;
  const first = doc.lineAt(from).number;
  const last = doc.lineAt(to).number;
  if (last - first < 1) return null;
  return { lines: linesOf(view, from, to), end: doc.line(last - 1).to };
}

function dispatchEdit(view: EditorView, edit: PropertyEdit): void {
  view.dispatch({
    changes: {
      from: edit.from,
      to: edit.to,
      insert: edit.insert.replaceAll("\n", view.state.lineBreak),
    },
    userEvent: "input.properties",
  });
}

/** Lucide icon paths (24px grid), drawn as static SVG. */
const ICONS = {
  text: '<path d="M4 7V4h16v3"/><path d="M9 20h6"/><path d="M12 4v16"/>',
  list: '<path d="M3 5h.01"/><path d="M3 12h.01"/><path d="M3 19h.01"/><path d="M8 5h13"/><path d="M8 12h13"/><path d="M8 19h13"/>',
  checkbox: '<rect width="18" height="18" x="3" y="3" rx="2"/><path d="m9 12 2 2 4-4"/>',
  date: '<path d="M8 2v4"/><path d="M16 2v4"/><rect width="18" height="18" x="3" y="4" rx="2"/><path d="M3 10h18"/>',
  close: '<path d="M18 6 6 18"/><path d="m6 6 12 12"/>',
  plus: '<path d="M5 12h14"/><path d="M12 5v14"/>',
};

function icon(name: keyof typeof ICONS, className: string): HTMLSpanElement {
  const span = document.createElement("span");
  span.className = className;
  span.setAttribute("aria-hidden", "true");
  span.innerHTML = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">${ICONS[name]}</svg>`;
  return span;
}

/**
 * The Properties panel of Live Preview, after Hubble's: a row for each
 * property the panel can read, with its type, its name and its value as
 * controls, and an × to take it out. Every control edits just its own text in
 * the document (property-edit.ts), so a save writes what was there plus that
 * change; a property the panel cannot read is not shown, and stays as it is.
 * Clicks in the panel never put the cursor in the frontmatter: Source mode
 * shows it as text.
 */
function propertiesPanel(view: EditorView, from: number, to: number): HTMLElement {
  const panel = document.createElement("div");
  panel.className = "nb-properties";
  panel.setAttribute("role", "group");
  panel.setAttribute("aria-label", "Properties");
  const locked = view.state.readOnly;
  const range = { from, to };
  const shown = locateProperties(linesOf(view, from, to));
  const keys = shown.map((property) => property.key);

  /** Runs an edit for the property at an index, if the document still has it there under that key. */
  const edit = (
    index: number,
    key: string,
    make: (property: EditableProperty) => PropertyEdit | null,
  ): boolean => {
    if (view.state.readOnly) return false;
    const found = frontmatterAt(view, panel, range);
    const property = found ? locateProperties(found.lines)[index] : undefined;
    if (!property || property.key !== key) return false;
    const change = make(property);
    if (change) dispatchEdit(view, change);
    return change !== null;
  };

  const control = <K extends keyof HTMLElementTagNameMap>(
    tag: K,
    className: string,
    label: string,
  ): HTMLElementTagNameMap[K] => {
    const element = document.createElement(tag);
    element.className = `nb-prop-control ${className}`;
    element.setAttribute("aria-label", label);
    if (locked && "disabled" in element) (element as unknown as HTMLButtonElement).disabled = true;
    // Keys typed here are the control's, not the editor's.
    element.addEventListener("keydown", (event) => event.stopPropagation());
    return element;
  };

  /** A field that commits on Enter or when it loses the focus, and puts its text back on Escape. */
  const field = (
    className: string,
    label: string,
    value: string,
    commit: (value: string) => void,
    type: "text" | "date" = "text",
  ): HTMLInputElement => {
    const input = control("input", `nb-prop-input ${className}`, label);
    input.type = type;
    input.value = value;
    input.spellcheck = false;
    input.readOnly = locked;
    input.addEventListener("change", () => {
      if (input.value !== value) commit(input.value);
    });
    input.addEventListener("keydown", (event) => {
      if (event.key === "Enter") {
        event.preventDefault();
        input.blur();
      } else if (event.key === "Escape") {
        input.value = value;
        input.blur();
      }
    });
    return input;
  };

  shown.forEach((property, index) => {
    const run = (make: (current: EditableProperty) => PropertyEdit | null) =>
      edit(index, property.key, make);
    const row = document.createElement("div");
    row.className = "nb-property";

    row.append(icon(property.type, "nb-prop-type"));
    const key = field("nb-prop-key", "Property name", property.key, (name) => {
      if (!run((current) => renameKey(current, name, keys))) key.value = property.key;
    });
    key.title = property.key;
    row.append(key);

    const value = document.createElement("div");
    value.className = "nb-property-value";
    if (property.type === "checkbox") {
      const checkbox = control("button", "nb-prop-check", property.key);
      checkbox.type = "button";
      checkbox.setAttribute("role", "checkbox");
      checkbox.setAttribute("aria-checked", String(property.values[0] === "true"));
      checkbox.addEventListener("click", () => run(toggleValue));
      value.append(checkbox);
    } else if (property.type === "list") {
      property.values.forEach((item, at) => {
        const chip = document.createElement("span");
        chip.className = "nb-property-chip";
        chip.append(item);
        if (!locked) {
          const remove = control("button", "nb-prop-remove", `Remove ${item}`);
          remove.type = "button";
          remove.append(icon("close", "nb-prop-icon"));
          remove.addEventListener("click", () => run((current) => removeItem(current, at)));
          chip.append(remove);
        }
        value.append(chip);
      });
      if (!locked) {
        const add = control("input", "nb-prop-input nb-prop-add", `Add to ${property.key}`);
        add.type = "text";
        add.placeholder = property.values.length === 0 ? "Empty" : "";
        add.spellcheck = false;
        const commit = () => {
          const text = add.value.trim();
          if (!text) return;
          focusAfterEdit = { key: property.key, control: "add" };
          run((current) => addItem(current, text));
        };
        add.addEventListener("keydown", (event) => {
          if (event.key === "Enter" || event.key === ",") {
            event.preventDefault();
            commit();
          } else if (event.key === "Backspace" && add.value === "" && property.values.length > 0) {
            focusAfterEdit = { key: property.key, control: "add" };
            run((current) => removeItem(current, current.values.length - 1));
          } else if (event.key === "Escape") {
            add.value = "";
            add.blur();
          }
        });
        add.addEventListener("blur", commit);
        if (focusAfterEdit?.key === property.key && focusAfterEdit.control === "add") {
          focusAfterEdit = null;
          requestAnimationFrame(() => add.focus());
        }
        value.append(add);
      }
    } else {
      const input = field(
        "nb-prop-text",
        property.key,
        property.values[0] ?? "",
        (next) => run((current) => setValue(current, next)),
        property.type === "date" ? "date" : "text",
      );
      input.placeholder = "Empty";
      if (focusAfterEdit?.key === property.key && focusAfterEdit.control === "value") {
        focusAfterEdit = null;
        requestAnimationFrame(() => input.focus());
      }
      value.append(input);
    }
    row.append(value);

    if (!locked) {
      const remove = control("button", "nb-prop-delete", `Delete ${property.key}`);
      remove.type = "button";
      remove.append(icon("close", "nb-prop-icon"));
      remove.addEventListener("click", () => run(removeProperty));
      row.append(remove);
    }
    panel.append(row);
  });

  if (!locked) {
    const add = control("button", "nb-prop-new", "Add property");
    add.type = "button";
    add.append(icon("plus", "nb-prop-type"), "Add property");
    add.addEventListener("click", () => {
      const row = document.createElement("div");
      row.className = "nb-property";
      row.append(icon("text", "nb-prop-type"));
      const input = control("input", "nb-prop-input nb-prop-key", "Property name");
      input.type = "text";
      input.placeholder = "Property";
      input.spellcheck = false;
      let done = false;
      const finish = (commit: boolean) => {
        if (done) return;
        done = true;
        const name = input.value.trim();
        const current = frontmatterAt(view, panel, range);
        if (commit && current && validKey(name, keys)) {
          focusAfterEdit = { key: name, control: "value" };
          dispatchEdit(view, addProperty(current.end, name));
        } else row.replaceWith(add);
      };
      input.addEventListener("keydown", (event) => {
        if (event.key === "Enter") {
          event.preventDefault();
          finish(true);
        } else if (event.key === "Escape") finish(false);
      });
      input.addEventListener("blur", () => finish(input.value.trim() !== ""));
      row.append(input);
      add.replaceWith(row);
      input.focus();
    });
    panel.append(add);
  }
  return panel;
}

/** The lines of a range of the document, between its first and last line. */
function linesOf(view: EditorView, from: number, to: number): Line[] {
  const { doc } = view.state;
  if (from < 0 || to > doc.length) return [];
  const lines: Line[] = [];
  for (let number = doc.lineAt(from).number + 1; number < doc.lineAt(to).number; number++) {
    const line = doc.line(number);
    lines.push({ text: line.text, from: line.from });
  }
  return lines;
}
