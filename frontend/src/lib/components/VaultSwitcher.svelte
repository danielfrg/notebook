<script lang="ts">
  import { SelectDropdown } from "@kenn-io/kit-ui";
  import { workspace } from "../state/workspace.svelte.ts";
  import { selectedItemPlacement } from "./select-placement.ts";

  const options = $derived(workspace.vaults.map(({ name }) => ({ value: name, label: name })));
</script>

{#if workspace.vault}
  <div class="vault-switcher" use:selectedItemPlacement>
    <SelectDropdown value={workspace.vault} {options} title="Vault" onchange={(name) => workspace.selectVault(name)} />
  </div>
{/if}

<style>
  .vault-switcher {
    display: inline-flex;
    min-width: 0;
    max-width: 100%;
  }

  .vault-switcher :global(.kit-select-dropdown) {
    min-width: 0;
    max-width: 100%;
  }

  .vault-switcher :global(.kit-select-dropdown__list) {
    top: var(--nb-select-top, 8px) !important;
    left: var(--nb-select-left, 8px) !important;
    min-width: var(--nb-select-minwidth, 0px) !important;
    max-width: var(--nb-select-maxwidth, calc(100vw - 16px));
    max-height: min(384px, var(--nb-select-maxheight, calc(100dvh - 16px)));
    overscroll-behavior: contain;
  }
</style>
