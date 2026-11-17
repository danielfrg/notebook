<script lang="ts">
  import { Button } from "@ui-sdk/svelte/button";
  import { Drawer, DrawerBackdrop, DrawerContent, DrawerTitle } from "@ui-sdk/svelte/drawer";
  import { Input, InputClear, InputGroup } from "@ui-sdk/svelte/input";
  import { Preferences } from "@ui-sdk/svelte/preferences";
  import {
    createResizableController,
    Resizable,
    ResizableHandle,
    ResizablePanel,
  } from "@ui-sdk/svelte/resizable";
  import { toast } from "@ui-sdk/svelte/toast";
  import { colorScheme } from "./lib/color-scheme.ts";
  import { copyToClipboard } from "./lib/clipboard.ts";
  import { appShortcuts, initShortcuts } from "./lib/shortcuts.ts";
  import Spinner from "./lib/components/Spinner.svelte";
  import ArrowDownAZIcon from "@lucide/svelte/icons/arrow-down-a-z";
  import ArrowDownWideNarrowIcon from "@lucide/svelte/icons/arrow-down-wide-narrow";
  import FolderPlusIcon from "@lucide/svelte/icons/folder-plus";
  import PanelLeftCloseIcon from "@lucide/svelte/icons/panel-left-close";
  import SearchIcon from "@lucide/svelte/icons/search";
  import SquarePenIcon from "@lucide/svelte/icons/square-pen";
  import { onMount, tick } from "svelte";
  import Editor from "./lib/editor/Editor.svelte";
  import { livePreview, previewContext } from "./lib/editor/livePreview.ts";
  import RecentList from "./lib/components/RecentList.svelte";
  import FileTree from "./lib/components/FileTree.svelte";
  import ConflictBanner from "./lib/components/ConflictBanner.svelte";
  import DiscardDialog from "./lib/components/DiscardDialog.svelte";
  import EntryDialog from "./lib/components/EntryDialog.svelte";
  import NoteHeader from "./lib/components/NoteHeader.svelte";
  import Notifications from "./lib/components/Notifications.svelte";
  import Palette, { type PaletteAction } from "./lib/components/Palette.svelte";
  import StartPage from "./lib/components/StartPage.svelte";
  import SearchResults from "./lib/components/SearchResults.svelte";
  import Toolbar from "./lib/components/Toolbar.svelte";
  import VaultSwitcher from "./lib/components/VaultSwitcher.svelte";
  import { workspace } from "./lib/state/workspace.svelte.ts";
  import { noteTitle, parentFolder } from "./lib/paths.ts";
  import { countWords } from "./lib/wordcount.ts";
  import { uniqueNoteName } from "./lib/entry-names.ts";
  import { parseWikilink, resolveNote, wikilinkCreation } from "./lib/wikilinks.ts";

  const narrowQuery = window.matchMedia("(max-width: 900px)");
  let narrow = $state(narrowQuery.matches);
  let collapsed = $state(narrowQuery.matches);
  const layout = createResizableController();
  let sidebarWidth = $state(272);
  let editor = $state<Editor>();
  let saveAsNew = $state(false);
  let discardConfirm = $state(false);
  let searchOpen = $state(false);
  let sortMode = $state<"name" | "recent">(
    (() => {
      try {
        return localStorage.getItem("notebook-sidebar-sort") === "recent" ? "recent" : "name";
      } catch {
        return "name";
      }
    })(),
  );
  function toggleSort(): void {
    sortMode = sortMode === "name" ? "recent" : "name";
    try {
      localStorage.setItem("notebook-sidebar-sort", sortMode);
    } catch {
      /* Storage may be disabled. */
    }
  }
  let paletteOpen = $state(false);
  let editorScrolled = $state(false);
  const words = $derived(countWords(workspace.value));
  let searchBox: HTMLDivElement | undefined = $state();
  let entryDialog: { kind: "note" | "folder"; folder: string; initial: string } | null =
    $state(null);
  const notePaths = $derived(
    (workspace.listing?.notes ?? [])
      .toSorted((a, b) => b.modified.localeCompare(a.modified))
      .map((entry) => entry.path),
  );
  const actions = $derived<PaletteAction[]>([
    { id: "new-note", label: "New note", disabled: workspace.readOnly },
    { id: "new-folder", label: "New folder", disabled: workspace.readOnly },
    { id: "toggle-mode", label: "Toggle Live / Source", combo: "mod+e", disabled: !workspace.path },
    { id: "toggle-theme", label: "Toggle theme" },
    { id: "open-search", label: "Search in sidebar", combo: "mod+shift+f" },
    { id: "reload", label: "Reload from disk", disabled: !workspace.path || workspace.dirty },
    ...workspace.vaults
      .filter(({ name }) => name !== workspace.vault)
      .map(({ name }) => ({ id: `vault:${name}`, label: `Switch to ${name}` })),
  ]);

  // In the wide layout the sidebar panel follows `collapsed`; the narrow one is a drawer.
  $effect(() => {
    if (narrow) return;
    if (collapsed) layout.collapse("sidebar");
    else layout.expand("sidebar");
  });
  $effect(() => {
    if (workspace.focusRequest > 0) void tick().then(() => editor?.focus());
  });
  $effect(() => {
    const target = workspace.searchTarget;
    if (target && workspace.path === target.path && !workspace.noteLoading) {
      void tick().then(() => {
        editor?.revealLine(target.line);
        workspace.searchTarget = null;
      });
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
    } catch {
      /* Storage may be disabled. */
    }
    const onNarrowChange = () => {
      narrow = narrowQuery.matches;
      collapsed = narrowQuery.matches;
    };
    narrowQuery.addEventListener("change", onNarrowChange);
    void workspace.init();
    const visibility = () => {
      if (document.visibilityState === "hidden") void workspace.saveNow();
    };
    const beforeUnload = (event: BeforeUnloadEvent) => {
      if (!workspace.dirty) return;
      event.preventDefault();
      event.returnValue = "";
    };
    const detachShortcuts = initShortcuts();
    const unregister = [
      appShortcuts.register("mod+k", () => {
        paletteOpen = true;
      }),
      appShortcuts.register("mod+o", () => {
        paletteOpen = true;
      }),
      appShortcuts.register("mod+p", () => {
        paletteOpen = true;
      }),
      appShortcuts.register("mod+[", () => {
        workspace.goBack();
      }),
      appShortcuts.register("mod+]", () => {
        workspace.goForward();
      }),
      appShortcuts.register("mod+s", () => {
        void workspace.saveNow();
      }),
      appShortcuts.register("mod+e", () => {
        if (workspace.path) workspace.setMode(workspace.mode === "live" ? "source" : "live");
      }),
      appShortcuts.register("mod+shift+f", () => {
        focusSearch();
      }),
    ];
    document.addEventListener("visibilitychange", visibility);
    window.addEventListener("beforeunload", beforeUnload);
    return () => {
      void workspace.saveNow();
      workspace.dispose();
      document.removeEventListener("visibilitychange", visibility);
      window.removeEventListener("beforeunload", beforeUnload);
      narrowQuery.removeEventListener("change", onNarrowChange);
      unregister.forEach((remove) => remove());
      detachShortcuts();
    };
  });

  /** Keeps the collapsed state in step with a drag, and remembers the width the user chose. */
  function commitLayout(): void {
    collapsed = layout.isCollapsed("sidebar");
    if (collapsed) return;
    const width = document.getElementById("sidebar")?.getBoundingClientRect().width ?? 0;
    if (width < 200 || width > 600) return;
    sidebarWidth = Math.round(width);
    try {
      localStorage.setItem("notebook-sidebar", String(sidebarWidth));
    } catch {
      /* Storage may be disabled. */
    }
  }

  function blurEditor(event: FocusEvent): void {
    const current = event.currentTarget as HTMLElement;
    if (!event.relatedTarget || !current.contains(event.relatedTarget as Node))
      void workspace.saveNow();
  }

  function focusSearch(): void {
    collapsed = false;
    searchOpen = true;
    void tick().then(() => searchBox?.querySelector("input")?.focus());
  }

  function openHit(hit: import("./lib/api.ts").SearchHit): void {
    workspace.openSearchHit(hit);
    if (narrow) collapsed = true;
  }

  function newNote(folder = ""): void {
    entryDialog = {
      kind: "note",
      folder,
      initial: uniqueNoteName(
        (workspace.listing?.notes ?? [])
          .filter((note) => parentFolder(note.path) === folder)
          .map((note) => noteTitle(note.path)),
        "Untitled",
      ),
    };
  }

  async function copyNote(): Promise<void> {
    if (await copyToClipboard(workspace.value)) toast("Markdown copied");
    else toast.error("Could not copy");
  }

  function closeSearch(): void {
    workspace.setSearchQuery("");
    searchOpen = false;
  }

  function runAction(id: string): void {
    paletteOpen = false;
    if (id.startsWith("vault:")) {
      void workspace.selectVault(id.slice("vault:".length));
      return;
    }
    const folder = parentFolder(workspace.path ?? "");
    switch (id) {
      case "new-note":
        newNote(folder);
        break;
      case "new-folder":
        entryDialog = { kind: "folder", folder, initial: "" };
        break;
      case "toggle-mode":
        workspace.setMode(workspace.mode === "live" ? "source" : "live");
        break;
      case "toggle-theme": {
        const mode = colorScheme.get().value;
        colorScheme.set(mode === "dark" ? "system" : mode === "system" ? "light" : "dark");
        break;
      }
      case "open-search":
        focusSearch();
        break;
      case "reload":
        void workspace.reloadFromDisk();
        break;
    }
  }

  function openWikilink(source: string): void {
    const link = parseWikilink(source);
    if (!link) return;
    if (!link.target) return;
    const path = resolveNote(notePaths, workspace.path ?? "", link.target);
    if (path) {
      workspace.openNote(path);
      return;
    }
    if (workspace.readOnly) return;
    entryDialog = { kind: "note", ...wikilinkCreation(workspace.path ?? "", link.target) };
  }
</script>

{#snippet sidebar()}
  <div class="sidebar-header">
    <VaultSwitcher />
    <Button
      data-variant="ghost"
      data-size="sm"
      aria-label="Search"
      title="Search (⌘K)"
      onclick={() => (paletteOpen = true)}><SearchIcon aria-hidden="true" /></Button
    >
    {#if !workspace.readOnly && !workspace.loading && !workspace.error}
      <Button
        data-variant="ghost"
        data-size="sm"
        aria-label="New note"
        title="New note"
        onclick={() => newNote()}><SquarePenIcon aria-hidden="true" /></Button
      >
      <Button
        data-variant="ghost"
        data-size="sm"
        aria-label="New folder"
        title="New folder"
        onclick={() => (entryDialog = { kind: "folder", folder: "", initial: "" })}
        ><FolderPlusIcon aria-hidden="true" /></Button
      >
    {/if}
    <Button
      data-variant="ghost"
      data-size="sm"
      aria-label={sortMode === "name" ? "Sort by recently edited" : "Sort by name"}
      title={sortMode === "name" ? "Sorted by name" : "Sorted by recently edited"}
      aria-pressed={sortMode === "recent"}
      onclick={toggleSort}
    >
      {#if sortMode === "name"}<ArrowDownAZIcon aria-hidden="true" />{:else}<ArrowDownWideNarrowIcon
          aria-hidden="true"
        />{/if}
    </Button>
    <Button
      data-variant="ghost"
      data-size="sm"
      aria-label="Hide sidebar"
      title="Hide sidebar"
      onclick={() => (collapsed = true)}><PanelLeftCloseIcon aria-hidden="true" /></Button
    >
  </div>
  {#if searchOpen || workspace.searchQuery}
    <div
      class="sidebar-search"
      bind:this={searchBox}
      onfocusout={(event) => {
        if (
          !workspace.searchQuery &&
          !event.currentTarget.contains(event.relatedTarget as Node | null)
        )
          searchOpen = false;
      }}
    >
      <InputGroup>
        <Input
          type="search"
          value={workspace.searchQuery}
          onValueChange={(value) => workspace.setSearchQuery(value)}
          placeholder="Search note contents…"
          aria-label="Search note contents"
          onkeydown={(event) => {
            if (event.key === "Enter" && workspace.searchHits[0]) {
              event.preventDefault();
              openHit(workspace.searchHits[0]);
            } else if (event.key === "Escape" && !workspace.searchQuery) closeSearch();
          }}
        />
        <InputClear aria-label="Clear search" />
      </InputGroup>
    </div>
  {/if}
  {#if workspace.loading}
    <div class="sidebar-message"><Spinner label="Loading vault" /></div>
  {:else if workspace.error}
    <div class="sidebar-message" role="alert">
      <p>{workspace.error}</p>
      <Button onclick={() => void workspace.retry()}>Retry</Button>
    </div>
  {:else}
    {#if workspace.searchQuery.trim()}
      <SearchResults
        hits={workspace.searchHits}
        query={workspace.searchQuery}
        loading={workspace.searchLoading}
        error={workspace.searchError}
        onopen={openHit}
      />
    {:else if sortMode === "recent"}
      <RecentList
        onOpenNote={() => {
          if (narrow) collapsed = true;
        }}
      />
    {:else}
      <FileTree
        onOpenNote={() => {
          if (narrow) collapsed = true;
        }}
      />
    {/if}
  {/if}
{/snippet}

{#snippet main()}
  <NoteHeader {collapsed} onOpenSidebar={() => (collapsed = false)} />
  <main class="app-content">
    {#if workspace.loading}
      <div class="content-message"><Spinner label="Loading vault" /></div>
    {:else if workspace.error}
      <div class="content-message" role="alert">
        <p>{workspace.error}</p>
        <Button onclick={() => void workspace.retry()}>Retry</Button>
      </div>
    {:else if workspace.noteLoading}
      <div class="content-message"><Spinner label="Loading note" /></div>
    {:else if workspace.noteError}
      <div class="content-message" role="alert">{workspace.noteError}</div>
    {:else if workspace.path}
      {#if workspace.missingNote || workspace.externalConflict || workspace.saveState === "conflict" || workspace.saveState === "error"}
        <ConflictBanner
          missing={workspace.missingNote}
          readOnly={workspace.readOnly}
          saveError={workspace.saveState === "error"
            ? workspace.saveDetail === "missing"
              ? "The note reappeared on disk. Retry save to check for changes."
              : (workspace.saveDetail ?? "The save failed.")
            : null}
          navigationBlocked={workspace.navigationBlocked}
          dirty={workspace.dirty}
          onload={() => workspace.reloadFromDisk()}
          onoverwrite={() => workspace.overwriteDisk()}
          onsaveasnew={() => (saveAsNew = true)}
          onretry={() => workspace.saveNow()}
          ondiscard={() => (discardConfirm = true)}
        />
      {/if}
      <Toolbar
        run={(command) => editor?.run(command)}
        oncopy={() => void copyNote()}
        disabled={workspace.readOnly || !!workspace.lineSeparatorIssue}
      />
      {#if workspace.lineSeparatorIssue}<p class="note-reason" role="status">
          {workspace.lineSeparatorIssue}
        </p>{/if}
      <div class="editor-host" class:is-scrolled={editorScrolled} onfocusout={blurEditor}>
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
          onscrolled={(scrolled) => (editorScrolled = scrolled)}
        />
      </div>
      <footer class="editor-status">
        <span>{words.toLocaleString()} {words === 1 ? "word" : "words"}</span>
      </footer>
    {:else}
      <StartPage
        recent={notePaths.slice(0, 5)}
        readOnly={workspace.readOnly}
        onnew={() => newNote()}
        onsearch={() => (paletteOpen = true)}
        onopen={(path) => workspace.openNote(path)}
      />
    {/if}
  </main>
{/snippet}

<Preferences preferences={[colorScheme]}>
  <div class="app-shell">
    {#if narrow}
      <Drawer
        side="left"
        open={!collapsed}
        onOpenChange={(open) => {
          collapsed = !open;
        }}
      >
        <DrawerBackdrop />
        <DrawerContent class="sidebar-drawer">
          <DrawerTitle class="sr-only">Notes</DrawerTitle>
          <div class="sidebar">{@render sidebar()}</div>
        </DrawerContent>
      </Drawer>
      <div class="app-main">{@render main()}</div>
    {:else}
      <Resizable class="app-split" controller={layout} onLayoutCommit={commitLayout}>
        <ResizablePanel
          id="sidebar"
          class="app-sidebar"
          defaultSize={`${sidebarWidth}px`}
          minSize="200px"
          maxSize="600px"
          collapsible
          collapsedSize="0px"
        >
          <div class="sidebar">{@render sidebar()}</div>
        </ResizablePanel>
        <ResizableHandle aria-label="Resize sidebar" />
        <ResizablePanel class="app-main-panel">
          <div class="app-main">{@render main()}</div>
        </ResizablePanel>
      </Resizable>
    {/if}
    <Notifications />
  </div>
  {#if paletteOpen}
    <Palette
      notes={notePaths}
      {actions}
      search={(query) => workspace.searchContent(query)}
      onopen={(path) => {
        paletteOpen = false;
        workspace.openNote(path);
      }}
      onhit={(hit) => {
        paletteOpen = false;
        workspace.openSearchHit(hit);
      }}
      onaction={runAction}
      onclose={() => (paletteOpen = false)}
    />
  {/if}
  {#if entryDialog}
    {@const dialog = entryDialog}
    <EntryDialog
      kind={dialog.kind}
      initial={dialog.initial}
      onsubmit={(name) =>
        dialog.kind === "note"
          ? workspace.createNote(dialog.folder, name)
          : workspace.createFolder(dialog.folder, name)}
      onclose={() => (entryDialog = null)}
    />
  {/if}

  {#if saveAsNew && workspace.path}
    <EntryDialog
      kind="save-as-new"
      initial={noteTitle(workspace.path)}
      onsubmit={(name) => workspace.saveMissingAsNew(name)}
      onclose={() => (saveAsNew = false)}
    />
  {/if}
  {#if discardConfirm}
    <DiscardDialog
      onconfirm={() => workspace.discardLocalEdits()}
      onclose={() => (discardConfirm = false)}
    />
  {/if}
</Preferences>
