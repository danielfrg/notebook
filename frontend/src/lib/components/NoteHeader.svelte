<script lang="ts">
  import { Button } from "@ui-sdk/svelte/button";
  import {
    Menu,
    MenuContent,
    MenuItem,
    MenuItemIcon,
    MenuSeparator,
    MenuTrigger,
  } from "@ui-sdk/svelte/menu";
  import {
    SegmentedControl,
    SegmentedControlIndicator,
    SegmentedControlItem,
  } from "@ui-sdk/svelte/segmented-control";
  import { toast } from "@ui-sdk/svelte/toast";
  import ThemeToggle from "../../design-system/custom/theme-toggle/ThemeToggle.svelte";
  import { colorScheme } from "../color-scheme.ts";
  import { copyToClipboard } from "../clipboard.ts";
  import PanelLeftOpenIcon from "@lucide/svelte/icons/panel-left-open";
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
  const modeOptions = $derived([
    { value: "live", label: narrow ? "Live" : "Live Preview" },
    { value: "source", label: "Source" },
  ]);
  const notePath = $derived(
    workspace.path?.endsWith(".md") ? workspace.path.slice(0, -3) : workspace.path,
  );
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
    if (event.key === "Enter") {
      event.preventDefault();
      titleElement?.blur();
    } else if (event.key === "Escape") {
      event.preventDefault();
      resetTitle();
      titleElement?.blur();
    }
  }

  /** Renames the note to the edited title when the title loses the focus. */
  async function commitTitle(): Promise<void> {
    const path = workspace.path;
    if (!path || !titleElement) return;
    const name = (titleElement.textContent ?? "").replace(/\s+/g, " ").trim();
    titleElement.scrollLeft = 0;
    if (!name || name === noteTitle(path)) {
      resetTitle();
      return;
    }
    try {
      await workspace.renameEntry(path, name);
    } catch (error) {
      resetTitle();
      toast.error(error instanceof Error ? error.message : "Could not rename the note");
    }
  }
  let confirmPath = $state<string | null>(null);

  async function copyPath(): Promise<void> {
    if (!workspace.path) return;
    if (await copyToClipboard(workspace.path)) toast("Path copied");
    else toast.error("Could not copy path");
  }

  onMount(() => {
    const media = window.matchMedia("(max-width: 640px)");
    const update = () => {
      narrow = media.matches;
    };
    media.addEventListener("change", update);
    return () => media.removeEventListener("change", update);
  });
</script>

<header class="note-header">
  <div class="note-header-left">
    {#if collapsed}
      <Button
        data-variant="ghost"
        aria-label="Show sidebar"
        title="Show sidebar"
        onclick={onOpenSidebar}><PanelLeftOpenIcon aria-hidden="true" /></Button
      >
    {/if}
    <div class="note-history">
      <Button
        data-variant="ghost"
        data-size="sm"
        aria-label="Back"
        title="Back (⌘[)"
        disabled={!workspace.canGoBack}
        onclick={() => workspace.goBack()}><ArrowLeftIcon aria-hidden="true" /></Button
      >
      <Button
        data-variant="ghost"
        data-size="sm"
        aria-label="Forward"
        title="Forward (⌘])"
        disabled={!workspace.canGoForward}
        onclick={() => workspace.goForward()}><ArrowRightIcon aria-hidden="true" /></Button
      >
    </div>
    {#if workspace.path}
      <div class="note-location" title={workspace.path}>
        {#if parentPath}
          <span class="note-breadcrumb">{parentPath}</span>
          <ChevronRightIcon class="note-location-divider" aria-hidden="true" />
        {/if}
        <h1
          class="note-title"
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
  </div>
  <div class="note-header-right">
    {#if workspace.path && !workspace.noteError}
      <SegmentedControl
        aria-label="Editor mode"
        bind:value={() => workspace.mode, (value) => workspace.setMode(value as "live" | "source")}
      >
        <SegmentedControlIndicator />
        {#each modeOptions as option (option.value)}
          <SegmentedControlItem value={option.value}>{option.label}</SegmentedControlItem>
        {/each}
      </SegmentedControl>
    {/if}
    {#if workspace.path}
      <Menu>
        <MenuTrigger class="note-actions-trigger" data-variant="ghost" aria-label="Note actions"
          ><EllipsisVerticalIcon aria-hidden="true" /></MenuTrigger
        >
        <MenuContent align="end" aria-label="Note actions">
          <MenuItem onSelect={() => void copyPath()}
            ><MenuItemIcon><CopyIcon /></MenuItemIcon>Copy path</MenuItem
          >
          {#if !workspace.readOnly && !workspace.missingNote}
            <MenuItem onSelect={() => (renamePath = workspace.path)}
              ><MenuItemIcon><PencilLineIcon /></MenuItemIcon>Rename</MenuItem
            >
            <MenuSeparator />
            <MenuItem onSelect={() => (confirmPath = workspace.path)}
              ><MenuItemIcon><Trash2Icon /></MenuItemIcon>Delete</MenuItem
            >
          {/if}
        </MenuContent>
      </Menu>
    {/if}
    <ThemeToggle {colorScheme} />
  </div>
</header>
{#if workspace.path}
  <div class="mobile-note-context"><h1 title={workspace.path}>{notePath}</h1></div>
{/if}

{#if renamePath}
  <EntryDialog
    kind="rename-note"
    initial={noteTitle(renamePath)}
    onsubmit={(name) => workspace.renameEntry(renamePath!, name)}
    onclose={() => (renamePath = null)}
  />
{/if}
{#if confirmPath}
  <ConfirmDialog
    path={confirmPath}
    onconfirm={() => workspace.trashEntry(confirmPath!)}
    onclose={() => (confirmPath = null)}
  />
{/if}
