<script lang="ts">
  import { ChevronsUpDownIcon } from "@lucide/svelte";
  import {
    Menu,
    MenuContent,
    MenuRadioGroup,
    MenuRadioItem,
    MenuTrigger,
  } from "@ui-sdk/svelte/menu";
  import { workspace } from "../state/workspace.svelte.ts";
</script>

{#if workspace.vault}
  <Menu>
    <MenuTrigger
      class="vault-trigger"
      data-variant="ghost"
      data-size="sm"
      aria-label={`Vault: ${workspace.vault}`}
      title="Switch vault"
      disabled={workspace.vaults.length < 2}
    >
      <span class="vault-name">{workspace.vault}</span>
      {#if workspace.vaults.length > 1}<ChevronsUpDownIcon aria-hidden="true" />{/if}
    </MenuTrigger>
    <MenuContent aria-label="Vaults">
      <MenuRadioGroup
        value={workspace.vault}
        onValueChange={(name) => name && workspace.selectVault(name)}
        aria-label="Vaults"
      >
        {#each workspace.vaults as vault (vault.name)}
          <MenuRadioItem value={vault.name} closeOnSelect>{vault.name}</MenuRadioItem>
        {/each}
      </MenuRadioGroup>
    </MenuContent>
  </Menu>
{/if}
