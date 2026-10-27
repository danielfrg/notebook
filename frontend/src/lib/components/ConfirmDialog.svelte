<script lang="ts">
  import { Button, Modal } from "@kenn-io/kit-ui";

  let { path, onconfirm, onclose }: { path: string; onconfirm: () => Promise<void>; onclose: () => void } = $props();
  let error = $state<string | null>(null);
  let busy = $state(false);

  async function confirm(): Promise<void> {
    if (busy) return;
    busy = true;
    error = null;
    try {
      await onconfirm();
      onclose();
    } catch (failure) {
      error = failure instanceof Error ? failure.message : "Could not move this entry";
    } finally {
      busy = false;
    }
  }
</script>

<Modal title="Move to .trash?" tone="danger" {onclose} width="min(420px, calc(100vw - 32px))">
  <p class="dialog-description">{path} will be moved to .trash.</p>
  {#if error}<p class="dialog-error" role="alert">{error}</p>{/if}
  {#snippet footer()}
    <Button label="Cancel" onclick={onclose} disabled={busy} />
    <Button label="Move to .trash" tone="danger" surface="solid" onclick={() => void confirm()} disabled={busy} />
  {/snippet}
</Modal>
