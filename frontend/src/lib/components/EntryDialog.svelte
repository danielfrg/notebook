<script lang="ts">
  import { Button } from "@ui-sdk/svelte/button";
  import { Field, FieldLabel } from "@ui-sdk/svelte/field";
  import { Input } from "@ui-sdk/svelte/input";
  import Modal from "./Modal.svelte";
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
    if (!initialized) {
      name = initial;
      initialized = true;
    }
  });
  let error = $state<string | null>(null);
  let busy = $state(false);
  const isNote = $derived(kind === "note" || kind === "rename-note" || kind === "save-as-new");
  const title = $derived(
    kind === "note"
      ? "New note"
      : kind === "folder"
        ? "New folder"
        : kind === "save-as-new"
          ? "Save as new note"
          : "Rename",
  );
  const action = $derived(
    kind.startsWith("rename") ? "Rename" : kind === "save-as-new" ? "Save as new note" : "Create",
  );

  async function submit(): Promise<void> {
    if (busy) return;
    const normalized = name.trim();
    error = isNote ? validateNoteName(normalized) : validateEntryName(normalized);
    if (error) return;
    busy = true;
    try {
      await onsubmit(normalized);
      onclose();
    } catch (failure) {
      error = failure instanceof Error ? failure.message : "The operation failed";
    } finally {
      busy = false;
    }
  }
</script>

<Modal
  {title}
  size="sm"
  onclose={() => {
    if (!busy) onclose();
  }}
>
  <form
    class="entry-form"
    id="entry-form"
    onsubmit={(event) => {
      event.preventDefault();
      void submit();
    }}
  >
    <Field invalid={error ? true : undefined}>
      <FieldLabel>Name</FieldLabel>
      <Input bind:value={name} autofocus oninput={() => (error = null)} />
    </Field>
    {#if error}<p class="dialog-error" role="alert">{error}</p>{/if}
  </form>
  {#snippet footer()}
    <Button onclick={onclose} disabled={busy}>Cancel</Button>
    <Button data-variant="solid" type="submit" form="entry-form" disabled={busy}>{action}</Button>
  {/snippet}
</Modal>
