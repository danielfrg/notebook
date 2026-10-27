<script lang="ts">
  import { workspace } from "../state/workspace.svelte.ts";
  const label = $derived(
    workspace.saveState === "clean" ? "Saved" :
    workspace.saveState === "dirty" ? "Unsaved changes" :
    workspace.saveState === "saving" ? "Saving" :
    workspace.saveState === "conflict" ? "Conflict" : "Error"
  );
</script>

<span class="status-pill" data-state={workspace.saveState} class:status-pill-alert={workspace.saveState === "conflict" || workspace.saveState === "error"} role={workspace.saveState === "conflict" || workspace.saveState === "error" ? "status" : undefined} title={workspace.saveDetail ?? label}><span class="status-dot" aria-hidden="true"></span><span class="status-label">{label}</span></span>
