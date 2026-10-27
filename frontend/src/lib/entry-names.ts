import { joinPath, parentFolder, validateEntryName } from "./paths.ts";

/** A note's file name always has the lower-case extension the vault lists. */
export function noteFileName(name: string): string {
  return `${name.trim().replace(/\.md$/i, "")}.md`;
}

/** Validate both what was typed and the actual filesystem component. */
export function validateNoteName(name: string): string | null {
  return validateEntryName(name.trim()) ?? validateEntryName(noteFileName(name));
}

export function uniqueNoteName(existing: readonly string[], base: string): string {
  const title = base.replace(/\.md$/i, "");
  const used = new Set(existing.map((name) => name.replace(/\.md$/i, "").toLocaleLowerCase()));
  if (!used.has(title.toLocaleLowerCase())) return title;
  for (let number = 2; ; number++) {
    const candidate = `${title} ${number}`;
    if (!used.has(candidate.toLocaleLowerCase())) return candidate;
  }
}

/** Rename within the current folder; notes keep their .md extension. */
export function renameTarget(path: string, newName: string): string {
  newName = newName.trim();
  const isNote = path.endsWith(".md");
  const reason = isNote ? validateNoteName(newName) : validateEntryName(newName);
  if (reason) throw new Error(reason);
  return joinPath(parentFolder(path), isNote ? noteFileName(newName) : newName);
}
