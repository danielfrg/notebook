<script lang="ts">
  import { SegmentedControl, SidebarToggle, ThemeToggle, TopBar } from "@kenn-io/kit-ui";
  import { onMount } from "svelte";
  import { workspace } from "../state/workspace.svelte.ts";
  import StatusPill from "./StatusPill.svelte";

  let { onOpenSidebar, collapsed }: { onOpenSidebar: () => void; collapsed: boolean } = $props();
  let narrow = $state(window.matchMedia("(max-width: 640px)").matches);
  const modeOptions = $derived([{ value: "live", label: narrow ? "Live" : "Live Preview" }, { value: "source", label: "Source" }]);
  const notePath = $derived(workspace.path?.endsWith(".md") ? workspace.path.slice(0, -3) : workspace.path);

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
    <ThemeToggle />
  {/snippet}
</TopBar>
{#if workspace.path}
  <div class="mobile-note-context"><h1 title={workspace.path}>{workspace.vault} / {notePath}</h1></div>
{/if}
