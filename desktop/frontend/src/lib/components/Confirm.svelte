<script lang="ts">
  // Small in-app confirm dialog (window.confirm is a no-op in the Wails webview).
  // Render conditionally; resolves via the two buttons.
  let { message, confirmLabel = "确认", cancelLabel = "取消", danger = false, onresult }:
    { message: string; confirmLabel?: string; cancelLabel?: string; danger?: boolean; onresult: (ok: boolean) => void } = $props();

  function onKey(e: KeyboardEvent) {
    if (e.key === "Escape") { e.stopPropagation(); onresult(false); }
    else if (e.key === "Enter") { e.stopPropagation(); onresult(true); }
  }
</script>

<div class="cscrim" role="presentation" onclick={() => onresult(false)} onkeydown={onKey}>
  <!-- svelte-ignore a11y_click_events_have_key_events (scrim's onkeydown handles keys; this onclick only stops propagation) -->
  <div class="confirm" role="alertdialog" aria-label={message} tabindex="-1" onclick={(e) => e.stopPropagation()}>
    <p>{message}</p>
    <div class="cbtns">
      <button onclick={() => onresult(false)}>{cancelLabel}</button>
      <button class="primary" class:danger onclick={() => onresult(true)}>{confirmLabel}</button>
    </div>
  </div>
</div>

<style>
  .cscrim { position: absolute; inset: 0; z-index: 60; background: color-mix(in srgb, var(--ink) 32%, transparent); backdrop-filter: blur(2px); display: grid; place-items: center; }
  .confirm { width: min(360px, 80%); background: var(--surface); border: 1px solid var(--hairline); border-radius: var(--radius); box-shadow: var(--shadow-pop); padding: 18px; }
  .confirm p { margin: 0 0 14px; font-size: 13px; color: var(--ink); line-height: 1.5; }
  .cbtns { display: flex; justify-content: flex-end; gap: 8px; }
  .cbtns button { padding: 7px 14px; border-radius: var(--radius-control); border: 1px solid var(--hairline); background: color-mix(in srgb, var(--ink) 4%, var(--surface)); color: var(--ink); font-size: 12px; transition: background .14s ease, box-shadow .14s ease; }
  .cbtns button:focus-visible { outline: none; box-shadow: 0 0 0 3px color-mix(in srgb, var(--accent) 30%, transparent); }
  .cbtns .primary { background: linear-gradient(135deg, var(--grad-a), var(--grad-b)); color: #fff; border: none; }
  .cbtns .primary.danger { background: var(--danger); }
</style>
