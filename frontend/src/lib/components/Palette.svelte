<script lang="ts">
  import { KbdBadge, SearchInput, appShortcuts, trapFocus } from "@kenn-io/kit-ui";
  import { onMount } from "svelte";
  import { fuzzyNotes } from "../fuzzy.ts";

  let { notes, onopen, onclose }: {
    notes: readonly string[];
    onopen: (path: string) => void;
    onclose: () => void;
  } = $props();

  let query = $state("");
  let selected = $state(0);
  const hits = $derived(fuzzyNotes(notes, query, 40));
  $effect(() => { query; selected = 0; });
  onMount(() => appShortcuts.pushScope("notebook-quick-switcher"));

  function choose(index: number): void {
    const hit = hits[index];
    if (hit) onopen(hit.path);
  }

  function keydown(event: KeyboardEvent): void {
    if (event.key === "ArrowDown") { event.preventDefault(); selected = Math.min(hits.length - 1, selected + 1); }
    else if (event.key === "ArrowUp") { event.preventDefault(); selected = Math.max(0, selected - 1); }
    else if (event.key === "Enter") { event.preventDefault(); choose(selected); }
    else if (event.key === "Escape") { event.preventDefault(); onclose(); }
  }
</script>

<div class="quick-palette-overlay" role="presentation" onclick={(event) => { if (event.target === event.currentTarget) onclose(); }}>
  <div class="quick-palette" role="dialog" aria-modal="true" aria-label="Quick switcher" tabindex="-1" onkeydown={keydown} {@attach trapFocus}>
    <div class="quick-palette-search"><SearchInput bind:value={query} autofocus block placeholder="Open a note…" ariaLabel="Find a note" role="combobox" ariaExpanded={true} ariaControls="quick-notes" ariaActivedescendant={hits[selected] ? `quick-note-${selected}` : undefined} /></div>
    <div class="quick-palette-list" role="listbox" aria-label="Notes" id="quick-notes">
      {#if hits.length === 0}<p class="quick-palette-empty">No matching notes</p>{/if}
      {#each hits as hit, index (hit.path)}
        <button type="button" class="quick-palette-row" class:is-selected={index === selected} role="option" id={`quick-note-${index}`} aria-selected={index === selected} onclick={() => choose(index)}>
          <span class="quick-palette-title">
            {#each hit.ranges as range, rangeIndex}
              {#if rangeIndex === 0}{hit.title.slice(0, range[0])}{:else}{hit.title.slice(hit.ranges[rangeIndex - 1]![1], range[0])}{/if}<mark>{hit.title.slice(range[0], range[1])}</mark>
            {/each}
            {hit.title.slice(hit.ranges.at(-1)?.[1] ?? 0)}
          </span>
          <span class="quick-palette-path">{hit.path}</span>
        </button>
      {/each}
    </div>
    <div class="quick-palette-hint"><KbdBadge keys={["↑", "↓"]} /> to select <KbdBadge keys={["↵"]} /> to open</div>
  </div>
</div>
