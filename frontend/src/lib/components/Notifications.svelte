<script lang="ts">
  import { Toast, ToastClose, ToastRegion, ToastTitle } from "@ui-sdk/svelte/toast";
  import { XIcon } from "@lucide/svelte";
</script>

<ToastRegion side="top" align="center">
  <Toast>
    <ToastTitle />
    <ToastClose aria-label="Dismiss notification"><XIcon aria-hidden="true" /></ToastClose>
  </Toast>
</ToastRegion>
