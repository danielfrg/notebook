<script lang="ts">
  import type { SearchHit } from "../api.ts";
  import { termParts } from "../search-terms.ts";

  let { hits, query, loading, error, onopen }: {
    hits: SearchHit[];
    query: string;
    loading: boolean;
    error: string | null;
    onopen: (hit: SearchHit) => void;
  } = $props();

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
              {#each termParts(snippet.text, query) as part}{#if part.match}<mark>{part.text}</mark>{:else}{part.text}{/if}{/each}
            </span>
          {/each}
        </button>
      {/each}
    </div>
  {/if}
</div>
