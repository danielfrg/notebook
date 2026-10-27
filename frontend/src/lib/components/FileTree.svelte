<script lang="ts">
  import ChevronRightIcon from "@lucide/svelte/icons/chevron-right";
  import FileTextIcon from "@lucide/svelte/icons/file-text";
  import FolderIcon from "@lucide/svelte/icons/folder";
  import { tabStopPath, visibleRows } from "../tree.ts";
  import { workspace } from "../state/workspace.svelte.ts";

  let { onOpenNote = () => {} }: { onOpenNote?: () => void } = $props();

  const rows = $derived(visibleRows(workspace.tree, workspace.expanded));
  let focused = $state<string | null>(null);
  const tabStop = $derived(tabStopPath(rows, focused));
  let tree: HTMLDivElement;

  function focusRow(index: number): void {
    const row = rows[index];
    if (!row) return;
    focused = row.node.path;
    requestAnimationFrame(() => tree.querySelectorAll<HTMLElement>('[role="treeitem"]')[index]?.focus());
  }

  function onKeydown(event: KeyboardEvent): void {
    const target = event.target as HTMLElement;
    const item = target.closest<HTMLElement>('[role="treeitem"]');
    if (!item) return;
    const index = Array.from(tree.querySelectorAll('[role="treeitem"]')).indexOf(item);
    const row = rows[index];
    if (!row) return;
    const node = row.node;
    let next = index;
    switch (event.key) {
      case "ArrowDown": next = Math.min(rows.length - 1, index + 1); break;
      case "ArrowUp": next = Math.max(0, index - 1); break;
      case "Home": next = 0; break;
      case "End": next = rows.length - 1; break;
      case "ArrowRight":
        if (node.kind === "folder" && !workspace.expanded.has(node.path)) workspace.toggleFolder(node.path);
        else if (node.kind === "folder" && node.children.length) next = index + 1;
        break;
      case "ArrowLeft":
        if (node.kind === "folder" && workspace.expanded.has(node.path)) workspace.toggleFolder(node.path);
        else if (row.depth > 0) {
          for (let i = index - 1; i >= 0; i--) if (rows[i].depth === row.depth - 1) { next = i; break; }
        }
        break;
      case "Enter":
      case " ":
        if (node.kind === "folder") workspace.toggleFolder(node.path);
        else openNote(node.path);
        break;
      default: return;
    }
    event.preventDefault();
    if (next !== index) focusRow(next);
  }

  function openNote(path: string): void {
    workspace.openNote(path);
    onOpenNote();
  }
</script>

<div class="file-tree" role="tree" aria-label="Files" tabindex="-1" bind:this={tree}>
  {#each rows as row (row.node.path)}
    {@const node = row.node}
    <div
      class="file-tree-row"
      class:is-current={node.kind === "note" && workspace.path === node.path}
      role="treeitem"
      aria-level={row.depth + 1}
      aria-expanded={node.kind === "folder" ? workspace.expanded.has(node.path) : undefined}
      aria-selected={node.kind === "note" ? workspace.path === node.path : undefined}
      tabindex={tabStop === node.path ? 0 : -1}
      style={`--depth: ${row.depth}`}
      onfocus={() => focused = node.path}
      onkeydown={onKeydown}
      onclick={() => node.kind === "folder" ? workspace.toggleFolder(node.path) : openNote(node.path)}
    >
      {#if node.kind === "folder"}
        <ChevronRightIcon size={14} class={workspace.expanded.has(node.path) ? "tree-open" : ""} aria-hidden="true" />
        <FolderIcon size={15} aria-hidden="true" />
      {:else}
        <FileTextIcon size={15} aria-hidden="true" />
      {/if}
      <span class="file-tree-name">{node.name}</span>
    </div>
  {/each}
</div>
