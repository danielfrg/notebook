<script lang="ts">
  import { Button } from "@ui-sdk/svelte/button";
  import { noteTitle } from "../paths.ts";
  import { groupRecent } from "../recent.ts";
  import { workspace } from "../state/workspace.svelte.ts";

  let { onOpenNote = () => {} }: { onOpenNote?: () => void } = $props();
  const groups = $derived(groupRecent(workspace.listing?.notes ?? []));
</script>

<div class="recent-list">
  {#if groups.length === 0}<p class="file-tree-empty">
      <strong>Empty vault</strong><br />No notes here yet.
    </p>{/if}
  {#each groups as group (group.label)}
    <p class="recent-heading">{group.label}</p>
    {#each group.paths as path (path)}
      <Button
        class="recent-row"
        data-variant="ghost"
        title={path}
        aria-current={workspace.path === path ? "page" : undefined}
        onclick={() => {
          workspace.openNote(path);
          onOpenNote();
        }}
      >
        <span class="recent-title">{noteTitle(path)}</span>
      </Button>
    {/each}
  {/each}
</div>
