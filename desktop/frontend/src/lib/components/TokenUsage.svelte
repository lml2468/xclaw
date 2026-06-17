<script lang="ts">
  import { store } from "../store.svelte";
  import { onMount } from "svelte";

  let { onclose }: { onclose: () => void } = $props();

  // Fetch every bot's cumulative usage when the window opens (no-op in preview).
  onMount(() => store.loadUsage());

  const bots = $derived(store.bots);

  // Grand total across all bots (only bots that have reported usage contribute).
  const total = $derived.by(() => {
    const t = { inputTokens: 0, outputTokens: 0, cachedTokens: 0, costUsd: 0, turns: 0 };
    for (const b of bots) {
      const u = b.usage;
      if (!u) continue;
      t.inputTokens += u.inputTokens;
      t.outputTokens += u.outputTokens;
      t.cachedTokens += u.cachedTokens;
      t.costUsd += u.costUsd;
      t.turns += u.turns;
    }
    return t;
  });

  const anyUsage = $derived(bots.some((b) => b.usage && b.usage.turns > 0));

  // Compact number: 1_284_500 → "1.28M", 96_120 → "96.1K".
  function fmt(n: number): string {
    if (n < 1000) return String(n);
    if (n < 1_000_000) return `${(n / 1000).toFixed(1)}K`;
    if (n < 1_000_000_000) return `${(n / 1_000_000).toFixed(2)}M`;
    return `${(n / 1_000_000_000).toFixed(2)}B`;
  }
  function cost(n: number): string {
    return `$${n.toFixed(n < 1 ? 4 : 2)}`;
  }
</script>

<div class="scrim" onclick={onclose} role="presentation">
  <div class="modal" onclick={(e) => e.stopPropagation()} role="dialog" aria-label="Token usage">
    <header>
      <h2>Token Usage</h2>
      <button class="x" onclick={onclose} aria-label="Close">✕</button>
    </header>

    <div class="body">
      {#if bots.length === 0}
        <div class="empty">No bots configured.</div>
      {:else if !anyUsage}
        <div class="empty">No token usage recorded yet. Totals appear here once bots complete turns.</div>
      {:else}
        <!-- Grand total card -->
        <div class="total">
          <span class="tlabel">All bots</span>
          <div class="tstats">
            <div class="big"><span class="n">{fmt(total.inputTokens)}</span><span class="k">input</span></div>
            <div class="big"><span class="n">{fmt(total.outputTokens)}</span><span class="k">output</span></div>
            <div class="big"><span class="n">{fmt(total.cachedTokens)}</span><span class="k">cached</span></div>
            <div class="big"><span class="n">{cost(total.costUsd)}</span><span class="k">cost</span></div>
          </div>
        </div>

        <!-- Per-bot table -->
        <div class="tablewrap">
          <table>
            <thead>
              <tr>
                <th class="lcol">Bot</th>
                <th>Input</th>
                <th>Output</th>
                <th>Cached</th>
                <th>Cost</th>
                <th>Turns</th>
              </tr>
            </thead>
            <tbody>
              {#each bots as b (b.id)}
                <tr>
                  <td class="lcol">
                    <span class="dot" class:on={b.connected}></span>{b.id}
                  </td>
                  <td class="num">{b.usage ? fmt(b.usage.inputTokens) : "—"}</td>
                  <td class="num">{b.usage ? fmt(b.usage.outputTokens) : "—"}</td>
                  <td class="num">{b.usage ? fmt(b.usage.cachedTokens) : "—"}</td>
                  <td class="num">{b.usage ? cost(b.usage.costUsd) : "—"}</td>
                  <td class="num dim">{b.usage ? b.usage.turns : "—"}</td>
                </tr>
              {/each}
            </tbody>
          </table>
        </div>

        <p class="note">Cumulative across all completed turns, persisted per bot. “Cached” is the input served from the prompt cache.</p>
      {/if}
    </div>
  </div>
</div>

<style>
  .scrim { position: fixed; inset: 0; z-index: 50; background: color-mix(in srgb, var(--ink) 28%, transparent); display: grid; place-items: center; }
  .modal { width: min(720px, 92vw); height: min(560px, 88vh); display: flex; flex-direction: column; background: var(--surface); border: 1px solid var(--hairline); border-radius: var(--radius); box-shadow: var(--shadow-pop); overflow: hidden; }
  header { display: flex; align-items: center; padding: 16px 18px; border-bottom: 1px solid var(--hairline); }
  header h2 { font-size: 17px; flex: 1; }
  .x { background: none; border: none; color: var(--ink-soft); font-size: 15px; cursor: pointer; }

  .body { flex: 1; overflow-y: auto; padding: 18px; display: flex; flex-direction: column; gap: 16px; }
  .empty { color: var(--ink-faint); font-size: 13px; padding: 32px 8px; text-align: center; line-height: 1.5; }

  /* Grand-total card */
  .total {
    border: 1px solid var(--hairline); border-radius: 8px; padding: 16px 18px;
    background: color-mix(in srgb, var(--accent) 6%, var(--surface));
    display: flex; flex-direction: column; gap: 12px;
  }
  .tlabel { font-size: 12px; font-weight: 600; color: var(--accent-strong, var(--accent)); letter-spacing: 0.4px; text-transform: uppercase; }
  .tstats { display: grid; grid-template-columns: repeat(4, 1fr); gap: 12px; }
  .big { display: flex; flex-direction: column; gap: 2px; }
  .big .n { font-family: var(--mono); font-size: 22px; font-weight: 600; color: var(--ink); font-variant-numeric: tabular-nums; }
  .big .k { font-size: 11px; color: var(--ink-soft); }

  /* Per-bot table */
  .tablewrap { border: 1px solid var(--hairline); border-radius: 8px; overflow: hidden; }
  table { width: 100%; border-collapse: collapse; }
  th, td { text-align: right; padding: 9px 14px; font-size: 13px; }
  th { font-size: 11px; font-weight: 600; color: var(--ink-soft); text-transform: uppercase; letter-spacing: 0.3px; background: color-mix(in srgb, var(--ink) 3%, transparent); border-bottom: 1px solid var(--hairline); }
  tbody tr { border-bottom: 1px solid color-mix(in srgb, var(--hairline) 60%, transparent); }
  tbody tr:last-child { border-bottom: none; }
  tbody tr:hover { background: color-mix(in srgb, var(--ink) 3%, transparent); }
  .lcol { text-align: left; }
  td.lcol { font-size: 13px; color: var(--ink); display: flex; align-items: center; gap: 8px; }
  .num { font-family: var(--mono); color: var(--ink); font-variant-numeric: tabular-nums; }
  .num.dim { color: var(--ink-faint); }
  .dot { width: 7px; height: 7px; border-radius: 50%; background: var(--ink-faint); flex: 0 0 auto; }
  .dot.on { background: var(--accent); }

  .note { font-size: 11px; color: var(--ink-faint); line-height: 1.5; margin: 0; }
</style>
