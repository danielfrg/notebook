<script lang="ts">
  let { keys }: { keys: readonly string[] } = $props();
</script>

<span class="kbd-group" aria-hidden="true">
  {#each keys as key, index (index)}<kbd data-slot="kbd">{key}</kbd>{/each}
</span>
