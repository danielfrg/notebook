<script lang="ts">
  import { workspace } from "../state/workspace.svelte.ts";
  const label = $derived(
    workspace.saveState === "clean" ? "Saved" :
    workspace.saveState === "dirty" ? "Unsaved changes" :
    workspace.saveState === "saving" ? "Saving" :
    workspace.saveState === "conflict" ? "Conflict" : "Error"
  );
</script>

<span class="status-pill" class:status-pill-alert={workspace.saveState === "conflict" || workspace.saveState === "error"} role="status" title={workspace.saveDetail ?? label}>{label}</span>
