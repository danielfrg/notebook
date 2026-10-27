<script lang="ts">
  import type { SearchHit } from "../api.ts";

  let { hits, query, loading, error, onopen }: {
    hits: SearchHit[];
    query: string;
    loading: boolean;
    error: string | null;
    onopen: (hit: SearchHit) => void;
  } = $props();

  function parts(text: string): { text: string; match: boolean }[] {
    const terms = [...query.matchAll(/"([^"]+)"|(\S+)/g)].map((match) => match[1] ?? match[2]).filter(Boolean);
    if (!terms.length) return [{ text, match: false }];
    const folded: string[] = [];
    const offsets: number[] = [];
    for (let i = 0; i < text.length;) {
      const char = String.fromCodePoint(text.codePointAt(i)!)!;
      for (const part of char.normalize("NFD").replace(/\p{M}/gu, "").toLocaleLowerCase()) {
        folded.push(part);
        offsets.push(i);
      }
      i += char.length;
    }
    const mask = new Array(text.length).fill(false);
    const content = folded.join("");
    for (const term of terms) {
      const needle = term.normalize("NFD").replace(/\p{M}/gu, "").toLocaleLowerCase();
      if (!needle) continue;
      for (let at = content.indexOf(needle); at >= 0; at = content.indexOf(needle, at + 1)) {
        const start = offsets[at]!;
        const end = (offsets[at + needle.length] ?? text.length);
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
</script>

<div class="search-results" role="region" aria-label="Search results">
  {#if loading}<p class="search-results-message" role="status">Searching…</p>
  {:else if error}<p class="search-results-message" role="alert">{error}</p>
  {:else if hits.length === 0}<p class="search-results-message">No results</p>
  {:else}
    <div class="search-results-list">
      {#each hits as hit (hit.path)}
        <button type="button" class="search-result" onclick={() => onopen(hit)}>
          <strong class="search-result-title">{hit.title}</strong>
          <span class="search-result-path">{hit.path}</span>
          {#each hit.snippets.slice(0, 3) as snippet}
            <span class="search-result-snippet"><span class="search-result-line">{snippet.line}</span>
              {#each parts(snippet.text) as part}{#if part.match}<mark>{part.text}</mark>{:else}{part.text}{/if}{/each}
            </span>
          {/each}
        </button>
      {/each}
    </div>
  {/if}
</div>
