<script lang="ts">
  import { KbdBadge, formatShortcutKeys } from "@kenn-io/kit-ui";
  import FilePlusIcon from "@lucide/svelte/icons/file-plus";
  import FileTextIcon from "@lucide/svelte/icons/file-text";
  import SearchIcon from "@lucide/svelte/icons/search";
  import { noteTitle, parentFolder } from "../paths.ts";

  let { recent, readOnly, onnew, onsearch, onopen }: {
    recent: readonly string[];
    readOnly: boolean;
    onnew: () => void;
    onsearch: () => void;
    onopen: (path: string) => void;
  } = $props();
  let options: HTMLDivElement;

  /** Arrow keys move through the options, as in a menu. */
  function keydown(event: KeyboardEvent): void {
    const step = event.key === "ArrowDown" ? 1 : event.key === "ArrowUp" ? -1 : 0;
    if (!step) return;
    const items = [...options.querySelectorAll<HTMLElement>("button")];
    const at = items.indexOf(document.activeElement as HTMLElement);
    items[(at + step + items.length) % items.length]?.focus();
    event.preventDefault();
  }
</script>

<div class="start-page">
  <!-- svelte-ignore a11y_no_noninteractive_element_interactions -->
  <div class="start-options" role="group" aria-label="Start" bind:this={options} onkeydown={keydown}>
    {#if !readOnly}
      <button type="button" class="start-option" onclick={onnew}><FilePlusIcon size={16} aria-hidden="true" /><span>New note</span></button>
    {/if}
    <button type="button" class="start-option" onclick={onsearch}>
      <SearchIcon size={16} aria-hidden="true" /><span>Search notes</span>
      <KbdBadge keys={formatShortcutKeys("mod+k")} />
    </button>
    {#if recent.length > 0}
      <p class="start-heading">Recent</p>
      {#each recent as path (path)}
        <button type="button" class="start-option" onclick={() => onopen(path)}>
          <FileTextIcon size={16} aria-hidden="true" /><span>{noteTitle(path)}</span>
          {#if parentFolder(path)}<span class="start-folder">{parentFolder(path)}</span>{/if}
        </button>
      {/each}
    {/if}
  </div>
</div>
