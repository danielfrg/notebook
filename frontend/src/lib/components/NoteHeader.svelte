<script lang="ts">
  import { Menu, MenuContent, MenuItem, MenuSeparator, MenuTrigger, SegmentedControl, SidebarToggle, ThemeToggle, TopBar, copyToClipboard, showFlash } from "@kenn-io/kit-ui";
  import EllipsisIcon from "@lucide/svelte/icons/ellipsis";
  import { onMount } from "svelte";
  import { noteTitle } from "../paths.ts";
  import { workspace } from "../state/workspace.svelte.ts";
  import EntryDialog from "./EntryDialog.svelte";
  import ConfirmDialog from "./ConfirmDialog.svelte";
  import StatusPill from "./StatusPill.svelte";

  let { onOpenSidebar, collapsed }: { onOpenSidebar: () => void; collapsed: boolean } = $props();
  let narrow = $state(window.matchMedia("(max-width: 640px)").matches);
  const modeOptions = $derived([{ value: "live", label: narrow ? "Live" : "Live Preview" }, { value: "source", label: "Source" }]);
  const notePath = $derived(workspace.path?.endsWith(".md") ? workspace.path.slice(0, -3) : workspace.path);
  let renamePath = $state<string | null>(null);
  let confirmPath = $state<string | null>(null);

  async function copyPath(): Promise<void> {
    if (!workspace.path) return;
    showFlash(await copyToClipboard(workspace.path) ? "Path copied" : "Could not copy path", { tone: "neutral" });
  }

  onMount(() => {
    const media = window.matchMedia("(max-width: 640px)");
    const update = () => { narrow = media.matches; };
    media.addEventListener("change", update);
    return () => media.removeEventListener("change", update);
  });
</script>

<TopBar>
  {#snippet left()}
    {#if collapsed}<SidebarToggle state="collapsed" onclick={onOpenSidebar} />{/if}
    <h1 class="app-title" class:app-title--with-note={!!workspace.path}>Notebook</h1>
    {#if workspace.path}<span class="note-breadcrumb" title={workspace.path}>{workspace.vault} / {notePath}</span>{/if}
  {/snippet}
  {#snippet right()}
    {#if workspace.path && !workspace.noteError}
      <SegmentedControl options={modeOptions} value={workspace.mode} ariaLabel="Editor mode" onchange={(value) => workspace.setMode(value as "live" | "source")} />
      <StatusPill />
    {/if}
    {#if workspace.path}
      <Menu align="end">
        <MenuTrigger ariaLabel="Note actions" class="note-actions-trigger"><EllipsisIcon size={17} aria-hidden="true" /></MenuTrigger>
        <MenuContent ariaLabel="Note actions">
          {#if !workspace.readOnly && !workspace.missingNote}
            <MenuItem onselect={() => renamePath = workspace.path}>Rename</MenuItem>
            <MenuItem tone="danger" onselect={() => confirmPath = workspace.path}>Delete</MenuItem>
            <MenuSeparator />
          {/if}
          <MenuItem onselect={() => void copyPath()}>Copy path</MenuItem>
        </MenuContent>
      </Menu>
    {/if}
    <ThemeToggle />
  {/snippet}
</TopBar>
{#if workspace.path}
  <div class="mobile-note-context"><h1 title={workspace.path}>{workspace.vault} / {notePath}</h1></div>
{/if}

{#if renamePath}
  <EntryDialog kind="rename-note" initial={noteTitle(renamePath)} onsubmit={(name) => workspace.renameEntry(renamePath!, name)} onclose={() => renamePath = null} />
{/if}
{#if confirmPath}
  <ConfirmDialog path={confirmPath} onconfirm={() => workspace.trashEntry(confirmPath!)} onclose={() => confirmPath = null} />
{/if}
