import type { CompletionSource } from "@codemirror/autocomplete";
import { fuzzyNotes } from "../fuzzy.ts";

export function wikilinkCompletions(
  notes: readonly string[],
  query: string,
  limit = 20,
): { label: string; detail: string; apply: string }[] {
  const counts = new Map<string, number>();
  for (const path of notes) {
    const name = path
      .slice(path.lastIndexOf("/") + 1)
      .replace(/\.md$/i, "")
      .normalize("NFC")
      .toLocaleLowerCase();
    counts.set(name, (counts.get(name) ?? 0) + 1);
  }
  return fuzzyNotes(notes, query, limit).map(({ path, title }) => ({
    label: title,
    detail: path.includes("/")
      ? path.slice(0, path.lastIndexOf("/"))
      : counts.get(title.normalize("NFC").toLocaleLowerCase())! > 1
        ? "Vault root"
        : "",
    apply: title,
  }));
}

/** Complete the open wikilink at the cursor without changing surrounding Markdown. */
export function wikilinkSource(getNotes: () => readonly string[]): CompletionSource {
  return (context) => {
    const line = context.state.doc.lineAt(context.pos);
    const before = context.state.sliceDoc(line.from, context.pos);
    const opening = before.lastIndexOf("[[");
    if (opening < 0 || /[\[\]|#]/.test(before.slice(opening + 2))) return null;
    const from = line.from + opening + 2;
    const query = before.slice(opening + 2);
    const options = wikilinkCompletions(getNotes(), query).map((hit) => ({
      label: hit.label,
      detail: hit.detail,
      apply: (
        view: import("@codemirror/view").EditorView,
        _completion: unknown,
        start: number,
        end: number,
      ) => {
        const suffix = view.state.sliceDoc(end, end + 2) === "]]" ? "" : "]]";
        view.dispatch({ changes: { from: start, to: end, insert: hit.apply + suffix } });
      },
    }));
    return options.length ? { from, options } : null;
  };
}
