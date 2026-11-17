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

  let { onconfirm, onclose }: { onconfirm: () => void; onclose: () => void } = $props();
</script>

<!-- The parent owns the open state: every close request goes to onclose and the parent unmounts. -->
<AlertDialog
  open
  onOpenChange={(open, details) => {
    if (open) return;
    details.cancel();
    onclose();
  }}
>
  <AlertDialogBackdrop />
  <AlertDialogContent>
    <div data-slot="stack">
      <AlertDialogTitle>Discard local edits?</AlertDialogTitle>
      <AlertDialogDescription
        >Your unsaved edits will be lost. This cannot be undone.</AlertDialogDescription
      >
    </div>
    <div data-slot="row">
      <AlertDialogClose>Keep editing</AlertDialogClose>
      <Button
        data-variant="destructive"
        onclick={() => {
          onconfirm();
          onclose();
        }}>Discard local edits</Button
      >
    </div>
  </AlertDialogContent>
</AlertDialog>
