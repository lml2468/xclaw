<script lang="ts">
  // Bottom-of-modal error bar shared by the per-bot settings panels. The host
  // owns the error state; this just renders + emits dismiss. Hidden when
  // `error` is empty so the host can include it unconditionally.
  let { error, onclear }: { error: string; onclear: () => void } = $props();
</script>

{#if error}
  <footer class="errbar">
    <span class="err">⚠️ {error}</span>
    <button class="dismiss" onclick={onclear} aria-label="清除">✕</button>
  </footer>
{/if}

<style>
  .errbar { display: flex; align-items: center; gap: 8px; padding: 8px 18px; border-top: 1px solid var(--hairline); background: color-mix(in srgb, var(--danger) 6%, var(--surface)); }
  .err { flex: 1; color: var(--danger); font-size: 12px; }
  .dismiss { width: 24px; height: 24px; display: grid; place-items: center; background: none; border: none; border-radius: 6px; color: var(--ink-soft); font-size: 11px; }
  .dismiss:hover { background: color-mix(in srgb, var(--ink) 8%, transparent); color: var(--ink); }
  .dismiss:focus-visible { outline: none; box-shadow: 0 0 0 3px color-mix(in srgb, var(--accent) 30%, transparent); }
</style>
