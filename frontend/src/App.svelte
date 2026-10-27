<script lang="ts">
  import { CollapsibleSidebar, EmptyState, Spinner, SidebarToggle, Button, FlashBanner } from "@kenn-io/kit-ui";
  import NotebookTextIcon from "@lucide/svelte/icons/notebook-text";
  import { onMount, tick } from "svelte";
  import Editor from "./lib/editor/Editor.svelte";
  import { livePreview, previewContext } from "./lib/editor/livePreview.ts";
  import FileTree from "./lib/components/FileTree.svelte";
  import ConflictBanner from "./lib/components/ConflictBanner.svelte";
  import EntryDialog from "./lib/components/EntryDialog.svelte";
  import NoteHeader from "./lib/components/NoteHeader.svelte";
  import Toolbar from "./lib/components/Toolbar.svelte";
  import VaultSwitcher from "./lib/components/VaultSwitcher.svelte";
  import { workspace } from "./lib/state/workspace.svelte.ts";
  import { noteTitle } from "./lib/paths.ts";

  let collapsed = $state(window.innerWidth < 900);
  let sidebarWidth = $state(280);
  let editor = $state<Editor>();
  let saveAsNew = $state(false);

  $effect(() => {
    if (workspace.focusRequest > 0) void tick().then(() => editor?.focus());
  });
  const extensions = $derived([
    previewContext.of({
      notes: workspace.listing?.notes.map((note) => note.path) ?? [],
      assets: workspace.listing?.assets.map((asset) => asset.path) ?? [],
      path: workspace.path ?? "",
      assetUrl: (path: string) => workspace.assetUrl(path),
    }),
    livePreview(), // Includes taskToggleExtension for rendered checkboxes.
  ]);

  onMount(() => {
    try {
      const width = Number(localStorage.getItem("notebook-sidebar"));
      if (Number.isFinite(width) && width >= 200 && width <= 600) sidebarWidth = width;
    } catch { /* Storage may be disabled. */ }
    void workspace.init();
    const visibility = () => { if (document.visibilityState === "hidden") void workspace.saveNow(); };
    const beforeUnload = (event: BeforeUnloadEvent) => {
      if (!workspace.dirty) return;
      event.preventDefault();
      event.returnValue = "";
    };
    const shortcuts = (event: KeyboardEvent) => {
      if (!(event.metaKey || event.ctrlKey) || event.altKey || event.shiftKey) return;
      if (event.key.toLowerCase() === "e" && workspace.path) {
        event.preventDefault();
        workspace.setMode(workspace.mode === "live" ? "source" : "live");
      } else if (event.key.toLowerCase() === "s") {
        event.preventDefault();
        void workspace.saveNow();
      }
    };
    document.addEventListener("visibilitychange", visibility);
    window.addEventListener("beforeunload", beforeUnload);
    window.addEventListener("keydown", shortcuts);
    return () => {
      void workspace.saveNow();
      workspace.dispose();
      document.removeEventListener("visibilitychange", visibility);
      window.removeEventListener("beforeunload", beforeUnload);
      window.removeEventListener("keydown", shortcuts);
    };
  });

  function resize(width: number): void {
    sidebarWidth = width;
    try { localStorage.setItem("notebook-sidebar", String(width)); } catch { /* Storage may be disabled. */ }
  }

  function blurEditor(event: FocusEvent): void {
    const current = event.currentTarget as HTMLElement;
    if (!event.relatedTarget || !current.contains(event.relatedTarget as Node)) void workspace.saveNow();
  }
</script>

<div class="app-shell">
  <CollapsibleSidebar
    isCollapsed={collapsed}
    overlayOnNarrow
    mainOverflow="hidden"
    {sidebarWidth}
    onSidebarResize={resize}
    onExpand={() => (collapsed = false)}
  >
    {#snippet sidebar()}
      <header class="app-sidebar-header">
        <VaultSwitcher />
        <SidebarToggle onclick={() => (collapsed = true)} class="kit-sidebar-toggle--push" />
      </header>
      {#if workspace.loading}
        <div class="sidebar-message"><Spinner label="Loading vault" /></div>
      {:else if workspace.error}
        <div class="sidebar-message" role="alert">
          <p>{workspace.error}</p>
          <Button label="Retry" onclick={() => void workspace.retry()} />
        </div>
      {:else}
        <FileTree onOpenNote={() => { if (window.innerWidth < 900) collapsed = true; }} />
      {/if}
    {/snippet}

    <NoteHeader {collapsed} onOpenSidebar={() => (collapsed = false)} />
    <main class="app-content">
      {#if workspace.loading}
        <div class="content-message"><Spinner label="Loading vault" /></div>
      {:else if workspace.error}
        <div class="content-message" role="alert"><p>{workspace.error}</p><Button label="Retry" onclick={() => void workspace.retry()} /></div>
      {:else if workspace.noteLoading}
        <div class="content-message"><Spinner label="Loading note" /></div>
      {:else if workspace.noteError}
        <div class="content-message" role="alert">{workspace.noteError}</div>
      {:else if workspace.path}
        {#if workspace.missingNote || workspace.externalConflict || workspace.saveState === "conflict"}
          <ConflictBanner
            missing={workspace.missingNote}
            readOnly={workspace.readOnly}
            onload={() => workspace.reloadFromDisk()}
            onoverwrite={() => workspace.overwriteDisk()}
            onsaveasnew={() => saveAsNew = true}
          />
        {/if}
        <Toolbar run={(command) => editor?.run(command)} disabled={workspace.readOnly || !!workspace.lineSeparatorIssue} />
        {#if workspace.lineSeparatorIssue}<p class="note-reason" role="status">{workspace.lineSeparatorIssue}</p>{/if}
        <div class="editor-host" onfocusout={blurEditor}>
          <Editor
            bind:this={editor}
            value={workspace.value}
            mode={workspace.mode}
            readOnly={workspace.readOnly || !!workspace.lineSeparatorIssue}
            {extensions}
            ariaLabel={workspace.path}
            onchange={(value) => workspace.edit(value)}
            onsave={() => void workspace.saveNow()}
            onlineseparator={(separator) => workspace.setLineSeparator(separator)}
          />
        </div>
      {:else}
        <EmptyState title="No note open">
          {#snippet icon()}<NotebookTextIcon size={28} strokeWidth={1.5} aria-hidden="true" />{/snippet}
        </EmptyState>
      {/if}
    </main>
  </CollapsibleSidebar>
  <FlashBanner />
</div>

{#if saveAsNew && workspace.path}
  <EntryDialog kind="save-as-new" initial={noteTitle(workspace.path)} onsubmit={(name) => workspace.saveMissingAsNew(name)} onclose={() => saveAsNew = false} />
{/if}
