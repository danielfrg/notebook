<script lang="ts">
  import { Menu, MenuContent, MenuItem, MenuSeparator, MenuTrigger, IconButton, SegmentedControl, SidebarToggle, ThemeToggle, TopBar, copyToClipboard, showFlash } from "@kenn-io/kit-ui";
  import EllipsisVerticalIcon from "@lucide/svelte/icons/ellipsis-vertical";
  import ArrowLeftIcon from "@lucide/svelte/icons/arrow-left";
  import ArrowRightIcon from "@lucide/svelte/icons/arrow-right";
  import PencilLineIcon from "@lucide/svelte/icons/pencil-line";
  import Trash2Icon from "@lucide/svelte/icons/trash-2";
  import CopyIcon from "@lucide/svelte/icons/copy";
  import ChevronRightIcon from "@lucide/svelte/icons/chevron-right";
  import { onMount } from "svelte";
  import { noteTitle, parentFolder } from "../paths.ts";
  import { workspace } from "../state/workspace.svelte.ts";
  import EntryDialog from "./EntryDialog.svelte";
  import ConfirmDialog from "./ConfirmDialog.svelte";

  let { onOpenSidebar, collapsed }: { onOpenSidebar: () => void; collapsed: boolean } = $props();
  let narrow = $state(window.matchMedia("(max-width: 640px)").matches);
  const modeOptions = $derived([{ value: "live", label: narrow ? "Live" : "Live Preview" }, { value: "source", label: "Source" }]);
  const notePath = $derived(workspace.path?.endsWith(".md") ? workspace.path.slice(0, -3) : workspace.path);
  const parentPath = $derived(workspace.path ? parentFolder(workspace.path) : "");
  let renamePath = $state<string | null>(null);
  let titleElement: HTMLHeadingElement | undefined = $state();
  const editable = $derived(!!workspace.path && !workspace.readOnly && !workspace.missingNote);

  // The title is the element's own text so the browser can edit it in place;
  // it is set from the path whenever it is not being edited.
  $effect(() => {
    const title = workspace.path ? noteTitle(workspace.path) : "";
    if (titleElement && document.activeElement !== titleElement) titleElement.textContent = title;
  });

  function resetTitle(): void {
    if (titleElement) titleElement.textContent = workspace.path ? noteTitle(workspace.path) : "";
  }

  function titleKeydown(event: KeyboardEvent): void {
    if (event.key === "Enter") { event.preventDefault(); titleElement?.blur(); }
    else if (event.key === "Escape") { event.preventDefault(); resetTitle(); titleElement?.blur(); }
  }

  /** Renames the note to the edited title when the title loses the focus. */
  async function commitTitle(): Promise<void> {
    const path = workspace.path;
    if (!path || !titleElement) return;
    const name = (titleElement.textContent ?? "").replace(/\s+/g, " ").trim();
    titleElement.scrollLeft = 0;
    if (!name || name === noteTitle(path)) { resetTitle(); return; }
    try {
      await workspace.renameEntry(path, name);
    } catch (error) {
      resetTitle();
      showFlash(error instanceof Error ? error.message : "Could not rename the note", { tone: "danger" });
    }
  }
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

<TopBar class="note-header">
  {#snippet left()}
    {#if collapsed}<SidebarToggle state="collapsed" onclick={onOpenSidebar} />{/if}
    <div class="note-history">
      <IconButton ariaLabel="Back" title="Back (⌘[)" size="sm" disabled={!workspace.canGoBack} onclick={() => workspace.goBack()}><ArrowLeftIcon size={15} strokeWidth={1.5} aria-hidden="true" /></IconButton>
      <IconButton ariaLabel="Forward" title="Forward (⌘])" size="sm" disabled={!workspace.canGoForward} onclick={() => workspace.goForward()}><ArrowRightIcon size={15} strokeWidth={1.5} aria-hidden="true" /></IconButton>
    </div>
    {#if workspace.path}
      <div class="note-location" title={workspace.path}>
        {#if parentPath}
          <span class="note-breadcrumb">{parentPath}</span>
          <ChevronRightIcon class="note-location-divider" size={13} aria-hidden="true" />
        {/if}
        <h1
          class="app-title app-title--with-note note-title"
          bind:this={titleElement}
          contenteditable={editable ? "plaintext-only" : "false"}
          spellcheck="false"
          role={editable ? "textbox" : undefined}
          aria-label={editable ? "Note name" : undefined}
          title={editable ? "Rename note" : undefined}
          onkeydown={titleKeydown}
          onblur={() => void commitTitle()}
        ></h1>
      </div>
    {/if}
  {/snippet}
  {#snippet right()}
    {#if workspace.path && !workspace.noteError}
      <SegmentedControl options={modeOptions} value={workspace.mode} ariaLabel="Editor mode" onchange={(value) => workspace.setMode(value as "live" | "source")} />
    {/if}
    {#if workspace.path}
      <Menu align="end">
        <MenuTrigger ariaLabel="Note actions" class="note-actions-trigger"><EllipsisVerticalIcon size={17} aria-hidden="true" /></MenuTrigger>
        <MenuContent ariaLabel="Note actions">
          <MenuItem onselect={() => void copyPath()}>{#snippet icon()}<CopyIcon aria-hidden="true" />{/snippet}Copy path</MenuItem>
          {#if !workspace.readOnly && !workspace.missingNote}
            <MenuItem onselect={() => renamePath = workspace.path}>{#snippet icon()}<PencilLineIcon aria-hidden="true" />{/snippet}Rename</MenuItem>
            <MenuSeparator />
            <MenuItem onselect={() => confirmPath = workspace.path}>{#snippet icon()}<Trash2Icon aria-hidden="true" />{/snippet}Delete</MenuItem>
          {/if}
        </MenuContent>
      </Menu>
    {/if}
    <ThemeToggle />
  {/snippet}
</TopBar>
{#if workspace.path}
  <div class="mobile-note-context"><h1 title={workspace.path}>{notePath}</h1></div>
{/if}

{#if renamePath}
  <EntryDialog kind="rename-note" initial={noteTitle(renamePath)} onsubmit={(name) => workspace.renameEntry(renamePath!, name)} onclose={() => renamePath = null} />
{/if}
{#if confirmPath}
  <ConfirmDialog path={confirmPath} onconfirm={() => workspace.trashEntry(confirmPath!)} onclose={() => confirmPath = null} />
{/if}
