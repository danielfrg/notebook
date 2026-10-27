<script lang="ts">
  import { Button } from "@kenn-io/kit-ui";

  let {
    missing,
    readOnly,
    onload,
    onoverwrite,
    onsaveasnew,
  }: {
    missing: boolean;
    readOnly: boolean;
    onload: () => Promise<void>;
    onoverwrite: () => Promise<void>;
    onsaveasnew: () => void;
  } = $props();
  let busy = $state(false);
  let error = $state<string | null>(null);

  async function run(action: () => Promise<void>): Promise<void> {
    if (busy) return;
    busy = true;
    error = null;
    try { await action(); }
    catch (failure) { error = failure instanceof Error ? failure.message : "Could not resolve this change"; }
    finally { busy = false; }
  }
</script>

<div class="conflict-banner" role="alert">
  <div class="conflict-banner-copy">
    <strong>{missing ? "This note no longer exists on disk" : "This note changed on disk"}</strong>
    {#if !missing}<span>Choose which version to keep.</span>{/if}
    {#if error}<span class="dialog-error">{error}</span>{/if}
  </div>
  <div class="conflict-banner-actions">
    {#if missing}
      {#if !readOnly}<Button label="Save as new note" onclick={onsaveasnew} disabled={busy} />{/if}
    {:else}
      <Button label="Load disk version" onclick={() => void run(onload)} disabled={busy} />
      {#if !readOnly}<Button label="Overwrite" surface="solid" onclick={() => void run(onoverwrite)} disabled={busy} />{/if}
    {/if}
  </div>
</div>
