// Shared modal behaviour for the full-window settings panes and overlays:
//   - Escape closes (calls opts.onclose)
//   - marks the node aria-modal and gives it a focus target
//   - moves focus into the dialog on mount, restores it to the opener on destroy
//
// Usage:  <div class="modal" use:modal={{ onclose }}> … </div>
// window.confirm() is a no-op in the Wails webview, so closing is always routed
// through the supplied onclose (which may itself run an in-app confirm first).

type ModalOpts = { onclose: () => void };

export function modal(node: HTMLElement, opts: ModalOpts) {
  let current = opts;
  const opener = (document.activeElement as HTMLElement) ?? null;

  node.setAttribute("aria-modal", "true");
  if (!node.getAttribute("role")) node.setAttribute("role", "dialog");

  function onKey(e: KeyboardEvent) {
    if (e.key === "Escape") {
      e.stopPropagation();
      current.onclose();
    }
  }
  node.addEventListener("keydown", onKey);

  // Focus the first natural control, else the dialog itself.
  const focusable = node.querySelector<HTMLElement>(
    'input, textarea, button, [href], select, [tabindex]:not([tabindex="-1"])',
  );
  if (focusable) {
    setTimeout(() => focusable.focus(), 40);
  } else {
    node.tabIndex = -1;
    setTimeout(() => node.focus(), 40);
  }

  return {
    update(next: ModalOpts) {
      current = next;
    },
    destroy() {
      node.removeEventListener("keydown", onKey);
      try {
        opener?.focus?.();
      } catch (_) {}
    },
  };
}
