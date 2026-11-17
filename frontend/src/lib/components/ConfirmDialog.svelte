<script lang="ts">
  import { Button } from "@ui-sdk/svelte/button";
  import {
    AlertDialog,
    AlertDialogBackdrop,
    AlertDialogClose,
    AlertDialogContent,
    AlertDialogDescription,
    AlertDialogTitle,
  } from "@ui-sdk/svelte/alert-dialog";

  let {
    path,
    onconfirm,
    onclose,
  }: { path: string; onconfirm: () => Promise<void>; onclose: () => void } = $props();
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

<!-- The parent owns the open state; a close request while busy is ignored. -->
<AlertDialog
  open
  onOpenChange={(open, details) => {
    if (open) return;
    details.cancel();
    if (!busy) onclose();
  }}
>
  <AlertDialogBackdrop />
  <AlertDialogContent>
    <div data-slot="stack">
      <AlertDialogTitle class="dialog-title">Move to .trash?</AlertDialogTitle>
      <AlertDialogDescription class="dialog-description"
        >{path} will be moved to .trash.</AlertDialogDescription
      >
    </div>
    {#if error}<p class="dialog-error" role="alert">{error}</p>{/if}
    <div data-slot="row">
      <AlertDialogClose disabled={busy}>Cancel</AlertDialogClose>
      <Button data-variant="destructive" disabled={busy} onclick={() => void confirm()}
        >Move to .trash</Button
      >
    </div>
  </AlertDialogContent>
</AlertDialog>
