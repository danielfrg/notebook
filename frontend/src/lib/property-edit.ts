/*
 * Edits to the frontmatter that the Properties panel makes. The panel never
 * rewrites the YAML: it finds where a value is written, and each control
 * replaces just that text (a flow list is rewritten as a whole, as one item
 * cannot change inside it). Everything else in the frontmatter, comments and
 * properties the panel cannot show included, stays as it was.
 *
 * Positions are in the document, which is what the editor dispatches against.
 * That is why the lines come in already split, each with the position of its
 * start: line breaks count as one position whatever they are in the file. An
 * inserted "\n" is a line break of the document, for the caller to spell.
 */
import {
  closingQuote,
  isIndented,
  isItem,
  isNoise,
  readValue,
  scalar,
  splitEntry,
} from "./properties.ts";

export type Line = { text: string; from: number };
export type Span = { from: number; to: number };
export type PropertyType = "text" | "checkbox" | "date" | "list";

/** A replacement of a range; a "\n" in insert is a line break. */
export type PropertyEdit = { from: number; to: number; insert: string };

export type EditableProperty = {
  key: string;
  type: PropertyType;
  /** Where the key is written, without the colon. */
  keySpan: Span;
  /** The whole property, from the line break before it to the end of its last line. */
  block: Span;
  /** The items of a list; a single value in an array of one, or none when empty. */
  values: string[];
  /** A single value: where it is written, and the quote around it. */
  value?: Span & { quote: "" | '"' | "'"; empty: boolean };
  /** A list written as [a, b]: from the "[" to the "]" (empty, where it would go, if there is no list yet). */
  flow?: Span & { empty: boolean };
  /** A list written as "- item" lines. */
  items?: { value: string; line: Span; breakBefore: number }[];
  /** Where a new "- item" line goes, and what it starts with. */
  append?: { at: number; prefix: string };
};

/** Keys whose value is a list even when it is empty. */
const LIST_KEYS = new Set(["tags", "aliases", "cssclasses"]);
const DATE = /^\d{4}-\d{2}-\d{2}$/;

const lineEnd = (line: Line): number => line.from + line.text.length;

/** The index of the colon that ends the key, as splitEntry cuts it. */
function colonOf(text: string): number {
  for (let colon = text.indexOf(":"); colon >= 0; colon = text.indexOf(":", colon + 1)) {
    const next = text.charAt(colon + 1);
    if (next === "" || next === " " || next === "\t") return colon;
  }
  return -1;
}

/** The index after the "]" that closes the list opened at text[start], or -1. */
function flowClose(text: string, start: number): number {
  for (let i = start + 1; i < text.length; i++) {
    const char = text[i];
    if (char === '"' || char === "'") {
      const end = closingQuote(text, i);
      if (end < 0) return -1;
      i = end;
    } else if (char === "]") return i + 1;
  }
  return -1;
}

/** Where a plain value written at text[start] ends: before " #" or the trailing space. */
function plainEnd(text: string, start: number): number {
  const rest = text.slice(start);
  const comment = /(^|\s)#/.exec(rest);
  return start + (comment ? rest.slice(0, comment.index) : rest).trimEnd().length;
}

/**
 * The properties of the lines between the fences that the panel can show and
 * edit, in file order: those parseProperties reads, with where they are.
 */
export function locateProperties(lines: Line[]): EditableProperty[] {
  const properties: EditableProperty[] = [];
  for (let i = 0; i < lines.length; i++) {
    const line = lines[i];
    if (isNoise(line.text) || isIndented(line.text)) continue;
    const entry = splitEntry(line.text);
    if (entry === null) continue;
    const colon = colonOf(line.text);

    const value = entry.value.startsWith("#") ? "" : entry.value;
    const bodyAt: number[] = [];
    while (i + 1 < lines.length) {
      const next = lines[i + 1].text;
      if (!isNoise(next) && !isIndented(next) && !(value === "" && isItem(next))) break;
      if (!isNoise(next)) bodyAt.push(i + 1);
      i++;
    }
    const read = readValue(
      entry.key,
      value,
      bodyAt.map((at) => lines[at].text),
    );
    if (read === null) continue;

    const key = read.key;
    const keySpan = { from: line.from, to: line.from + line.text.slice(0, colon).trimEnd().length };
    const lastLine = bodyAt.length > 0 ? lines[bodyAt[bodyAt.length - 1]] : line;
    const block = { from: line.from - 1, to: lineEnd(lastLine) };
    const base = { key, keySpan, block };
    const rest = line.text.slice(colon + 1);
    const valueStart = colon + 1 + (rest.length - rest.trimStart().length);
    const start = line.from + valueStart;

    if (value === "" && bodyAt.length > 0) {
      const items = bodyAt
        .map((at) => {
          const item = lines[at];
          const text = scalar(item.text.trimStart().slice(1).trim()) ?? "";
          const previous = lines[at - 1];
          return {
            value: text,
            line: { from: item.from, to: lineEnd(item) },
            breakBefore: lineEnd(previous),
          };
        })
        .filter((item) => item.value !== "");
      const last = lines[bodyAt[bodyAt.length - 1]];
      const prefix = /^\s*-[ \t]*/.exec(lines[bodyAt[0]].text)?.[0] ?? "  - ";
      properties.push({
        ...base,
        type: "list",
        values: read.values,
        items,
        append: { at: lineEnd(last), prefix: prefix.endsWith(" ") ? prefix : `${prefix} ` },
      });
    } else if (value.startsWith("[")) {
      const close = flowClose(line.text, valueStart);
      if (close < 0) continue;
      properties.push({
        ...base,
        type: "list",
        values: read.values,
        flow: { from: start, to: line.from + close, empty: false },
      });
    } else if (read.values.length === 0 && LIST_KEYS.has(key.toLowerCase())) {
      properties.push({
        ...base,
        type: "list",
        values: [],
        flow: { from: line.from + colon + 1, to: line.from + colon + 1, empty: true },
      });
    } else if (read.values.length === 0) {
      properties.push({
        ...base,
        type: "text",
        values: [],
        value: { from: line.from + colon + 1, to: line.from + colon + 1, quote: "", empty: true },
      });
    } else {
      const first = value.charAt(0);
      const quote = first === '"' || first === "'" ? first : "";
      const end = quote
        ? line.from + closingQuote(line.text, valueStart) + 1
        : line.from + plainEnd(line.text, valueStart);
      const text = read.values[0];
      const type: PropertyType =
        !quote && (text === "true" || text === "false")
          ? "checkbox"
          : DATE.test(text)
            ? "date"
            : "text";
      properties.push({
        ...base,
        type,
        values: read.values,
        value: { from: start, to: end, quote, empty: false },
      });
    }
  }
  return properties;
}

/** Whether a value would be read as something else than the text it is when plain. */
function needsQuotes(text: string, inList: boolean): boolean {
  if (text !== text.trim()) return true;
  if (/^[[\]{}>|*&!%@`'"#?:,-]/.test(text) && !/^-[^\s-]/.test(text)) return true;
  if (/: |:$| #/.test(text)) return true;
  if (inList && /[,[\]{}]/.test(text)) return true;
  return /^(true|false|null|~)$/i.test(text);
}

/** A value as it is written in YAML, in the quotes it had if it still needs them. */
export function formatScalar(text: string, quote: "" | '"' | "'" = "", inList = false): string {
  const clean = text.replace(/[\r\n]+/g, " ");
  if (quote === "'") return `'${clean.replaceAll("'", "''")}'`;
  if (quote === '"' || needsQuotes(clean, inList))
    return `"${clean.replaceAll("\\", "\\\\").replaceAll('"', '\\"')}"`;
  return clean;
}

const flowText = (values: string[]): string =>
  `[${values.map((value) => formatScalar(value, "", true)).join(", ")}]`;

/** Sets a single value: only the text of the value changes. */
export function setValue(property: EditableProperty, text: string): PropertyEdit | null {
  const value = property.value;
  if (!value) return null;
  if (text === "" && value.empty) return null;
  const insert = text === "" ? "" : formatScalar(text, value.quote);
  return { from: value.from, to: value.to, insert: value.empty ? ` ${insert}` : insert };
}

/** Flips true and false. */
export function toggleValue(property: EditableProperty): PropertyEdit | null {
  const value = property.value;
  if (property.type !== "checkbox" || !value) return null;
  return {
    from: value.from,
    to: value.to,
    insert: property.values[0] === "true" ? "false" : "true",
  };
}

/** Adds an item at the end of a list. */
export function addItem(property: EditableProperty, text: string): PropertyEdit | null {
  const item = text.trim();
  if (item === "" || property.type !== "list") return null;
  if (property.flow) {
    const { from, to, empty } = property.flow;
    const insert = flowText([...property.values, item]);
    return { from, to, insert: empty ? ` ${insert}` : insert };
  }
  if (property.append) {
    return {
      from: property.append.at,
      to: property.append.at,
      insert: `\n${property.append.prefix}${formatScalar(item)}`,
    };
  }
  return null;
}

/** Takes out the item at an index of the values of a list. */
export function removeItem(property: EditableProperty, index: number): PropertyEdit | null {
  if (property.type !== "list" || index < 0 || index >= property.values.length) return null;
  if (property.flow && !property.flow.empty) {
    return {
      from: property.flow.from,
      to: property.flow.to,
      insert: flowText(property.values.filter((_, at) => at !== index)),
    };
  }
  const item = property.items?.[index];
  return item ? { from: item.breakBefore, to: item.line.to, insert: "" } : null;
}

/** Whether a key can be written as a new property: not empty, plain, and not there yet. */
export function validKey(key: string, existing: readonly string[]): boolean {
  return (
    key !== "" &&
    !/^[-#[\]{}>|*&!%@`'"?:,]|:|^\s|\s$/.test(key) &&
    !existing.some((name) => name.toLowerCase() === key.toLowerCase())
  );
}

/** A new, empty property after the last line of the frontmatter's content. */
export function addProperty(at: number, key: string): PropertyEdit {
  return { from: at, to: at, insert: `\n${key}:` };
}

/** Renames a property: only its key changes. Null when the name cannot be used. */
export function renameKey(
  property: EditableProperty,
  name: string,
  existing: readonly string[],
): PropertyEdit | null {
  const key = name.trim();
  if (key === property.key) return null;
  if (
    !validKey(
      key,
      existing.filter((other) => other !== property.key),
    )
  )
    return null;
  return { from: property.keySpan.from, to: property.keySpan.to, insert: key };
}

/** Takes a property out, with its line break and its lines. */
export function removeProperty(property: EditableProperty): PropertyEdit {
  return { from: property.block.from, to: property.block.to, insert: "" };
}
