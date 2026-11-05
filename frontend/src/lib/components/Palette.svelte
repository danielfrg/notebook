<script lang="ts" module>
  export type PaletteAction = { id: string; label: string; combo?: string; disabled?: boolean };
</script>

<script lang="ts">
  /*
   * The search palette (Mod-k): note names by fuzzy match, note contents by
   * full-text search, and commands, in one list. Escape always closes it.
   */
  import {
    KbdBadge,
    SearchInput,
    appShortcuts,
    formatShortcutKeys,
    trapFocus,
  } from "@kenn-io/kit-ui";
  import FileTextIcon from "@lucide/svelte/icons/file-text";
  import TerminalIcon from "@lucide/svelte/icons/square-terminal";
  import { onMount, tick } from "svelte";
  import type { SearchHit } from "../api.ts";
  import { fuzzyNotes, titleSegments } from "../fuzzy.ts";
  import { parentFolder } from "../paths.ts";
  import { termParts } from "../search-terms.ts";

  let {
    notes,
    actions,
    search,
    onopen,
    onhit,
    onaction,
    onclose,
  }: {
    notes: readonly string[];
    actions: readonly PaletteAction[];
    search: (query: string) => Promise<SearchHit[]>;
    onopen: (path: string) => void;
    onhit: (hit: SearchHit) => void;
    onaction: (id: string) => void;
    onclose: () => void;
  } = $props();

  type Row =
    | { kind: "note"; path: string; title: string; ranges: [number, number][] }
    | { kind: "content"; hit: SearchHit }
    | { kind: "action"; action: PaletteAction };

  let query = $state("");
  let selected = $state(0);
  let hits: SearchHit[] = $state([]);
  let searching = $state(false);
  let list: HTMLDivElement;
  let sequence = 0;

  const noteRows = $derived(
    fuzzyNotes(notes, query, query.trim() ? 8 : 6).map((hit): Row => ({
      kind: "note",
      path: hit.path,
      title: hit.title,
      ranges: hit.ranges,
    })),
  );
  const contentRows = $derived(hits.map((hit): Row => ({ kind: "content", hit })));
  const actionRows = $derived(
    actions
      .filter(
        (action) =>
          !query.trim() || action.label.toLowerCase().includes(query.trim().toLowerCase()),
      )
      .map((action): Row => ({ kind: "action", action })),
  );
  const sections = $derived(
    [
      { label: query.trim() ? "Notes" : "Recent", rows: noteRows },
      { label: "Content", rows: contentRows },
      { label: "Commands", rows: actionRows },
    ].filter((section) => section.rows.length > 0),
  );
  const rows = $derived(sections.flatMap((section) => section.rows));
  const offsets = $derived(
    sections.map((_, index) =>
      sections.slice(0, index).reduce((sum, section) => sum + section.rows.length, 0),
    ),
  );

  $effect(() => {
    const text = query.trim();
    selected = 0;
    const current = ++sequence;
    if (!text) {
      hits = [];
      searching = false;
      return;
    }
    searching = true;
    const timer = setTimeout(() => {
      search(text)
        .then((result) => {
          if (current === sequence) hits = result;
        })
        .catch(() => {
          if (current === sequence) hits = [];
        })
        .finally(() => {
          if (current === sequence) searching = false;
        });
    }, 150);
    return () => clearTimeout(timer);
  });
  $effect(() => {
    const index = selected;
    void tick().then(() =>
      list
        ?.querySelector<HTMLElement>(`#palette-row-${index}`)
        ?.scrollIntoView({ block: "nearest" }),
    );
  });
  onMount(() => appShortcuts.pushScope("notebook-search-palette"));

  function choose(index: number): void {
    const row = rows[index];
    if (!row) return;
    if (row.kind === "note") onopen(row.path);
    else if (row.kind === "content") onhit(row.hit);
    else if (!row.action.disabled) onaction(row.action.id);
  }

  function keydown(event: KeyboardEvent): void {
    if (event.key === "ArrowDown") {
      event.preventDefault();
      selected = Math.min(rows.length - 1, selected + 1);
    } else if (event.key === "ArrowUp") {
      event.preventDefault();
      selected = Math.max(0, selected - 1);
    } else if (event.key === "Enter") {
      event.preventDefault();
      choose(selected);
    }
  }

  /** Escape closes, even with text typed: the field would otherwise take it to clear itself. */
  function escape(event: KeyboardEvent): void {
    if (event.key !== "Escape") return;
    event.preventDefault();
    event.stopPropagation();
    onclose();
  }
</script>

<div
  class="quick-palette-overlay"
  role="presentation"
  onclick={(event) => {
    if (event.target === event.currentTarget) onclose();
  }}
>
  <div
    class="quick-palette"
    role="dialog"
    aria-modal="true"
    aria-label="Search"
    tabindex="-1"
    onkeydown={keydown}
    onkeydowncapture={escape}
    {@attach trapFocus}
  >
    <div class="quick-palette-search">
      <SearchInput
        bind:value={query}
        autofocus
        block
        placeholder="Search notes and commands…"
        ariaLabel="Search"
        role="combobox"
        ariaExpanded={true}
        ariaControls="palette-rows"
        ariaActivedescendant={rows[selected] ? `palette-row-${selected}` : undefined}
      />
    </div>
    <div
      class="quick-palette-list"
      role="listbox"
      aria-label="Results"
      id="palette-rows"
      bind:this={list}
    >
      {#if rows.length === 0}<p class="quick-palette-empty">
          {searching ? "Searching…" : "No results"}
        </p>{/if}
      {#each sections as section, sectionIndex (section.label)}
        <p class="quick-palette-section" role="presentation">{section.label}</p>
        {#each section.rows as row, rowIndex}
          {@const index = offsets[sectionIndex] + rowIndex}
          <button
            type="button"
            class="quick-palette-row"
            class:is-selected={index === selected}
            class:is-disabled={row.kind === "action" && row.action.disabled}
            role="option"
            id={`palette-row-${index}`}
            aria-selected={index === selected}
            aria-disabled={row.kind === "action" && row.action.disabled ? "true" : undefined}
            tabindex="-1"
            onpointerenter={() => (selected = index)}
            onclick={() => choose(index)}
          >
            {#if row.kind === "note"}
              <FileTextIcon size={15} strokeWidth={1.5} aria-hidden="true" />
              <span class="quick-palette-title"
                >{#each titleSegments(row.title, row.ranges) as part}{#if part.match}<mark
                      >{part.text}</mark
                    >{:else}{part.text}{/if}{/each}</span
              >
              {#if parentFolder(row.path)}<span class="quick-palette-path"
                  >{parentFolder(row.path)}</span
                >{/if}
            {:else if row.kind === "content"}
              <FileTextIcon size={15} strokeWidth={1.5} aria-hidden="true" />
              <span class="quick-palette-body">
                <span class="quick-palette-head">
                  <span class="quick-palette-title">{row.hit.title}</span>
                  {#if parentFolder(row.hit.path)}<span class="quick-palette-path"
                      >{parentFolder(row.hit.path)}</span
                    >{/if}
                </span>
                {#each row.hit.snippets.slice(0, 3) as snippet}
                  <span class="quick-palette-snippet"
                    >{#each termParts(snippet.text, query) as part}{#if part.match}<mark
                          >{part.text}</mark
                        >{:else}{part.text}{/if}{/each}</span
                  >
                {/each}
              </span>
            {:else}
              <TerminalIcon size={15} strokeWidth={1.5} aria-hidden="true" />
              <span class="quick-palette-title">{row.action.label}</span>
              {#if row.action.combo}<KbdBadge keys={formatShortcutKeys(row.action.combo)} />{/if}
            {/if}
          </button>
        {/each}
      {/each}
    </div>
    <div class="quick-palette-hint">
      <KbdBadge keys={["↑", "↓"]} /> Navigate <KbdBadge keys={["↵"]} /> Open <KbdBadge
        keys={["esc"]}
      /> Close
    </div>
  </div>
</div>
