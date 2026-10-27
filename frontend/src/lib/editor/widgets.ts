import { syntaxTree } from "@codemirror/language";
import type { EditorState, Extension, TransactionSpec } from "@codemirror/state";
import { EditorView, WidgetType } from "@codemirror/view";
import { parseProperties } from "../properties.ts";
import type { WidgetKind } from "./livePreview.ts";
import { NodeName } from "./markdown.ts";

/** A task marker's one mutable character, or null outside a parsed task. */
export function toggleTaskAt(state: EditorState, pos: number): TransactionSpec | null {
  if (state.readOnly) return null;
  const marker = syntaxTree(state).resolveInner(pos, 1);
  const taskMarker = marker.name === "TaskMarker" ? marker : marker.parent?.name === "TaskMarker" ? marker.parent : null;
  if (!taskMarker || pos < taskMarker.from || pos > taskMarker.to) return null;
  const char = state.sliceDoc(taskMarker.from + 1, taskMarker.from + 2);
  if (char !== " " && char !== "x" && char !== "X") return null;
  return { changes: { from: taskMarker.from + 1, to: taskMarker.from + 2, insert: char === " " ? "x" : " " }, userEvent: "input.toggle" };
}

/** Activate source-backed widgets before CodeMirror can move the selection into their replacements. */
export function taskToggleExtension(): Extension {
  const activate = (target: EventTarget | null, view: EditorView, modifier = false): boolean => {
    if (!(target instanceof Element)) return false;
    const checkbox = target.closest<HTMLElement>(".nb-task-checkbox");
    if (checkbox && view.dom.contains(checkbox)) {
      const pos = view.posAtDOM(checkbox);
      const listItem = syntaxTree(view.state).resolveInner(pos, 1).parent;
      const marker = listItem?.name === "ListItem" ? listItem.getChild("Task")?.getChild("TaskMarker") : null;
      const change = marker && toggleTaskAt(view.state, marker.from + 1);
      if (change) view.dispatch(change);
      return true;
    }
    const properties = target.closest<HTMLElement>(".nb-properties");
    if (properties && view.dom.contains(properties)) {
      const node = syntaxTree(view.state).resolveInner(view.posAtDOM(properties), 1);
      if (node.name === NodeName.Frontmatter) {
        view.dispatch({ selection: { anchor: Math.min(node.to, node.from + 4) }, scrollIntoView: true });
        view.focus();
      }
      return true;
    }
    const link = target.closest<HTMLAnchorElement>("a.nb-wikilink");
    if (!link || !view.dom.contains(link)) return false;
    if (!modifier) return false;
    const node = syntaxTree(view.state).resolveInner(view.posAtDOM(link), 1);
    if (node.name !== NodeName.Wikilink) return true;
    view.dom.dispatchEvent(new CustomEvent("notebook:wikilink", {
      bubbles: true,
      detail: { source: view.state.sliceDoc(node.from, node.to), from: node.from, to: node.to, resolved: !link.classList.contains("nb-wikilink-unresolved") },
    }));
    return true;
  };
  return EditorView.domEventHandlers({
    mousedown(event, view) {
      if (event.button !== 0) return false;
      if ((event.metaKey || event.ctrlKey) && (event.target as Element).closest?.("a.nb-wikilink")) event.preventDefault();
      return activate(event.target, view, event.metaKey || event.ctrlKey);
    },
    click(event, view) {
      const target = event.target;
      if (!(target instanceof Element) || !target.closest(".nb-task-checkbox, a.nb-wikilink")) return false;
      if (target.closest("a.nb-wikilink")) event.preventDefault();
      // Pointer activation already ran on mousedown. Keyboard-generated clicks have detail 0.
      if (event.detail === 0) activate(target, view, event.metaKey || event.ctrlKey);
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
    return this.from === other.from && this.to === other.to && JSON.stringify(this.kind) === JSON.stringify(other.kind);
  }

  ignoreEvent(): boolean {
    return this.kind.kind !== "task" && this.kind.kind !== "wikilink" && this.kind.kind !== "properties";
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
        checkbox.setAttribute("aria-label", this.kind.checked ? "Mark task incomplete" : "Mark task complete");
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
        return link;
      }
      case "properties": {
        const panel = document.createElement("div");
        panel.className = "nb-properties";
        panel.setAttribute("role", "button");
        panel.tabIndex = 0;
        panel.setAttribute("aria-label", "Edit properties");
        for (const property of parseProperties(this.kind.yaml)) {
          const row = document.createElement("div");
          row.className = "nb-property";
          const key = document.createElement("span");
          key.className = "nb-property-key";
          key.textContent = property.key;
          row.append(key);
          for (const value of property.values) {
            const item = document.createElement("span");
            item.className = property.list ? "nb-property-chip" : "nb-property-value";
            item.textContent = value;
            row.append(item);
          }
          panel.append(row);
        }
        const enter = () => {
          view.dispatch({ selection: { anchor: Math.min(this.to, this.from + 4) }, scrollIntoView: true });
          view.focus();
        };
        panel.addEventListener("click", enter);
        panel.addEventListener("keydown", (event) => {
          if (event.key === "Enter" || event.key === " ") { event.preventDefault(); enter(); }
        });
        return panel;
      }
    }
  }
}
