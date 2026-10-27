/*
 * Vault paths: slash-separated, relative to the vault, case-preserving, never
 * normalized. These helpers only cut and join strings; the server decides what
 * a valid path is (internal/vault CleanPath), and validateEntryName mirrors
 * its rules for a single name so that a dialog can say why before asking.
 */

const NOTE_EXTENSION = ".md";

/** The last segment of a path: "c.md" for "a/b/c.md", "b" for "a/b". */
export const baseName = (path: string): string => path.slice(path.lastIndexOf("/") + 1);

/** The folder a path is in, "" at the root. */
export const parentFolder = (path: string): string => {
  const slash = path.lastIndexOf("/");
  return slash < 0 ? "" : path.slice(0, slash);
};

/** A note's base name without .md. Only a lower-case .md makes a note, as on the server. */
export const noteTitle = (path: string): string => {
  const name = baseName(path);
  return name.endsWith(NOTE_EXTENSION) ? name.slice(0, -NOTE_EXTENSION.length) : name;
};

export const joinPath = (folder: string, name: string): string =>
  folder === "" ? name : `${folder}/${name}`;

/** The file name for a note called name: appends .md unless it is already there. */
export const withMd = (name: string): string =>
  name.endsWith(NOTE_EXTENSION) ? name : name + NOTE_EXTENSION;

// A path component is at most 255 bytes of UTF-8 (the full path at most 512,
// which only the server can judge).
const MAX_NAME_BYTES = 255;

/**
 * Why a name cannot be given to a note or a folder, or null when it can. The
 * name is judged as typed: a note's .md is not counted. A name made only of
 * spaces counts as empty, which is stricter than the server and never matters.
 */
export const validateEntryName = (name: string): string | null => {
  if (name.trim() === "") return "Enter a name";
  if (name.includes("/") || name.includes("\\")) return "A name cannot contain / or \\";
  // Covers "." and ".." too: a name that starts with a dot is hidden.
  if (name.startsWith(".")) return "A name cannot start with a dot";
  if (/\p{Cc}/u.test(name)) return "A name cannot contain control characters";
  if (new TextEncoder().encode(name).length > MAX_NAME_BYTES) return "That name is too long";
  return null;
};
