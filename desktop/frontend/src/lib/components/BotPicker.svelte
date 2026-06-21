<script lang="ts">
  // The bot picker shown in the SettingsHeader's children slot on the per-bot
  // panels (Skills, Workflows). Caller passes the roster + current id; the
  // panel keeps its own dirty-guard so onpick can opt out of an unsaved switch.
  let { value, bots, onpick }:
    { value: string | null; bots: { id: string }[]; onpick: (id: string) => void } = $props();
</script>

<label class="botpick">
  <span>Bot</span>
  <select {value} onchange={(e) => onpick((e.currentTarget as HTMLSelectElement).value)}>
    {#each bots as b (b.id)}
      <option value={b.id}>{b.id}</option>
    {/each}
  </select>
</label>

<style>
  .botpick { display: inline-flex; align-items: center; gap: 7px; font-size: 12px; color: var(--ink-soft); -webkit-app-region: no-drag; }
  .botpick select { background: color-mix(in srgb, var(--ink) 5%, var(--surface)); border: 1px solid var(--hairline); border-radius: 8px; padding: 5px 9px; color: var(--ink); font-size: 12px; font-family: var(--mono); outline: none; }
</style>
