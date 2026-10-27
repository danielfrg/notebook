<script lang="ts">
  import { Button, Modal, TextInput } from "@kenn-io/kit-ui";
  import { validateNoteName } from "../entry-names.ts";
  import { validateEntryName } from "../paths.ts";

  let {
    kind,
    initial = "",
    onsubmit,
    onclose,
  }: {
    kind: "note" | "folder" | "rename-note" | "rename-folder" | "save-as-new";
    initial?: string;
    onsubmit: (name: string) => Promise<void>;
    onclose: () => void;
  } = $props();

  let name = $state("");
  let initialized = false;
  $effect.pre(() => {
    if (!initialized) { name = initial; initialized = true; }
  });
  let error = $state<string | null>(null);
  let busy = $state(false);
  const isNote = $derived(kind === "note" || kind === "rename-note" || kind === "save-as-new");
  const title = $derived(kind === "note" ? "New note" : kind === "folder" ? "New folder" : kind === "save-as-new" ? "Save as new note" : "Rename");
  const action = $derived(kind.startsWith("rename") ? "Rename" : kind === "save-as-new" ? "Save as new note" : "Create");

  async function submit(): Promise<void> {
    if (busy) return;
    error = (isNote ? validateNoteName(name) : validateEntryName(name));
    if (error) return;
    busy = true;
    try {
      await onsubmit(name);
      onclose();
    } catch (failure) {
      error = failure instanceof Error ? failure.message : "The operation failed";
    } finally {
      busy = false;
    }
  }
</script>

<Modal {title} {onclose} width="min(420px, calc(100vw - 32px))">
  <form class="entry-form" onsubmit={(event) => { event.preventDefault(); void submit(); }}>
    <label for="entry-name">Name</label>
    <TextInput id="entry-name" bind:value={name} block autofocus invalid={!!error} oninput={() => (error = null)} />
    {#if error}<p class="dialog-error" role="alert">{error}</p>{/if}
    <button hidden type="submit">{action}</button>
  </form>
  {#snippet footer()}
    <Button label="Cancel" onclick={onclose} disabled={busy} />
    <Button label={action} surface="solid" onclick={() => void submit()} disabled={busy} />
  {/snippet}
</Modal>
