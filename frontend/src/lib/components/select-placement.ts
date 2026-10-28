/** Position the existing kit list around its selected row. Selection, focus,
 * keyboard navigation, and dismissal remain owned by SelectDropdown. */
export function selectedItemPlacement(container: HTMLElement): { destroy(): void } {
  let activePanel: HTMLElement | null = null;
  let activeTrigger: HTMLElement | null = null;
  let frame: number | undefined;
  const optionMethods = new Map<HTMLElement, {
    original: PropertyDescriptor | undefined;
    scoped: HTMLElement["scrollIntoView"];
  }>();

  function restoreOption(option: HTMLElement): void {
    const method = optionMethods.get(option);
    if (!method) return;
    if (option.scrollIntoView === method.scoped) {
      if (method.original) Object.defineProperty(option, "scrollIntoView", method.original);
      else Reflect.deleteProperty(option, "scrollIntoView");
    }
    optionMethods.delete(option);
  }

  function setLength(name: string, value: number): void {
    const length = `${value}px`;
    if (container.style.getPropertyValue(name) !== length) container.style.setProperty(name, length);
  }

  function clamp(value: number, min: number, max: number): number {
    return Math.max(min, Math.min(value, max));
  }

  function position(): void {
    const trigger = container.querySelector<HTMLElement>('[role="combobox"]');
    const panel = container.querySelector<HTMLElement>('[role="listbox"]');
    if (trigger !== activeTrigger) {
      if (activeTrigger) resize.unobserve(activeTrigger);
      activeTrigger = trigger;
      if (trigger) resize.observe(trigger);
    }

    const opening = panel !== activePanel;
    if (opening) {
      if (activePanel) resize.unobserve(activePanel);
      for (const option of optionMethods.keys()) restoreOption(option);
      activePanel = panel;
      if (panel) resize.observe(panel);
    }
    if (!trigger || !panel) return;

    // Native scrollIntoView also scrolls the sidebar and document ancestors.
    // Limit kit's opening and keyboard scrolls to this list, then restore the
    // original methods when its options leave the DOM or the action ends.
    for (const option of optionMethods.keys()) {
      if (!panel.contains(option)) restoreOption(option);
    }
    const panelStyle = getComputedStyle(panel);
    const paddingTop = Number.parseFloat(panelStyle.paddingTop) || 0;
    const paddingBottom = Number.parseFloat(panelStyle.paddingBottom) || 0;
    for (const option of panel.querySelectorAll<HTMLElement>('[role="option"]')) {
      if (optionMethods.has(option)) continue;
      const scoped: HTMLElement["scrollIntoView"] = () => {
        const top = option.offsetTop;
        const bottom = top + option.offsetHeight;
        if (top < panel.scrollTop + paddingTop) panel.scrollTop = top - paddingTop;
        else if (bottom > panel.scrollTop + panel.clientHeight - paddingBottom) {
          panel.scrollTop = bottom - panel.clientHeight + paddingBottom;
        }
      };
      optionMethods.set(option, {
        original: Object.getOwnPropertyDescriptor(option, "scrollIntoView"),
        scoped,
      });
      option.scrollIntoView = scoped;
    }

    const zoom = panel.currentCSSZoom || 1;
    const viewport = window.visualViewport;
    const viewportLeft = (viewport?.offsetLeft ?? 0) / zoom;
    const viewportTop = (viewport?.offsetTop ?? 0) / zoom;
    const viewportWidth = (viewport?.width ?? window.innerWidth) / zoom;
    const viewportHeight = (viewport?.height ?? window.innerHeight) / zoom;
    const maxWidth = Math.max(0, viewportWidth - 16);
    const maxHeight = Math.max(0, viewportHeight - 16);
    const rect = trigger.getBoundingClientRect();
    setLength("--nb-select-minwidth", Math.min(rect.width / zoom + 8, maxWidth));
    setLength("--nb-select-maxwidth", maxWidth);
    setLength("--nb-select-maxheight", maxHeight);

    const selected = panel.querySelector<HTMLElement>('[aria-selected="true"]');
    const width = panel.getBoundingClientRect().width / zoom;
    const height = panel.getBoundingClientRect().height / zoom;
    const selectedCenter = (selected?.offsetTop ?? paddingTop) + (selected?.offsetHeight ?? 32) / 2;
    const triggerCenter = (rect.top + rect.height / 2) / zoom;
    const minTop = viewportTop + 8;
    const maxTop = Math.max(minTop, viewportTop + viewportHeight - height - 8);

    if (opening) {
      const centeredTop = clamp(triggerCenter - height / 2, minTop, maxTop);
      panel.scrollTop = clamp(selectedCenter - triggerCenter + centeredTop, 0, panel.scrollHeight - panel.clientHeight);
    }
    setLength("--nb-select-top", clamp(triggerCenter - selectedCenter + panel.scrollTop, minTop, maxTop));
    setLength("--nb-select-left", clamp(rect.left / zoom - 1.5, viewportLeft + 8,
      Math.max(viewportLeft + 8, viewportLeft + viewportWidth - width - 8)));
  }

  function schedulePosition(): void {
    if (frame !== undefined) return;
    frame = requestAnimationFrame(() => {
      frame = undefined;
      position();
    });
  }

  const resize = new ResizeObserver(schedulePosition);
  resize.observe(container);
  const mutations = new MutationObserver(position);
  mutations.observe(container, {
    childList: true,
    characterData: true,
    subtree: true,
    attributes: true,
    attributeFilter: ["aria-selected"],
  });
  const onScroll = (event: Event): void => {
    if (event.target !== activePanel) schedulePosition();
  };
  window.addEventListener("resize", schedulePosition);
  window.addEventListener("scroll", onScroll, true);
  window.visualViewport?.addEventListener("resize", schedulePosition);
  window.visualViewport?.addEventListener("scroll", schedulePosition);
  position();

  return {
    destroy(): void {
      mutations.disconnect();
      resize.disconnect();
      if (frame !== undefined) cancelAnimationFrame(frame);
      window.removeEventListener("resize", schedulePosition);
      window.removeEventListener("scroll", onScroll, true);
      window.visualViewport?.removeEventListener("resize", schedulePosition);
      window.visualViewport?.removeEventListener("scroll", schedulePosition);
      for (const option of optionMethods.keys()) restoreOption(option);
      for (const property of ["top", "left", "minwidth", "maxwidth", "maxheight"]) {
        container.style.removeProperty(`--nb-select-${property}`);
      }
    },
  };
}
