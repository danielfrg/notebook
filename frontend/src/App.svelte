<script lang="ts">
  import {
    CollapsibleSidebar,
    EmptyState,
    SidebarToggle,
    ThemeToggle,
    TopBar,
  } from "@kenn-io/kit-ui";
  import NotebookTextIcon from "@lucide/svelte/icons/notebook-text";

  let collapsed = $state(false);
  let sidebarWidth = $state(280);
</script>

<div class="app-shell">
  <CollapsibleSidebar
    isCollapsed={collapsed}
    showCollapsedStrip
    overlayOnNarrow
    mainOverflow="hidden"
    {sidebarWidth}
    onSidebarResize={(width) => (sidebarWidth = width)}
    onExpand={() => (collapsed = false)}
  >
    {#snippet sidebar()}
      <header class="app-sidebar-header">
        <h1 class="app-title">Notebook</h1>
        <SidebarToggle onclick={() => (collapsed = true)} class="kit-sidebar-toggle--push" />
      </header>
    {/snippet}

    <TopBar>
      {#snippet right()}
        <ThemeToggle />
      {/snippet}
    </TopBar>
    <main class="app-content">
      <EmptyState title="No note open">
        {#snippet icon()}
          <NotebookTextIcon size="28" strokeWidth="1.5" aria-hidden="true" />
        {/snippet}
      </EmptyState>
    </main>
  </CollapsibleSidebar>
</div>
