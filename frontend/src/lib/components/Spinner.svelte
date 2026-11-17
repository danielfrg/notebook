<script lang="ts">
  interface Props {
    size?: number;
    label?: string;
  }
  let { size = 24, label = "Loading" }: Props = $props();
</script>

<span class="spinner" style:--spinner-size="{size}px" role="status" aria-label={label}></span>
