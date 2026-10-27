<script lang="ts">
  import { CollapsibleSidebar, EmptyState, Spinner, SidebarToggle, Button, FlashBanner, SearchInput, CommandPalette, appShortcuts, initShortcuts, getThemeMode, setThemeMode, type PaletteCommand } from "@kenn-io/kit-ui";
  import NotebookTextIcon from "@lucide/svelte/icons/notebook-text";
  import { onMount, tick } from "svelte";
  import Editor from "./lib/editor/Editor.svelte";
  import { livePreview, previewContext } from "./lib/editor/livePreview.ts";
  import FileTree from "./lib/components/FileTree.svelte";
  import ConflictBanner from "./lib/components/ConflictBanner.svelte";
  import DiscardDialog from "./lib/components/DiscardDialog.svelte";
  import EntryDialog from "./lib/components/EntryDialog.svelte";
  import NoteHeader from "./lib/components/NoteHeader.svelte";
  import Palette from "./lib/components/Palette.svelte";
  import SearchResults from "./lib/components/SearchResults.svelte";
  import Toolbar from "./lib/components/Toolbar.svelte";
  import VaultSwitcher from "./lib/components/VaultSwitcher.svelte";
  import { workspace } from "./lib/state/workspace.svelte.ts";
  import { noteTitle, parentFolder } from "./lib/paths.ts";
  import { uniqueNoteName } from "./lib/entry-names.ts";
  import { parseWikilink, resolveNote } from "./lib/wikilinks.ts";

  let collapsed = $state(window.innerWidth < 900);
  let sidebarWidth = $state(280);
  let editor = $state<Editor>();
  let saveAsNew = $state(false);
  let discardConfirm = $state(false);
  let quickOpen = $state(false);
  let commandsOpen = $state(false);
  let vaultOpen = $state(false);
  let searchInput: HTMLInputElement | undefined = $state();
  let entryDialog: { kind: "note" | "folder"; folder: string; initial: string } | null = $state(null);
  const notePaths = $derived((workspace.listing?.notes ?? [])
    .toSorted((a, b) => b.modified.localeCompare(a.modified))
    .map((entry) => entry.path));
  const commands = $derived<PaletteCommand[]>([
    { id: "new-note", label: "New note", disabled: workspace.readOnly },
    { id: "new-folder", label: "New folder", disabled: workspace.readOnly },
    { id: "toggle-mode", label: "Toggle Live / Source", combo: "mod+e", disabled: !workspace.path },
    { id: "toggle-theme", label: "Toggle theme" },
    { id: "switch-vault", label: "Switch vault" },
    { id: "open-search", label: "Open search", combo: "mod+shift+f" },
    { id: "reload", label: "Reload from disk", disabled: !workspace.path || workspace.dirty },
  ]);
  const vaultCommands = $derived<PaletteCommand[]>(workspace.vaults.map(({ name }) => ({ id: name, label: name })));

  $effect(() => {
    if (workspace.focusRequest > 0) void tick().then(() => editor?.focus());
  });
  $effect(() => {
    const target = workspace.searchTarget;
    if (target && workspace.path === target.path && !workspace.noteLoading) {
      void tick().then(() => { editor?.revealLine(target.line); workspace.searchTarget = null; });
    }
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
    const detachShortcuts = initShortcuts();
    const unregister = [
      appShortcuts.register("mod+o", () => { quickOpen = true; }, { description: "Quick switcher" }),
      appShortcuts.register("mod+p", () => { commandsOpen = true; }, { description: "Command palette" }),
      appShortcuts.register("mod+s", () => { void workspace.saveNow(); }, { description: "Save note" }),
      appShortcuts.register("mod+e", () => { if (workspace.path) workspace.setMode(workspace.mode === "live" ? "source" : "live"); }, { description: "Toggle editor mode" }),
      appShortcuts.register("mod+shift+f", () => { focusSearch(); }, { description: "Search notes" }),
    ];
    document.addEventListener("visibilitychange", visibility);
    window.addEventListener("beforeunload", beforeUnload);
    return () => {
      void workspace.saveNow();
      workspace.dispose();
      document.removeEventListener("visibilitychange", visibility);
      window.removeEventListener("beforeunload", beforeUnload);
      unregister.forEach((remove) => remove());
      detachShortcuts();
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

  function focusSearch(): void {
    collapsed = false;
    void tick().then(() => searchInput?.focus());
  }

  function openHit(hit: import("./lib/api.ts").SearchHit): void {
    workspace.openSearchHit(hit);
    if (window.innerWidth < 900) collapsed = true;
  }

  function runCommand(command: PaletteCommand): void {
    const folder = parentFolder(workspace.path ?? "");
    switch (command.id) {
      case "new-note": entryDialog = { kind: "note", folder, initial: uniqueNoteName((workspace.listing?.notes ?? []).filter((note) => parentFolder(note.path) === folder).map((note) => noteTitle(note.path)), "Untitled") }; break;
      case "new-folder": entryDialog = { kind: "folder", folder, initial: "" }; break;
      case "toggle-mode": workspace.setMode(workspace.mode === "live" ? "source" : "live"); break;
      case "toggle-theme": {
        const mode = getThemeMode();
        setThemeMode(mode === "dark" ? "system" : mode === "system" ? "light" : "dark");
        break;
      }
      case "switch-vault": vaultOpen = true; break;
      case "open-search": focusSearch(); break;
      case "reload": void workspace.reloadFromDisk(); break;
    }
  }

  function openWikilink(source: string): void {
    const link = parseWikilink(source);
    if (!link) return;
    if (!link.target) return;
    const path = resolveNote(notePaths, workspace.path ?? "", link.target);
    if (path) { workspace.openNote(path); return; }
    if (workspace.readOnly) return;
    const slash = link.target.lastIndexOf("/");
    const targetFolder = slash < 0 ? parentFolder(workspace.path ?? "") : link.target.slice(0, slash);
    const folder = targetFolder && workspace.listing?.folders.includes(targetFolder) ? targetFolder : parentFolder(workspace.path ?? "");
    entryDialog = { kind: "note", folder, initial: link.target.slice(slash + 1).replace(/\.md$/i, "") };
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
      <div class="sidebar-search">
        <SearchInput value={workspace.searchQuery} oninput={(value) => workspace.setSearchQuery(value)} bind:inputEl={searchInput} block placeholder="Search notes…" ariaLabel="Search notes" onkeydown={(event) => { if (event.key === "Enter" && workspace.searchHits[0]) { event.preventDefault(); openHit(workspace.searchHits[0]); } }} />
      </div>
      {#if workspace.loading}
        <div class="sidebar-message"><Spinner label="Loading vault" /></div>
      {:else if workspace.error}
        <div class="sidebar-message" role="alert">
          <p>{workspace.error}</p>
          <Button label="Retry" onclick={() => void workspace.retry()} />
        </div>
      {:else}
        {#if workspace.searchQuery.trim()}
          <SearchResults hits={workspace.searchHits} query={workspace.searchQuery} loading={workspace.searchLoading} error={workspace.searchError} onopen={openHit} />
        {:else}
          <FileTree onOpenNote={() => { if (window.innerWidth < 900) collapsed = true; }} />
        {/if}
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
        {#if workspace.missingNote || workspace.externalConflict || workspace.saveState === "conflict" || workspace.saveState === "error"}
          <ConflictBanner
            missing={workspace.missingNote}
            readOnly={workspace.readOnly}
            saveError={workspace.saveState === "error" ? workspace.saveDetail === "missing" ? "The note reappeared on disk. Retry save to check for changes." : workspace.saveDetail ?? "The save failed." : null}
            navigationBlocked={workspace.navigationBlocked}
            dirty={workspace.dirty}
            onload={() => workspace.reloadFromDisk()}
            onoverwrite={() => workspace.overwriteDisk()}
            onsaveasnew={() => saveAsNew = true}
            onretry={() => workspace.saveNow()}
            ondiscard={() => discardConfirm = true}
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
            onwikilink={openWikilink}
            notes={notePaths}
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

{#if quickOpen}<Palette notes={notePaths} onopen={(path) => { quickOpen = false; workspace.openNote(path); }} onclose={() => quickOpen = false} />{/if}
<CommandPalette bind:open={commandsOpen} {commands} onrun={runCommand} ariaLabel="Command palette" />
<CommandPalette bind:open={vaultOpen} commands={vaultCommands} onrun={(command) => workspace.selectVault(command.id)} ariaLabel="Switch vault" placeholder="Choose a vault…" />
{#if entryDialog}
  {@const dialog = entryDialog}
  <EntryDialog kind={dialog.kind} initial={dialog.initial} onsubmit={(name) => dialog.kind === "note" ? workspace.createNote(dialog.folder, name) : workspace.createFolder(dialog.folder, name)} onclose={() => entryDialog = null} />
{/if}

{#if saveAsNew && workspace.path}
  <EntryDialog kind="save-as-new" initial={noteTitle(workspace.path)} onsubmit={(name) => workspace.saveMissingAsNew(name)} onclose={() => saveAsNew = false} />
{/if}
{#if discardConfirm}
  <DiscardDialog onconfirm={() => workspace.discardLocalEdits()} onclose={() => discardConfirm = false} />
{/if}
