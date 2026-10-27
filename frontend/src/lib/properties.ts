/*
 * A reader for the frontmatter of a note, for the Properties summary that
 * Live Preview shows in its place. It is display only (the document is never
 * rewritten from it) and reads just the flat part of YAML that notes use:
 * "key: value", "key: [a, b]", and a list of "- item" lines. Every value is
 * the text as written (a date, a number and true are strings), with quotes
 * taken off. Anything it cannot show faithfully is left out, never guessed at,
 * and nothing throws. The scanner has no regular expression that can backtrack
 * over a long line, as a synced file may hold anything.
 */

/**
 * One key of the frontmatter. list is true for a YAML sequence, even with one
 * item or none, and false for a single value; a key with no value has no values.
 */
export type Property = { key: string; values: string[]; list: boolean };

const isIndented = (line: string): boolean => line.startsWith(" ") || line.startsWith("\t");
/** A line that says nothing: blank, or a comment. */
const isNoise = (line: string): boolean => line.trim() === "" || line.trimStart().startsWith("#");
const isItem = (line: string): boolean => {
  const text = line.trimStart();
  return text === "-" || text.startsWith("- ") || text.startsWith("-\t");
};
/** Whether the rest of a line, after a value, is empty or a comment. */
const endsAtComment = (rest: string): boolean => {
  const text = rest.trim();
  return text === "" || text.startsWith("#");
};

/** The properties in the YAML between the fences of a note's frontmatter, in file order. */
export function parseProperties(yaml: string): Property[] {
  const lines = yaml.split(/\r\n|\r|\n/);
  const properties: Property[] = [];
  for (let i = 0; i < lines.length; i++) {
    // Only a line at the left edge starts a property; indented lines either
    // belong to the property above, which takes them below, or to nothing.
    if (isNoise(lines[i]) || isIndented(lines[i])) continue;
    const entry = splitEntry(lines[i]);
    if (entry === null) continue;

    // The value, if it is not on the line itself, is below it: indented
    // lines, or items written level with the key.
    const value = entry.value.startsWith("#") ? "" : entry.value;
    const body: string[] = [];
    while (i + 1 < lines.length) {
      const next = lines[i + 1];
      if (!isNoise(next) && !isIndented(next) && !(value === "" && isItem(next))) break;
      if (!isNoise(next)) body.push(next);
      i++;
    }

    const property = readValue(entry.key, value, body);
    if (property !== null) properties.push(property);
  }
  return properties;
}

function readValue(key: string, value: string, body: string[]): Property | null {
  if (value === "") {
    if (body.length === 0) return { key, values: [], list: false };
    return body.every(isItem) ? readItems(key, body) : null; // else a nested mapping
  }
  if (body.length > 0) return null; // a value that runs over several lines
  if (value.startsWith("[")) {
    const values = flowItems(value);
    return values === null ? null : { key, values, list: true };
  }
  const text = scalar(value);
  if (text === null) return null;
  return { key, values: text === "" ? [] : [text], list: false };
}

function readItems(key: string, items: string[]): Property | null {
  const values: string[] = [];
  for (const item of items) {
    const text = scalar(item.trimStart().slice(1).trim());
    if (text === null) return null;
    if (text !== "") values.push(text);
  }
  return { key, values, list: true };
}

/**
 * Cuts "key: value" at its first colon that is followed by a space or ends the
 * line, so that "source: https://x.y/z" and "at: 10:30" keep their colons. Null
 * for a line that is no key: a list item, no colon, an empty or unreadable key.
 */
function splitEntry(line: string): { key: string; value: string } | null {
  if (isItem(line)) return null;
  for (let colon = line.indexOf(":"); colon >= 0; colon = line.indexOf(":", colon + 1)) {
    const next = line.charAt(colon + 1);
    if (next !== "" && next !== " " && next !== "\t") continue;
    const key = scalar(line.slice(0, colon).trim());
    return key ? { key, value: line.slice(colon + 1).trim() } : null;
  }
  return null;
}

/**
 * A plain, 'single quoted' or "double quoted" scalar, as its text; null for
 * anything else a value can be (a flow collection, a block scalar) or for a
 * quote that does not close where it should. A plain scalar ends at " #".
 * Escapes are read where they matter for display: \" and \\ in double
 * quotes, '' in single quotes.
 */
function scalar(text: string): string | null {
  const first = text.charAt(0);
  if (first === '"' || first === "'") {
    const end = closingQuote(text, 0);
    if (end < 0 || !endsAtComment(text.slice(end + 1))) return null;
    return unquote(text.slice(0, end + 1));
  }
  if (first === "[" || first === "{" || first === "|" || first === ">") return null;
  return text.replace(/(^|\s)#.*$/, "").trim();
}

/** The index of the quote that closes the one at text[start], or -1. */
function closingQuote(text: string, start: number): number {
  const quote = text[start];
  for (let i = start + 1; i < text.length; i++) {
    if (quote === '"' && text[i] === "\\") i++; // an escaped character
    else if (text[i] === quote) {
      if (quote === "'" && text[i + 1] === "'") i++; // '' is a quote inside the string
      else return i;
    }
  }
  return -1;
}

/** The text of a quoted string, which is passed with its quotes. */
function unquote(quoted: string): string {
  const inner = quoted.slice(1, -1);
  return quoted[0] === '"' ? inner.replace(/\\(["\\])/g, "$1") : inner.replaceAll("''", "'");
}

/**
 * The items of a list written on one line, [a, "b c"]; null when it is not one
 * that can be read: it does not close, it holds a list or a mapping, or text
 * follows it. Empty items (a trailing comma) are dropped.
 */
function flowItems(text: string): string[] | null {
  const items: string[] = [];
  let i = 1; // after the "["
  const skipSpace = () => {
    while (text[i] === " " || text[i] === "\t") i++;
  };
  for (;;) {
    skipSpace();
    if (i >= text.length) return null;
    let item: string;
    if (text[i] === '"' || text[i] === "'") {
      const end = closingQuote(text, i);
      if (end < 0) return null;
      item = unquote(text.slice(i, end + 1));
      i = end + 1;
    } else {
      let end = i;
      while (end < text.length && text[end] !== "," && text[end] !== "]") {
        if (text[end] === "[" || text[end] === "{" || text[end] === "}") return null;
        end++;
      }
      item = text.slice(i, end).trim();
      i = end;
    }
    if (item !== "") items.push(item);
    skipSpace();
    if (text[i] === ",") i++;
    else if (text[i] === "]") break;
    else return null;
  }
  return endsAtComment(text.slice(i + 1)) ? items : null;
}
