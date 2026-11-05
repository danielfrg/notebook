import { baseName, noteTitle, parentFolder } from "./paths.ts";

/**
 * A row of the file tree. name is what the sidebar shows: the folder's name,
 * or a note's file name without .md. path is the vault-relative path that the
 * API takes (and a folder's key in the set of expanded folders).
 */
export type TreeNode =
  | { kind: "folder"; name: string; path: string; children: TreeNode[] }
  | { kind: "note"; name: string; path: string; size: number; modified: string };

export type Row = { node: TreeNode; depth: number };

type Listing = {
  notes: readonly { path: string; size: number; modified: string }[];
  folders: readonly string[];
};

// Case and accents do not count and numbers count as numbers, so "Note 2"
// comes before "Note 10" and "Ñandú" sorts with the n's. The locale is fixed
// ("es" would make ñ a letter of its own) so that the order is the same on
// every browser, and like the server's search it ignores accents.
const loose = new Intl.Collator("en", { numeric: true, sensitivity: "base" });
const strict = new Intl.Collator("en", { numeric: true });

/** Names that the loose order calls equal ("résumé", "Resume") still get one fixed order. */
const compareNames = (a: string, b: string): number =>
  loose.compare(a, b) || strict.compare(a, b) || (a < b ? -1 : a > b ? 1 : 0);

const compareNodes = (a: TreeNode, b: TreeNode): number =>
  (a.kind === b.kind ? 0 : a.kind === "folder" ? -1 : 1) || compareNames(a.name, b.name);

/**
 * The folders and notes of a vault as a tree, folders before notes, each in
 * natural order. Empty folders are in it, and so are the folders above a note
 * that the list of folders leaves out. The listing is not changed.
 */
export function buildTree(listing: Listing): TreeNode[] {
  const roots: TreeNode[] = [];
  const folders = new Map<string, Extract<TreeNode, { kind: "folder" }>>();

  const childrenOf = (folder: string): TreeNode[] =>
    folder === "" ? roots : folderAt(folder).children;
  const folderAt = (path: string) => {
    let node = folders.get(path);
    if (!node) {
      node = { kind: "folder", name: baseName(path), path, children: [] };
      folders.set(path, node);
      childrenOf(parentFolder(path)).push(node);
    }
    return node;
  };

  for (const path of listing.folders) folderAt(path);
  for (const { path, size, modified } of listing.notes) {
    childrenOf(parentFolder(path)).push({
      kind: "note",
      name: noteTitle(path),
      path,
      size,
      modified,
    });
  }

  roots.sort(compareNodes);
  for (const folder of folders.values()) folder.children.sort(compareNodes);
  return roots;
}

/**
 * The rows to draw: every node of the top level and, below each folder in
 * expanded (a set of folder paths), its children one level deeper. A folder
 * inside a closed one stays hidden whether or not it is in the set.
 */
export function visibleRows(tree: readonly TreeNode[], expanded: ReadonlySet<string>): Row[] {
  const rows: Row[] = [];
  const walk = (nodes: readonly TreeNode[], depth: number) => {
    for (const node of nodes) {
      rows.push({ node, depth });
      if (node.kind === "folder" && expanded.has(node.path)) walk(node.children, depth + 1);
    }
  };
  walk(tree, 0);
  return rows;
}

/** The one keyboard entry point, even when a refresh removes the focused row. */
export function tabStopPath(rows: readonly Row[], focused: string | null): string | null {
  return rows.find((row) => row.node.path === focused)?.node.path ?? rows[0]?.node.path ?? null;
}

/** The folders above a path, outermost first: ["a", "a/b"] for "a/b/c.md". */
export function ancestors(path: string): string[] {
  const folders = path.split("/").slice(0, -1);
  return folders.map((_, i) => folders.slice(0, i + 1).join("/"));
}
