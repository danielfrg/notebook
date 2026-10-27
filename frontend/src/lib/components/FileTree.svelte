<script lang="ts">
  import ChevronRightIcon from "@lucide/svelte/icons/chevron-right";
  import FileTextIcon from "@lucide/svelte/icons/file-text";
  import FolderIcon from "@lucide/svelte/icons/folder";
  import EllipsisIcon from "@lucide/svelte/icons/ellipsis";
  import PlusIcon from "@lucide/svelte/icons/plus";
  import { Menu, MenuContent, MenuItem, MenuSeparator, MenuTrigger } from "@kenn-io/kit-ui";
  import { uniqueNoteName } from "../entry-names.ts";
  import { baseName, noteTitle, parentFolder } from "../paths.ts";
  import { tabStopPath, visibleRows } from "../tree.ts";
  import { workspace } from "../state/workspace.svelte.ts";
  import EntryDialog from "./EntryDialog.svelte";
  import ConfirmDialog from "./ConfirmDialog.svelte";

  let { onOpenNote = () => {} }: { onOpenNote?: () => void } = $props();

  const rows = $derived(visibleRows(workspace.tree, workspace.expanded));
  let focused = $state<string | null>(null);
  const tabStop = $derived(tabStopPath(rows, focused));
  let tree: HTMLDivElement;
  let contextPath = $state<string | null>(null);
  let dialog = $state<{ kind: "note" | "folder" | "rename-note" | "rename-folder"; folder: string; path: string; initial: string } | null>(null);
  let confirmPath = $state<string | null>(null);

  function newNote(folder: string): void {
    const existing = workspace.listing?.notes.filter((note) => parentFolder(note.path) === folder).map((note) => baseName(note.path)) ?? [];
    dialog = { kind: "note", folder, path: "", initial: uniqueNoteName(existing, "Untitled") };
  }

  function newFolder(folder: string): void {
    dialog = { kind: "folder", folder, path: "", initial: "" };
  }

  function rename(path: string, isNote: boolean): void {
    dialog = { kind: isNote ? "rename-note" : "rename-folder", folder: "", path, initial: isNote ? noteTitle(path) : baseName(path) };
  }

  async function submit(name: string): Promise<void> {
    if (!dialog) return;
    if (dialog.kind === "note") await workspace.createNote(dialog.folder, name);
    else if (dialog.kind === "folder") await workspace.createFolder(dialog.folder, name);
    else await workspace.renameEntry(dialog.path, name);
    if (window.innerWidth < 900 && dialog.kind === "note") onOpenNote();
  }

  function focusRow(index: number): void {
    const row = rows[index];
    if (!row) return;
    focused = row.node.path;
    requestAnimationFrame(() => tree.querySelectorAll<HTMLElement>('[role="treeitem"]')[index]?.focus());
  }

  function onKeydown(event: KeyboardEvent): void {
    const target = event.target as HTMLElement;
    if (target.closest("button")) return;
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

<div class="file-tree">
  {#if !workspace.readOnly}
    <div class="file-tree-tools">
      <Menu>
        <MenuTrigger ariaLabel="New entry" class="file-tree-new"><PlusIcon size={15} aria-hidden="true" /> New</MenuTrigger>
        <MenuContent ariaLabel="New entry">
          <MenuItem onselect={() => newNote("")}>New note</MenuItem>
          <MenuItem onselect={() => newFolder("")}>New folder</MenuItem>
        </MenuContent>
      </Menu>
    </div>
  {/if}
  {#if rows.length === 0}<p class="file-tree-empty"><strong>Empty vault</strong><br />No notes here yet.</p>{/if}
  <div role="tree" aria-label="Files" tabindex="-1" bind:this={tree}>
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
      oncontextmenu={(event) => { if (!workspace.readOnly) { event.preventDefault(); contextPath = node.path; } }}
      onclick={(event) => { if ((event.target as HTMLElement).closest("button")) return; node.kind === "folder" ? workspace.toggleFolder(node.path) : openNote(node.path); }}
    >
      {#if node.kind === "folder"}
        <ChevronRightIcon size={14} class={workspace.expanded.has(node.path) ? "tree-open" : ""} aria-hidden="true" />
        <FolderIcon size={15} aria-hidden="true" />
      {:else}
        <FileTextIcon size={15} aria-hidden="true" />
      {/if}
      <span class="file-tree-name">{node.name}</span>
      {#if !workspace.readOnly}
        <Menu open={contextPath === node.path} onopenchange={(open) => contextPath = open ? node.path : null} align="end">
          <MenuTrigger ariaLabel={`Actions for ${node.name}`} class="file-tree-actions"><EllipsisIcon size={15} aria-hidden="true" /></MenuTrigger>
          <MenuContent ariaLabel={`Actions for ${node.name}`}>
            <MenuItem onselect={() => newNote(node.kind === "folder" ? node.path : parentFolder(node.path))}>New note</MenuItem>
            <MenuItem onselect={() => newFolder(node.kind === "folder" ? node.path : parentFolder(node.path))}>New folder</MenuItem>
            <MenuSeparator />
            <MenuItem onselect={() => rename(node.path, node.kind === "note")}>Rename</MenuItem>
            <MenuItem tone="danger" onselect={() => confirmPath = node.path}>Delete</MenuItem>
          </MenuContent>
        </Menu>
      {/if}
    </div>
  {/each}
  </div>
</div>

{#if dialog}
  <EntryDialog kind={dialog.kind} initial={dialog.initial} onsubmit={submit} onclose={() => dialog = null} />
{/if}
{#if confirmPath}
  <ConfirmDialog path={confirmPath} onconfirm={() => workspace.trashEntry(confirmPath!)} onclose={() => confirmPath = null} />
{/if}
