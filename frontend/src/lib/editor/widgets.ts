import { syntaxTree } from "@codemirror/language";
import type { EditorState, Extension, TransactionSpec } from "@codemirror/state";
import { EditorView, WidgetType } from "@codemirror/view";
import { parseProperties } from "../properties.ts";
import type { WidgetKind } from "./livePreview.ts";

/** A task marker's one mutable character, or null outside a parsed task. */
export function toggleTaskAt(state: EditorState, pos: number): TransactionSpec | null {
  if (state.readOnly) return null;
  const marker = syntaxTree(state).resolveInner(pos, 1);
  const taskMarker = marker.name === "TaskMarker" ? marker : marker.parent?.name === "TaskMarker" ? marker.parent : null;
  if (!taskMarker || pos < taskMarker.from || pos > taskMarker.to) return null;
  const char = state.sliceDoc(taskMarker.from + 1, taskMarker.from + 2);
  if (char !== " " && char !== "x" && char !== "X") return null;
  return { changes: { from: taskMarker.from + 1, to: taskMarker.from + 2, insert: char === " " ? "x" : " " }, userEvent: "input" };
}

/** Click a rendered checkbox; its data position points back to its source. */
export function taskToggleExtension(): Extension {
  return EditorView.domEventHandlers({
    click(event, view) {
      const target = event.target;
      if (!(target instanceof Element)) return false;
      const checkbox = target.closest<HTMLElement>(".nb-task-checkbox");
      if (!checkbox || !view.dom.contains(checkbox)) return false;
      const pos = Number(checkbox.dataset.taskPos);
      const change = toggleTaskAt(view.state, pos);
      if (change) view.dispatch(change);
      event.preventDefault();
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
    return JSON.stringify(this.kind) === JSON.stringify(other.kind);
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
        checkbox.dataset.taskPos = String(this.from + 3);
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
        link.addEventListener("click", (event) => {
          event.preventDefault();
          view.dom.dispatchEvent(new CustomEvent("notebook:wikilink", {
            bubbles: true,
            detail: { source: view.state.sliceDoc(this.from, this.to), from: this.from, to: this.to, resolved: this.kind.kind === "wikilink" && this.kind.resolved },
          }));
        });
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
