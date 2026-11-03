import assert from "node:assert/strict";
import { test } from "node:test";
import type { EditorView } from "@codemirror/view";
import { LivePreviewWidget } from "./widgets.ts";

test("a focused wikilink widget activates on Enter and Space before CodeMirror handles them", () => {
  const previous = Object.getOwnPropertyDescriptor(globalThis, "document");
  let clicks = 0;
  const anchor = Object.assign(new EventTarget(), {
    className: "", href: "", textContent: "",
    click() { clicks++; },
  });
  Object.defineProperty(globalThis, "document", { configurable: true, value: { createElement: () => anchor } });
  try {
    new LivePreviewWidget({ kind: "wikilink", label: "Known", resolved: true }).toDOM({} as EditorView);
    const press = (key: string): Event => {
      const event = new Event("keydown", { cancelable: true });
      Object.defineProperty(event, "key", { value: key });
      anchor.dispatchEvent(event);
      return event;
    };
    assert.equal(press("ArrowDown").defaultPrevented, false);
    assert.equal(clicks, 0);
    assert.equal(press("Enter").defaultPrevented, true);
    assert.equal(press(" ").defaultPrevented, true);
    assert.equal(clicks, 2);
  } finally {
    if (previous) Object.defineProperty(globalThis, "document", previous);
    else Reflect.deleteProperty(globalThis, "document");
  }
});

test("the properties panel, a block widget, keeps its space in padding: CodeMirror does not measure margins", async () => {
  const { readFile } = await import("node:fs/promises");
  const css = await readFile(new URL("../../editor.css", import.meta.url), "utf8");
  const rules = [...css.matchAll(/([^{}]*\.nb-properties)\s*\{([^{}]*)\}/g)];
  assert.ok(rules.length > 0, "editor.css styles the properties panel");
  for (const [, selector, body] of rules) assert.doesNotMatch(body, /\bmargin(?:-(?:top|bottom|block\S*))?\s*:/, selector.trim());
});
