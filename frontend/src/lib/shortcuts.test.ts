import test from "node:test";
import assert from "node:assert/strict";
import {
  createShortcutManager,
  formatShortcutKeys,
  parseShortcut,
  shortcutMatches,
  type ShortcutKeyEvent,
} from "./shortcuts.ts";

const press = (
  key: string,
  modifiers: Partial<ShortcutKeyEvent> & { target?: EventTarget } = {},
): KeyboardEvent =>
  ({
    key,
    metaKey: false,
    ctrlKey: false,
    altKey: false,
    shiftKey: false,
    target: null,
    preventDefault() {},
    ...modifiers,
  }) as unknown as KeyboardEvent;

test("parseShortcut reads modifiers and aliases, and rejects typos", () => {
  assert.deepEqual(parseShortcut("mod+shift+f"), {
    key: "f",
    mod: true,
    shift: true,
    alt: false,
    ctrl: false,
    meta: false,
  });
  assert.equal(parseShortcut("esc").key, "escape");
  assert.equal(parseShortcut("mod+plus").key, "+");
  assert.throws(() => parseShortcut("mod+"));
  assert.throws(() => parseShortcut("hyper+k"));
});

test("mod is Meta on a Mac and Ctrl elsewhere, and matching is strict", () => {
  const combo = parseShortcut("mod+k");
  assert.ok(shortcutMatches(combo, press("k", { metaKey: true }), true));
  assert.ok(!shortcutMatches(combo, press("k", { ctrlKey: true }), true));
  assert.ok(shortcutMatches(combo, press("k", { ctrlKey: true }), false));
  assert.ok(!shortcutMatches(combo, press("k", { ctrlKey: true, shiftKey: true }), false));
  assert.ok(!shortcutMatches(combo, press("j", { ctrlKey: true }), false));
});

test("a shifted symbol matches through the character it produces", () => {
  const combo = parseShortcut("mod+shift+/");
  assert.ok(shortcutMatches(combo, press("?", { ctrlKey: true, shiftKey: true }), false));
  assert.ok(!shortcutMatches(parseShortcut("mod+/"), press("?", { ctrlKey: true }), false));
});

test("formatShortcutKeys labels keys for each platform", () => {
  assert.deepEqual(formatShortcutKeys("mod+k", true), ["⌘", "K"]);
  assert.deepEqual(formatShortcutKeys("mod+k", false), ["Ctrl", "K"]);
  assert.deepEqual(formatShortcutKeys("mod+shift+f", true), ["⇧", "⌘", "F"]);
  assert.deepEqual(formatShortcutKeys("escape", false), ["Esc"]);
});

test("a pushed scope suspends the root shortcuts until it is popped", () => {
  const manager = createShortcutManager(false);
  const calls: string[] = [];
  manager.register("mod+k", () => calls.push("root"));
  manager.register("mod+k", () => calls.push("palette"), { scope: "palette" });
  const pop = manager.pushScope("palette");
  assert.equal(manager.handleKeydown(press("k", { ctrlKey: true })), true);
  pop();
  assert.equal(manager.handleKeydown(press("k", { ctrlKey: true })), true);
  assert.deepEqual(calls, ["palette", "root"]);
});

test("a bare key does not fire in a text field unless allowed, a modifier combo does", () => {
  const manager = createShortcutManager(false);
  const calls: string[] = [];
  manager.register("g", () => calls.push("g"));
  manager.register("mod+s", () => calls.push("save"));
  const field = { target: { tagName: "INPUT" } as unknown as EventTarget };
  assert.equal(manager.handleKeydown(press("g", field)), false);
  assert.equal(manager.handleKeydown(press("s", { ctrlKey: true, ...field })), true);
  assert.deepEqual(calls, ["save"]);
});

test("unregistering removes the shortcut", () => {
  const manager = createShortcutManager(false);
  const remove = manager.register("mod+e", () => {});
  remove();
  assert.equal(manager.handleKeydown(press("e", { ctrlKey: true })), false);
});
