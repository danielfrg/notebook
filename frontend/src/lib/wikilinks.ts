import { parentFolder } from "./paths.ts";

/**
 * An Obsidian wikilink. For an embed (![[img.png|300]]) the alias is what is
 * after the pipe, here the size, and not a label.
 */
export type Wikilink = {
  target: string;
  heading: string | null;
  alias: string | null;
  embed: boolean;
};

/**
 * Reads one whole wikilink, the text of its node: [[Note]], [[folder/Note]],
 * [[Note#Heading]], [[Note|alias]], ![[image.png|300]] and their mixes. Null
 * when the text is not exactly one: unclosed, empty, nothing before the "#" or
 * "|" (a link to a heading of the same note is not shown as a link), or with a
 * bracket or a line break inside, which Obsidian does not link either.
 *
 * A "#" or "|" in a note's name cannot be written in a wikilink, so a link to
 * "100% #1.md" reads as the note "100% " and the heading "1".
 */
export function parseWikilink(source: string): Wikilink | null {
  const embed = source.startsWith("!");
  const open = embed ? 1 : 0;
  if (!source.startsWith("[[", open) || !source.endsWith("]]")) return null;
  const inner = source.slice(open + 2, -2);
  if (/[[\]\n\r]/.test(inner)) return null;

  // The first pipe starts the alias, which may hold more pipes. In a table
  // cell Obsidian wants the pipe escaped (\|), and the backslash is no part of
  // the target.
  const pipe = inner.indexOf("|");
  const alias = pipe < 0 ? "" : inner.slice(pipe + 1).trim();
  let place = pipe < 0 ? inner : inner.slice(0, pipe);
  if (pipe >= 0 && place.endsWith("\\")) place = place.slice(0, -1);

  // The first "#" starts the heading, which may hold more ("Note#H1#H2", "Note#^block").
  const hash = place.indexOf("#");
  const target = (hash < 0 ? place : place.slice(0, hash)).trim();
  const heading = hash < 0 ? "" : place.slice(hash + 1).trim();
  if (target === "") return null;
  return { target, heading: heading || null, alias: alias || null, embed };
}

// Names compare in one case and one Unicode form: a vault synced from a Mac
// may spell "é" as "e" plus an accent, and a link typed on a keyboard does not.
const fold = (text: string): string => text.normalize("NFC").toLowerCase();

type Candidate = { path: string; key: string; home: boolean; depth: number };

/** Whether a ranks ahead of b: in the folder of the linking note, then the fewest segments, then the alphabet. */
const before = (a: Candidate, b: Candidate): boolean =>
  a.home !== b.home
    ? a.home
    : a.depth !== b.depth
      ? a.depth < b.depth
      : a.key !== b.key
        ? a.key < b.key
        : a.path < b.path; // only names that differ in case alone get this far

/**
 * The best path in paths for name, which is a target folded and given its
 * extension, or null. A path matches when it is name, or ends with name after
 * a "/": "folder/Note.md" is found in "x/folder/Note.md" and not in
 * "xfolder/Note.md". Of several matches the best wins, whatever the order of paths.
 */
function resolve(paths: readonly string[], from: string, name: string): string | null {
  const home = fold(parentFolder(from));
  let best: Candidate | null = null;
  for (const path of paths) {
    const key = fold(path);
    if (key !== name && !key.endsWith("/" + name)) continue;
    const candidate = { path, key, home: fold(parentFolder(path)) === home, depth: key.split("/").length };
    if (best === null || before(candidate, best)) best = candidate;
  }
  return best === null ? null : best.path;
}

/**
 * The note that a wikilink target names, as its path in the vault, or null.
 * notes are the note paths (with .md) of the vault, from is the path of the
 * note holding the link, and target is a Wikilink's target. A bare name finds
 * the note with that file name anywhere in the vault, in any case and with or
 * without .md; "folder/Note" finds it by the end of its path. If several
 * notes match, the one in the folder of from wins, then the one with the
 * fewest path segments, then the first in alphabetical order.
 */
export function resolveNote(notes: readonly string[], from: string, target: string): string | null {
  const name = fold(target.trim());
  if (name === "") return null;
  return resolve(notes, from, name.endsWith(".md") ? name : name + ".md");
}

/**
 * The image that an embed target names, by the same rules as resolveNote,
 * except that the extension is part of the name: "pic" does not find "pic.png".
 */
export function resolveAsset(assets: readonly string[], from: string, target: string): string | null {
  const name = fold(target.trim());
  return name === "" ? null : resolve(assets, from, name);
}
