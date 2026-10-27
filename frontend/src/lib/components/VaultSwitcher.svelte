<script lang="ts">
  import { SelectDropdown } from "@kenn-io/kit-ui";
  import { workspace } from "../state/workspace.svelte.ts";

  const options = $derived(workspace.vaults.map(({ name }) => ({ value: name, label: name })));
</script>

{#if workspace.vault}
  <SelectDropdown class="vault-switcher" value={workspace.vault} {options} title="Vault" onchange={(name) => workspace.selectVault(name)} />
{/if}
