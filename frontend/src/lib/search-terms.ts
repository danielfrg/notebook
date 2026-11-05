/**
 * Splits text into the parts that match the terms of a full-text query and
 * the parts between, for highlighting. Terms are words, or "quoted phrases";
 * matching ignores case and accents, as the server's search does.
 */
export function termParts(text: string, query: string): { text: string; match: boolean }[] {
  const terms = [...query.matchAll(/"([^"]+)"|(\S+)/g)]
    .map((match) => match[1] ?? match[2])
    .filter(Boolean);
  if (!terms.length) return [{ text, match: false }];
  const folded: string[] = [];
  const offsets: number[] = [];
  for (let i = 0; i < text.length;) {
    const char = String.fromCodePoint(text.codePointAt(i)!);
    for (const part of char.normalize("NFD").replace(/\p{M}/gu, "").toLocaleLowerCase()) {
      folded.push(part);
      offsets.push(i);
    }
    i += char.length;
  }
  const mask = new Array<boolean>(text.length).fill(false);
  const content = folded.join("");
  for (const term of terms) {
    const needle = term.normalize("NFD").replace(/\p{M}/gu, "").toLocaleLowerCase();
    if (!needle) continue;
    for (let at = content.indexOf(needle); at >= 0; at = content.indexOf(needle, at + 1)) {
      const start = offsets[at]!;
      const end = offsets[at + needle.length] ?? text.length;
      for (let i = start; i < end; i++) mask[i] = true;
    }
  }
  const output: { text: string; match: boolean }[] = [];
  for (let i = 0; i < text.length;) {
    const start = i;
    const match = mask[i];
    while (i < text.length && mask[i] === match) i++;
    output.push({ text: text.slice(start, i), match });
  }
  return output;
}
