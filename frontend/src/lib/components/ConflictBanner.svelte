<script lang="ts">
  import { Button } from "@ui-sdk/svelte/button";

  let {
    missing,
    readOnly,
    saveError,
    navigationBlocked,
    dirty,
    onload,
    onoverwrite,
    onsaveasnew,
    onretry,
    ondiscard,
  }: {
    missing: boolean;
    readOnly: boolean;
    saveError: string | null;
    navigationBlocked: boolean;
    dirty: boolean;
    onload: () => Promise<void>;
    onoverwrite: () => Promise<void>;
    onsaveasnew: () => void;
    onretry: () => Promise<void>;
    ondiscard: () => void;
  } = $props();
  let busy = $state(false);
  let error = $state<string | null>(null);

  async function run(action: () => Promise<void>): Promise<void> {
    if (busy) return;
    busy = true;
    error = null;
    try {
      await action();
    } catch (failure) {
      error = failure instanceof Error ? failure.message : "Could not resolve this change";
    } finally {
      busy = false;
    }
  }
</script>

<div class="conflict-banner" role="alert">
  <div class="conflict-banner-copy">
    <strong
      >{missing
        ? "This note no longer exists on disk"
        : saveError
          ? "Could not save this note"
          : "This note changed on disk"}</strong
    >
    {#if saveError && !missing}<span>{saveError}</span>{:else if !missing}<span
        >Choose which version to keep.</span
      >{/if}
    {#if navigationBlocked}<span
        >Navigation was blocked to keep your unsaved edits. Save or discard them to leave.</span
      >{/if}
    {#if error}<span class="dialog-error">{error}</span>{/if}
  </div>
  <div class="conflict-banner-actions">
    {#if missing}
      {#if !readOnly}<Button onclick={onsaveasnew} disabled={busy}>Save as new note</Button>{/if}
    {:else if saveError}
      {#if !readOnly}<Button onclick={() => void run(onretry)} disabled={busy}>Retry save</Button
        >{/if}
    {:else}
      <Button onclick={() => void run(onload)} disabled={busy}>Load disk version</Button>
      {#if !readOnly}<Button
          data-variant="solid"
          onclick={() => void run(onoverwrite)}
          disabled={busy}>Overwrite</Button
        >{/if}
    {/if}
    {#if dirty}<Button onclick={ondiscard} disabled={busy}>Discard local edits</Button>{/if}
  </div>
</div>
