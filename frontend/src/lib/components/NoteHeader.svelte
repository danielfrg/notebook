<script lang="ts">
  import { SegmentedControl, SidebarToggle, ThemeToggle, TopBar } from "@kenn-io/kit-ui";
  import { noteTitle } from "../paths.ts";
  import { workspace } from "../state/workspace.svelte.ts";
  import StatusPill from "./StatusPill.svelte";

  let { onOpenSidebar, collapsed }: { onOpenSidebar: () => void; collapsed: boolean } = $props();
  const modeOptions = [{ value: "live", label: "Live Preview" }, { value: "source", label: "Source" }];
</script>

<TopBar>
  {#snippet left()}
    <SidebarToggle state={collapsed ? "collapsed" : "expanded"} onclick={onOpenSidebar} />
    <h1 class="app-title">Notebook</h1>
    {#if workspace.path}<span class="note-breadcrumb" title={workspace.path}>{workspace.vault} / {noteTitle(workspace.path)}</span>{/if}
  {/snippet}
  {#snippet right()}
    {#if workspace.path && !workspace.noteError}
      <SegmentedControl options={modeOptions} value={workspace.mode} ariaLabel="Editor mode" onchange={(value) => workspace.setMode(value as "live" | "source")} />
      <StatusPill />
    {/if}
    <ThemeToggle />
  {/snippet}
</TopBar>
