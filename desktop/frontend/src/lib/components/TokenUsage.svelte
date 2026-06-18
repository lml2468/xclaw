<script lang="ts">
  import { store, type BotUsage } from "../store.svelte";
  import { onMount } from "svelte";
  import { modal } from "../actions/modal";
  import SettingsHeader from "./SettingsHeader.svelte";

  let { onclose, onedit, onskills, onworkflows }: { onclose: () => void; onedit?: () => void; onskills?: () => void; onworkflows?: () => void } = $props();

  // Range selector. `since` is Unix seconds at a LOCAL-midnight bound (0 = all
  // time), computed from the user's own calendar so "today" matches their tz.
  type RangeKey = "all" | "30d" | "7d" | "yesterday" | "today";
  const RANGES: { key: RangeKey; label: string }[] = [
    { key: "all", label: "全部" },
    { key: "30d", label: "30 天" },
    { key: "7d", label: "7 天" },
    { key: "yesterday", label: "昨天" },
    { key: "today", label: "今天" },
  ];
  let range = $state<RangeKey>("all");

  function midnight(daysAgo: number): number {
    const d = new Date();
    d.setHours(0, 0, 0, 0);
    d.setDate(d.getDate() - daysAgo);
    return Math.floor(d.getTime() / 1000);
  }
  // since/until for a range key. until (today's midnight) is only used by the
  // single-day "yesterday" window; 0 elsewhere.
  function boundsFor(k: RangeKey): { since: number; until: number } {
    switch (k) {
      case "today": return { since: midnight(0), until: 0 };
      case "yesterday": return { since: midnight(1), until: midnight(0) };
      case "7d": return { since: midnight(7), until: 0 };
      case "30d": return { since: midnight(30), until: 0 };
      default: return { since: 0, until: 0 };
    }
  }
  const bounds = $derived(boundsFor(range));

  // Fetch the active range for every bot on open, and on each range change. We
  // drive fetches imperatively (onMount + the selector's onclick) rather than via
  // $effect, because loadUsage writes bot.usage — an effect that also read it
  // would re-run itself (update-depth loop). For "yesterday" we also need the
  // today-bound bucket to subtract, so fetch both.
  function fetchRange(k: RangeKey) {
    const bnd = boundsFor(k);
    store.loadUsage(bnd.since);
    if (k === "yesterday") store.loadUsage(bnd.until); // today's bucket, to subtract
  }
  onMount(() => fetchRange(range));
  function pickRange(k: RangeKey) {
    range = k;
    fetchRange(k);
  }

  const bots = $derived(store.bots);
  const ZERO: BotUsage = { inputTokens: 0, outputTokens: 0, cachedTokens: 0, cacheWriteTokens: 0, costUsd: 0, turns: 0 };
  // Usage for the active range. "Yesterday" is the today-bound minus the
  // today-bucket (until = today's midnight), derived from two `since` queries.
  function usageFor(b: { usage?: Record<number, BotUsage> }): BotUsage {
    const at = (s: number) => b.usage?.[s];
    if (range === "yesterday") {
      const a = at(bounds.since), today = at(bounds.until);
      if (!a) return ZERO;
      return today ? sub(a, today) : a;
    }
    return at(bounds.since) ?? ZERO;
  }
  function sub(a: BotUsage, b: BotUsage): BotUsage {
    return {
      inputTokens: a.inputTokens - b.inputTokens,
      outputTokens: a.outputTokens - b.outputTokens,
      cachedTokens: a.cachedTokens - b.cachedTokens,
      cacheWriteTokens: a.cacheWriteTokens - b.cacheWriteTokens,
      costUsd: a.costUsd - b.costUsd,
      turns: a.turns - b.turns,
    };
  }

  // Grand total across all bots for the active range.
  const total = $derived.by(() => {
    const t = { inputTokens: 0, outputTokens: 0, cachedTokens: 0, cacheWriteTokens: 0, costUsd: 0, turns: 0 };
    for (const b of bots) {
      const u = usageFor(b);
      t.inputTokens += u.inputTokens;
      t.outputTokens += u.outputTokens;
      t.cachedTokens += u.cachedTokens;
      t.cacheWriteTokens += u.cacheWriteTokens;
      t.costUsd += u.costUsd;
      t.turns += u.turns;
    }
    return t;
  });

  const anyUsage = $derived(total.turns > 0);

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
  <div class="modal" use:modal={{ onclose }} onclick={(e) => e.stopPropagation()} role="dialog" aria-label="Token 用量">
    <SettingsHeader active="usage" {onclose} onnav={(fn) => { onclose(); fn(); }} {onedit} {onskills} {onworkflows}>
      {#snippet children()}
        <div class="range" role="tablist" aria-label="时间范围">
          {#each RANGES as r (r.key)}
            <button role="tab" aria-selected={range === r.key} class:on={range === r.key} onclick={() => pickRange(r.key)}>{r.label}</button>
          {/each}
        </div>
      {/snippet}
    </SettingsHeader>

    <div class="body">
      {#if bots.length === 0}
        <div class="empty">尚未配置 Bot</div>
      {:else if !anyUsage}
        <div class="empty">该区间暂无用量</div>
      {:else}
        <div class="wrap">
          <!-- Grand total card -->
          <div class="total">
            <span class="tlabel">全部 Bot</span>
            <div class="tstats">
              <div class="big"><span class="n">{fmt(total.inputTokens)}</span><span class="k">输入</span></div>
              <div class="big"><span class="n">{fmt(total.outputTokens)}</span><span class="k">输出</span></div>
              <div class="big"><span class="n">{fmt(total.cachedTokens)}</span><span class="k">缓存读</span></div>
              <div class="big"><span class="n">{fmt(total.cacheWriteTokens)}</span><span class="k">缓存写</span></div>
              <div class="big"><span class="n">{cost(total.costUsd)}</span><span class="k">费用</span></div>
            </div>
          </div>

          <!-- Per-bot table -->
          <div class="tablewrap">
            <table>
              <thead>
                <tr>
                  <th class="lcol">Bot</th>
                  <th>输入</th>
                  <th>输出</th>
                  <th>缓存读</th>
                  <th>缓存写</th>
                  <th>费用</th>
                  <th>轮次</th>
                </tr>
              </thead>
              <tbody>
                {#each bots as b (b.id)}
                  {@const u = usageFor(b)}
                  <tr>
                    <td class="lcol">
                      <span class="dot" class:on={b.connected}></span><span class="bid">{b.id}</span>
                    </td>
                    <td class="num">{fmt(u.inputTokens)}</td>
                    <td class="num">{fmt(u.outputTokens)}</td>
                    <td class="num">{fmt(u.cachedTokens)}</td>
                    <td class="num">{fmt(u.cacheWriteTokens)}</td>
                    <td class="num cost">{cost(u.costUsd)}</td>
                    <td class="num dim">{u.turns}</td>
                  </tr>
                {/each}
              </tbody>
            </table>
          </div>
        </div>

        <p class="note">每个 Bot 独立持久化、按天分桶。<strong>缓存读</strong>=从提示缓存命中(读取)的输入;<strong>缓存写</strong>=写入缓存的输入。费用为 agent 上报的总额。早于按天统计前的用量只在「全部」区间出现。</p>
      {/if}
    </div>
  </div>
</div>

<style>
  .scrim { position: fixed; inset: 0; z-index: 50; background: var(--window-grad); display: block; }
  .modal { width: 100%; height: 100%; position: relative; display: flex; flex-direction: column; background: var(--glass); backdrop-filter: blur(24px) saturate(180%); -webkit-backdrop-filter: blur(24px) saturate(180%); border: none; border-radius: 0; box-shadow: none; overflow: hidden; color: var(--ink); font-family: var(--ui); }

  /* Time-range selector (lives in the shared header via the children snippet). */
  .range { display: inline-flex; border: 1px solid var(--hairline); border-radius: 7px; overflow: hidden; }
  .range button {
    font-size: 12px; padding: 5px 11px; border: none; background: transparent;
    color: var(--ink-soft); cursor: pointer; border-right: 1px solid var(--hairline);
    transition: background 0.12s ease, color 0.12s ease;
  }
  .range button:last-child { border-right: none; }
  .range button:hover { background: color-mix(in srgb, var(--ink) 6%, transparent); }
  .range button.on { background: color-mix(in srgb, var(--accent) 16%, transparent); color: var(--accent-strong, var(--accent)); }
  .range button:focus-visible { outline: none; box-shadow: 0 0 0 3px color-mix(in srgb, var(--accent) 30%, transparent); }

  .body { flex: 1; overflow-y: auto; padding: 18px; display: flex; flex-direction: column; gap: 16px; }
  .wrap { width: 100%; max-width: 980px; margin: 0 auto; display: flex; flex-direction: column; gap: 16px; }
  .empty { color: var(--ink-faint); font-size: 13px; padding: 32px 8px; text-align: center; line-height: 1.5; margin: auto; }

  /* Grand-total card */
  .total {
    border: 1px solid var(--hairline); border-radius: 8px; padding: 16px 18px;
    background: color-mix(in srgb, var(--accent) 6%, var(--surface));
    display: flex; flex-direction: column; gap: 12px;
  }
  .tlabel { font-size: 12px; font-weight: 600; color: var(--accent-strong, var(--accent)); letter-spacing: 0.4px; text-transform: uppercase; }
  .tstats { display: grid; grid-template-columns: repeat(5, 1fr); gap: 12px; }
  .big { display: flex; flex-direction: column; gap: 2px; }
  .big .n { font-family: var(--mono); font-size: 20px; font-weight: 600; color: var(--ink); font-variant-numeric: tabular-nums; }
  .big .k { font-size: 11px; color: var(--ink-soft); }

  /* Per-bot table */
  .tablewrap { border: 1px solid var(--hairline); border-radius: 8px; overflow: hidden; }
  table { width: 100%; border-collapse: collapse; }
  th, td { text-align: right; padding: 9px 14px; font-size: 13px; }
  th { font-size: 11px; font-weight: 600; color: var(--ink-soft); text-transform: uppercase; letter-spacing: 0.3px; background: color-mix(in srgb, var(--ink) 7%, transparent); border-bottom: 1px solid var(--hairline); }
  tbody tr { border-bottom: 1px solid color-mix(in srgb, var(--hairline) 60%, transparent); }
  tbody tr:last-child { border-bottom: none; }
  tbody tr:hover { background: color-mix(in srgb, var(--ink) 3%, transparent); }
  .lcol { text-align: left; }
  td.lcol { font-size: 13px; color: var(--ink); display: flex; align-items: center; gap: 8px; max-width: 220px; }
  .bid { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .num { font-family: var(--mono); color: var(--ink); font-variant-numeric: tabular-nums; }
  .num.cost { min-width: 84px; }
  .num.dim { color: var(--ink-faint); }
  .dot { width: 7px; height: 7px; border-radius: 50%; background: var(--ink-faint); flex: 0 0 auto; }
  .dot.on { background: var(--accent); }

  .note { width: 100%; max-width: 980px; margin: auto auto 0; padding-top: 14px; border-top: 1px solid var(--hairline); font-size: 11px; color: var(--ink-faint); line-height: 1.5; }
</style>
