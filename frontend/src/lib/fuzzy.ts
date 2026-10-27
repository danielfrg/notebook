export type FuzzyHit = { path: string; title: string; score: number; ranges: [number, number][] };

/** Fold displayed characters while retaining their UTF-16 spans for highlighting. */
function indexed(text: string): { chars: string[]; spans: [number, number][] } {
  const chars: string[] = [];
  const spans: [number, number][] = [];
  let offset = 0;
  for (const char of text) {
    const folded = char.normalize("NFD").replace(/\p{M}/gu, "").toLocaleLowerCase();
    for (const letter of folded) {
      chars.push(letter);
      spans.push([offset, offset + char.length]);
    }
    offset += char.length;
  }
  return { chars, spans };
}

function match(text: string, query: string): { score: number; ranges: [number, number][] } | null {
  const { chars, spans } = indexed(text);
  const letters = [...query];
  if (letters.length > chars.length) return null;
  let previous = new Map<number, { score: number; indexes: number[] }>();
  for (let q = 0; q < letters.length; q++) {
    const next = new Map<number, { score: number; indexes: number[] }>();
    let bestEarlier: { score: number; indexes: number[] } | undefined;
    for (let i = q; i < chars.length; i++) {
      const preceding = previous.get(i - 1);
      if (preceding && (!bestEarlier || preceding.score > bestEarlier.score)) bestEarlier = preceding;
      if (chars[i] !== letters[q]) continue;
      const start = spans[i]![0];
      const wordStart = start === 0 || /[^\p{L}\p{N}]/u.test(text[start - 1] ?? "");
      const base = 1 + (wordStart ? 5 : 0) - i * 0.01;
      if (q === 0) next.set(i, { score: base, indexes: [i] });
      else {
        const entry = preceding && preceding.score + 9 > (bestEarlier?.score ?? -Infinity) ? preceding : bestEarlier;
        if (entry) next.set(i, { score: entry.score + base + (entry === preceding ? 9 : 0), indexes: [...entry.indexes, i] });
      }
    }
    if (next.size === 0) return null;
    previous = next;
  }
  const best = [...previous.values()].sort((a, b) => b.score - a.score)[0]!;
  const ranges: [number, number][] = [];
  for (const index of best.indexes) {
    const [from, to] = spans[index]!;
    const last = ranges.at(-1);
    if (last && last[1] === from) last[1] = to;
    else if (!last || last[1] !== to) ranges.push([from, to]);
  }
  return { score: best.score, ranges };
}

/** Title matches beat path matches. The caller supplies notes in recency order. */
export function fuzzyNotes(notes: readonly string[], query: string, limit = 20): FuzzyHit[] {
  if (limit <= 0) return [];
  const folded = indexed(query.trim()).chars.join("");
  const hits: (FuzzyHit & { order: number })[] = [];
  notes.forEach((path, order) => {
    const title = path.slice(path.lastIndexOf("/") + 1).replace(/\.md$/i, "");
    if (!folded) { hits.push({ path, title, score: 0, ranges: [], order }); return; }
    const titleMatch = match(title, folded);
    const pathMatch = titleMatch ? null : match(path.replace(/\.md$/i, ""), folded);
    if (!titleMatch && !pathMatch) return;
    // atan preserves ranking within a field while reserving separate score bands.
    const score = Math.atan(titleMatch?.score ?? pathMatch!.score) * 100 + (titleMatch ? 1000 : 0);
    hits.push({ path, title, score, ranges: titleMatch?.ranges ?? [], order });
  });
  if (folded) hits.sort((a, b) => b.score - a.score || a.order - b.order);
  return hits.slice(0, limit).map(({ order: _order, ...hit }) => hit);
}
