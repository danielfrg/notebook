<script lang="ts">
  import { Button, Modal } from "@kenn-io/kit-ui";

  let { onconfirm, onclose }: { onconfirm: () => void; onclose: () => void } = $props();
</script>

<Modal title="Discard local edits?" tone="danger" {onclose} width="min(420px, calc(100vw - 32px))">
  <p class="dialog-description">Your unsaved edits will be lost. This cannot be undone.</p>
  {#snippet footer()}
    <Button label="Keep editing" onclick={onclose} />
    <Button label="Discard local edits" tone="danger" surface="solid" onclick={() => { onconfirm(); onclose(); }} />
  {/snippet}
</Modal>
