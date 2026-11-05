<script lang="ts">
  import { Menu, MenuContent, MenuRadioGroup, MenuRadioItem, MenuTrigger } from "@kenn-io/kit-ui";
  import ChevronsUpDownIcon from "@lucide/svelte/icons/chevrons-up-down";
  import { workspace } from "../state/workspace.svelte.ts";
</script>

{#if workspace.vault}
  <Menu>
    <MenuTrigger
      ariaLabel={`Vault: ${workspace.vault}`}
      title="Switch vault"
      class="vault-trigger"
      disabled={workspace.vaults.length < 2}
    >
      <span class="vault-name">{workspace.vault}</span>
      {#if workspace.vaults.length > 1}<ChevronsUpDownIcon
          size={14}
          strokeWidth={1.75}
          aria-hidden="true"
        />{/if}
    </MenuTrigger>
    <MenuContent ariaLabel="Vaults">
      <MenuRadioGroup
        value={workspace.vault}
        onchange={(name) => workspace.selectVault(name)}
        ariaLabel="Vaults"
      >
        {#each workspace.vaults as vault (vault.name)}
          <MenuRadioItem value={vault.name} closeOnSelect>{vault.name}</MenuRadioItem>
        {/each}
      </MenuRadioGroup>
    </MenuContent>
  </Menu>
{/if}
