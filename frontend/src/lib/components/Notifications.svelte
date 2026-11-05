<script lang="ts">
  import { dismissFlash, getFlashes, IconButton, type FlashTone } from "@kenn-io/kit-ui";
  import Inbox from "@lucide/svelte/icons/inbox";
  import Info from "@lucide/svelte/icons/info";
  import CircleCheck from "@lucide/svelte/icons/circle-check";
  import TriangleAlert from "@lucide/svelte/icons/triangle-alert";
  import CircleX from "@lucide/svelte/icons/circle-x";
  import X from "@lucide/svelte/icons/x";

  const toneIcons = {
    neutral: Inbox,
    info: Info,
    success: CircleCheck,
    warning: TriangleAlert,
    danger: CircleX,
  };
  const toneLabels: Record<FlashTone, string> = {
    neutral: "",
    info: "Info",
    success: "Success",
    warning: "Warning",
    danger: "Error",
  };

  // The kit store owns ordering, expiry, the stack limit, and dismissal.
  const notifications = $derived(getFlashes());
</script>

<div
  class="notifications"
  role="status"
  aria-live="polite"
  aria-atomic="false"
  aria-relevant="additions text"
>
  <div class="notifications__stack">
    {#each notifications as flash (flash.id)}
      {@const tone = flash.tone ?? "neutral"}
      {@const ToneIcon = toneIcons[tone]}
      <div class="notification" data-notification-tone={tone}>
        <span class="notification__icon" aria-hidden="true">
          <ToneIcon size={16} strokeWidth={1.8} />
        </span>
        <span class="notification__message">
          {#if toneLabels[tone]}<span class="kit-sr-only"
              >{toneLabels[tone]}:
            </span>{/if}{flash.message}
        </span>
        <IconButton
          size="sm"
          class="notification__dismiss"
          ariaLabel="Dismiss notification"
          onclick={() => dismissFlash(flash.id)}
          ><X size={14} strokeWidth={1.8} aria-hidden="true" /></IconButton
        >
      </div>
    {/each}
  </div>
</div>

<style>
  .notifications {
    position: fixed;
    z-index: var(--z-overlay);
    top: 64px;
    left: 50%;
    transform: translateX(-50%);
    width: 384px;
    max-width: calc(100vw - 32px);
    pointer-events: none;
  }

  .notifications__stack {
    box-sizing: content-box;
    display: flex;
    flex-direction: column;
    gap: 8px;
    max-height: calc(100dvh - 80px);
    overflow-y: auto;
    overscroll-behavior: contain;
    /* Keep the floating shadows inside the scroll area without changing
     * the cards' width or their 64px offset from the viewport top. */
    margin: -24px;
    padding: 24px;
  }

  .notification {
    box-sizing: border-box;
    display: flex;
    align-items: center;
    flex: none;
    gap: 8px;
    min-height: 49px;
    padding: 11px 12px;
    border: 1px solid var(--border-notification, var(--border-default));
    border-radius: 12px;
    background: var(--bg-notification, var(--bg-surface));
    color: var(--text-primary);
    box-shadow: var(--shadow-md);
    font-size: 13px;
    font-weight: 450;
    line-height: 20px;
    pointer-events: auto;
  }

  .notification__icon {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    flex: none;
    width: 16px;
    height: 16px;
    color: var(--text-muted);
  }

  .notification__message {
    flex: 1;
    min-width: 0;
    overflow-wrap: anywhere;
  }

  .notification :global(.kit-icon-button.notification__dismiss) {
    width: 16px;
    height: 24px;
    min-height: 24px;
    margin-left: 2px;
    color: var(--text-muted);
  }

  .notification :global(.kit-icon-button.notification__dismiss:hover) {
    background: var(--bg-notification-action, var(--bg-surface-hover));
    color: var(--text-primary);
  }
</style>
