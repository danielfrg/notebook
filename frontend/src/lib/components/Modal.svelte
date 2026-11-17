<script lang="ts">
  import type { Snippet } from "svelte";
  import { XIcon } from "@lucide/svelte";
  import {
    Dialog,
    DialogBackdrop,
    DialogClose,
    DialogContent,
    DialogTitle,
  } from "@ui-sdk/svelte/dialog";

  interface Props {
    title: string;
    /** Content width: a form, or the wider details view. */
    size?: "sm" | "md" | "lg";
    /** Escape, the backdrop and the close button call onclose; false keeps only explicit actions. */
    closable?: boolean;
    onclose: () => void;
    children: Snippet;
    footer?: Snippet;
  }
  let { title, size = "md", closable = true, onclose, children, footer }: Props = $props();

  // One Escape press can arrive twice (from the field and from the dialog); count it once.
  let lastClose = 0;
  function requestClose() {
    const now = Date.now();
    if (now - lastClose < 100) return;
    lastClose = now;
    onclose();
  }
</script>

<!-- The parent owns the open state: every close request goes to onclose and the parent unmounts. -->
<Dialog
  open
  dismissible={closable}
  onOpenChange={(open, details) => {
    if (open) return;
    details.cancel();
    requestClose();
  }}
>
  <DialogBackdrop />
  <DialogContent class="modal" data-size={size}>
    <DialogTitle class="modal-title">{title}</DialogTitle>
    <div class="modal-body">{@render children()}</div>
    {#if footer}<div data-slot="row" class="modal-footer">{@render footer()}</div>{/if}
    {#if closable}
      <DialogClose aria-label="Close"><XIcon aria-hidden="true" /></DialogClose>
    {/if}
  </DialogContent>
</Dialog>
