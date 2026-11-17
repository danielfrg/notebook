/*
 * Keyboard shortcuts: combo strings such as "mod+k" or "mod+shift+f", where mod is
 * ⌘ on Apple platforms and Ctrl elsewhere. A scope stack lets an open palette or
 * dialog suspend the app shortcuts underneath it. Matching is strict: modifiers a
 * combo does not name must not be pressed.
 */

export interface ParsedShortcut {
  key: string;
  mod: boolean;
  shift: boolean;
  alt: boolean;
  ctrl: boolean;
  meta: boolean;
}

export interface ShortcutKeyEvent {
  key: string;
  metaKey: boolean;
  ctrlKey: boolean;
  altKey: boolean;
  shiftKey: boolean;
}

const KEY_ALIASES: Record<string, string> = {
  space: " ",
  esc: "escape",
  return: "enter",
  up: "arrowup",
  down: "arrowdown",
  left: "arrowleft",
  right: "arrowright",
  plus: "+",
};

// US-layout pairs: a shifted symbol arrives as the produced character, so "shift+/" is key "?".
const SHIFTED_KEYS: Record<string, string> = {
  "1": "!",
  "2": "@",
  "3": "#",
  "4": "$",
  "5": "%",
  "6": "^",
  "7": "&",
  "8": "*",
  "9": "(",
  "0": ")",
  "-": "_",
  "=": "+",
  "[": "{",
  "]": "}",
  "\\": "|",
  ";": ":",
  "'": '"',
  ",": "<",
  ".": ">",
  "/": "?",
  "`": "~",
};

export function isMacPlatform(): boolean {
  if (typeof navigator === "undefined") return false;
  return /Mac|iPhone|iPad|iPod/.test(navigator.platform || navigator.userAgent || "");
}

/** Throws on a combo with no key or an unknown modifier, so typos fail at registration. */
export function parseShortcut(combo: string): ParsedShortcut {
  const parts = combo.toLowerCase().split("+");
  const rawKey = parts.pop() ?? "";
  const parsed: ParsedShortcut = {
    key: KEY_ALIASES[rawKey] ?? rawKey,
    mod: false,
    shift: false,
    alt: false,
    ctrl: false,
    meta: false,
  };
  if (!parsed.key) throw new Error(`Invalid shortcut "${combo}": missing key`);
  for (const part of parts) {
    if (part === "mod") parsed.mod = true;
    else if (part === "shift") parsed.shift = true;
    else if (part === "alt") parsed.alt = true;
    else if (part === "ctrl") parsed.ctrl = true;
    else if (part === "meta") parsed.meta = true;
    else throw new Error(`Invalid shortcut "${combo}": unknown modifier "${part}"`);
  }
  return parsed;
}

export function shortcutMatches(
  parsed: ParsedShortcut,
  event: ShortcutKeyEvent,
  isMac: boolean,
): boolean {
  const eventKey = event.key.toLowerCase();
  let keyMatches = eventKey === parsed.key;
  let shiftMatches = event.shiftKey === parsed.shift;
  const shiftedPair = parsed.shift ? SHIFTED_KEYS[parsed.key] : undefined;
  if (!keyMatches && shiftedPair !== undefined && shiftedPair === event.key) {
    keyMatches = true;
    shiftMatches = true;
  }
  if (!keyMatches) return false;
  const wantMeta = parsed.meta || (parsed.mod && isMac);
  const wantCtrl = parsed.ctrl || (parsed.mod && !isMac);
  return (
    event.metaKey === wantMeta &&
    event.ctrlKey === wantCtrl &&
    event.altKey === parsed.alt &&
    shiftMatches
  );
}

const KEY_LABELS: Record<string, string> = {
  escape: "Esc",
  enter: "↵",
  arrowup: "↑",
  arrowdown: "↓",
  arrowleft: "←",
  arrowright: "→",
  " ": "Space",
};

/** The keys to show for a combo: "mod+k" is ["⌘", "K"] on a Mac and ["Ctrl", "K"] elsewhere. */
export function formatShortcutKeys(combo: string, isMac: boolean = isMacPlatform()): string[] {
  const parsed = parseShortcut(combo);
  const keys: string[] = [];
  if (parsed.ctrl || (parsed.mod && !isMac)) keys.push(isMac ? "⌃" : "Ctrl");
  if (parsed.alt) keys.push(isMac ? "⌥" : "Alt");
  if (parsed.shift) keys.push(isMac ? "⇧" : "Shift");
  if (parsed.meta || (parsed.mod && isMac)) keys.push(isMac ? "⌘" : "Win");
  keys.push(
    KEY_LABELS[parsed.key] ?? (parsed.key.length === 1 ? parsed.key.toUpperCase() : parsed.key),
  );
  return keys;
}

export interface ShortcutOptions {
  /** The shortcut fires only while this scope is on top. Omit for the root scope. */
  scope?: string;
  /** Fire in a text field too. Combos with a modifier always do. */
  allowInInput?: boolean;
}

export const ROOT_SCOPE = "root";

interface Entry {
  parsed: ParsedShortcut;
  handler: (event: KeyboardEvent) => void;
  scope: string;
  allowInInput: boolean;
}

function isEditableTarget(target: EventTarget | null): boolean {
  const element = target as { tagName?: unknown; isContentEditable?: unknown } | null;
  if (!element || typeof element.tagName !== "string") return false;
  if (element.isContentEditable === true) return true;
  return ["INPUT", "TEXTAREA", "SELECT"].includes(element.tagName);
}

export function createShortcutManager(isMac: boolean = isMacPlatform()) {
  const entries = new Set<Entry>();
  const scopes: string[] = [];

  const activeScope = (): string => scopes[scopes.length - 1] ?? ROOT_SCOPE;

  return {
    /** Registers a combo and returns the function that removes it. */
    register(
      combo: string,
      handler: (event: KeyboardEvent) => void,
      options: ShortcutOptions = {},
    ): () => void {
      const entry: Entry = {
        parsed: parseShortcut(combo),
        handler,
        scope: options.scope ?? ROOT_SCOPE,
        allowInInput: options.allowInInput ?? false,
      };
      entries.add(entry);
      return () => entries.delete(entry);
    },
    /** Makes a scope the active one until the returned function is called. */
    pushScope(scope: string): () => void {
      scopes.push(scope);
      return () => {
        const at = scopes.lastIndexOf(scope);
        if (at >= 0) scopes.splice(at, 1);
      };
    },
    activeScope,
    /** Feeds a keydown; returns whether a shortcut handled it. */
    handleKeydown(event: KeyboardEvent): boolean {
      const scope = activeScope();
      for (const entry of entries) {
        if (entry.scope !== scope || !shortcutMatches(entry.parsed, event, isMac)) continue;
        const { mod, ctrl, alt, meta } = entry.parsed;
        const hasModifier = mod || ctrl || alt || meta;
        if (!hasModifier && !entry.allowInInput && isEditableTarget(event.target)) continue;
        event.preventDefault();
        entry.handler(event);
        return true;
      }
      return false;
    },
  };
}

export const appShortcuts = createShortcutManager();

/** Listens for keydown on the document and feeds the app shortcuts; returns the cleanup. */
export function initShortcuts(): () => void {
  const listener = (event: KeyboardEvent) => void appShortcuts.handleKeydown(event);
  document.addEventListener("keydown", listener);
  return () => document.removeEventListener("keydown", listener);
}
