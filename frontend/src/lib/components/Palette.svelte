<script lang="ts" module>
  export type PaletteAction = { id: string; label: string; combo?: string; disabled?: boolean };
</script>

<script lang="ts">
  /*
   * The search palette (Mod-k): note names by fuzzy match, note contents by
   * full-text search, and commands, in one list. Escape always closes it.
   */
  import {
    Command,
    CommandEmpty,
    CommandGroup,
    CommandGroupLabel,
    CommandInput,
    CommandItem,
    CommandItemIcon,
    CommandList,
  } from "@ui-sdk/svelte/command";
  import { Dialog, DialogBackdrop, DialogContent, DialogTitle } from "@ui-sdk/svelte/dialog";
  import SearchIcon from "@lucide/svelte/icons/search";
  import Kbd from "./Kbd.svelte";
  import { appShortcuts, formatShortcutKeys } from "../shortcuts.ts";
  import FileTextIcon from "@lucide/svelte/icons/file-text";
  import TerminalIcon from "@lucide/svelte/icons/square-terminal";
  import { onMount } from "svelte";
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
  let hits: SearchHit[] = $state([]);
  let searching = $state(false);
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
  onMount(() => {
    const popScope = appShortcuts.pushScope("notebook-search-palette");
    document.addEventListener("keydown", escape, true);
    return () => {
      popScope();
      document.removeEventListener("keydown", escape, true);
    };
  });

  const value = (row: Row): string =>
    row.kind === "note"
      ? `note:${row.path}`
      : row.kind === "content"
        ? `content:${row.hit.path}`
        : `action:${row.action.id}`;

  function run(id: string): void {
    const row = rows.find((candidate) => value(candidate) === id);
    if (!row) return;
    if (row.kind === "note") onopen(row.path);
    else if (row.kind === "content") onhit(row.hit);
    else if (!row.action.disabled) onaction(row.action.id);
  }

  /** Escape closes, even with text typed: the field would otherwise take it to clear itself. */
  function escape(event: KeyboardEvent): void {
    if (event.key !== "Escape") return;
    event.preventDefault();
    event.stopPropagation();
    onclose();
  }
</script>

<!-- The parent owns the open state: every close request goes to onclose and the parent unmounts. -->
<Dialog
  open
  onOpenChange={(open, details) => {
    if (open) return;
    details.cancel();
    onclose();
  }}
>
  <DialogBackdrop />
  <DialogContent class="palette">
    <DialogTitle class="sr-only">Search</DialogTitle>
    <Command filter={false} bind:query onAction={run}>
      <div data-slot="row" class="palette-search">
        <SearchIcon aria-hidden="true" />
        <CommandInput aria-label="Search" placeholder="Search notes and commands…" />
      </div>
      <CommandEmpty>{searching ? "Searching…" : "No results"}</CommandEmpty>
      <CommandList class="palette-list">
        {#each sections as section (section.label)}
          <CommandGroup>
            <CommandGroupLabel>{section.label}</CommandGroupLabel>
            {#each section.rows as row (value(row))}
              <CommandItem
                value={value(row)}
                disabled={row.kind === "action" && row.action.disabled}
              >
                {#if row.kind === "note"}
                  <CommandItemIcon><FileTextIcon /></CommandItemIcon>
                  <span class="palette-title"
                    >{#each titleSegments(row.title, row.ranges) as part}{#if part.match}<mark
                          >{part.text}</mark
                        >{:else}{part.text}{/if}{/each}</span
                  >
                  {#if parentFolder(row.path)}<span class="palette-path"
                      >{parentFolder(row.path)}</span
                    >{/if}
                {:else if row.kind === "content"}
                  <CommandItemIcon><FileTextIcon /></CommandItemIcon>
                  <span class="palette-body">
                    <span class="palette-head">
                      <span class="palette-title">{row.hit.title}</span>
                      {#if parentFolder(row.hit.path)}<span class="palette-path"
                          >{parentFolder(row.hit.path)}</span
                        >{/if}
                    </span>
                    {#each row.hit.snippets.slice(0, 3) as snippet}
                      <span class="palette-snippet"
                        >{#each termParts(snippet.text, query) as part}{#if part.match}<mark
                              >{part.text}</mark
                            >{:else}{part.text}{/if}{/each}</span
                      >
                    {/each}
                  </span>
                {:else}
                  <CommandItemIcon><TerminalIcon /></CommandItemIcon>
                  <span class="palette-title">{row.action.label}</span>
                  {#if row.action.combo}<Kbd keys={formatShortcutKeys(row.action.combo)} />{/if}
                {/if}
              </CommandItem>
            {/each}
          </CommandGroup>
        {/each}
      </CommandList>
    </Command>
    <div class="palette-hint">
      <Kbd keys={["↑", "↓"]} /> Navigate <Kbd keys={["↵"]} /> Open <Kbd keys={["Esc"]} /> Close
    </div>
  </DialogContent>
</Dialog>
