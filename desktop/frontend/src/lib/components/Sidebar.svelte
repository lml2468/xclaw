<script lang="ts">
  import { store } from "../store.svelte";
  import Octopus from "./Octopus.svelte";

  let { onedit }: { onedit: () => void } = $props();
</script>

<div class="sidebar">
  <div class="identity" style="--wails-draggable: drag;">
    <span class="mark"><Octopus size={22} /></span>
    <span class="word">XClaw</span>
    <button class="gear" style="--wails-draggable: no-drag;" onclick={onedit} title="Edit bots" aria-label="Edit bots">
      <svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="3"/><path d="M19.4 15a1.65 1.65 0 0 0 .33 1.82l.06.06a2 2 0 1 1-2.83 2.83l-.06-.06a1.65 1.65 0 0 0-1.82-.33 1.65 1.65 0 0 0-1 1.51V21a2 2 0 0 1-4 0v-.09A1.65 1.65 0 0 0 8 19.4a1.65 1.65 0 0 0-1.82.33l-.06.06a2 2 0 1 1-2.83-2.83l.06-.06a1.65 1.65 0 0 0 .33-1.82 1.65 1.65 0 0 0-1.51-1H2a2 2 0 0 1 0-4h.09A1.65 1.65 0 0 0 3.6 8a1.65 1.65 0 0 0-.33-1.82l-.06-.06a2 2 0 1 1 2.83-2.83l.06.06a1.65 1.65 0 0 0 1.82.33H8a1.65 1.65 0 0 0 1-1.51V2a2 2 0 0 1 4 0v.09a1.65 1.65 0 0 0 1 1.51 1.65 1.65 0 0 0 1.82-.33l.06-.06a2 2 0 1 1 2.83 2.83l-.06.06a1.65 1.65 0 0 0-.33 1.82V8a1.65 1.65 0 0 0 1.51 1H22a2 2 0 0 1 0 4h-.09a1.65 1.65 0 0 0-1.51 1z"/></svg>
    </button>
  </div>

  <div class="section">Bots</div>
  <div class="list">
    {#if store.bots.length === 0}
      <div class="empty">No bots configured</div>
    {/if}
    {#each store.bots as b (b.id)}
      <button class="bot" class:sel={b.id === store.selectedBotId} onclick={() => store.selectBot(b.id)}>
        <span class="dot" class:on={b.connected} title={b.connected ? "connected" : "offline"}></span>
        <span class="meta">
          <span class="id">{b.id}</span>
          <span class="status">{b.connected ? "connected" : (b.lastError ?? "offline")}</span>
        </span>
      </button>
    {/each}
  </div>
</div>

<style>
  .sidebar { display: flex; flex-direction: column; height: 100%; }
  .identity {
    display: flex; align-items: center; gap: 9px;
    padding: 30px 14px 14px; color: var(--brand);
    border-bottom: 1px solid var(--hairline);
  }
  .mark { display: inline-flex; }
  .word { font-family: var(--serif); font-size: 18px; font-weight: 600; color: var(--ink); flex: 1; letter-spacing: 0.2px; }
  .gear { display: inline-flex; background: none; border: none; color: var(--ink-faint); padding: 4px; border-radius: 7px; transition: color 0.15s ease, background 0.15s ease; }
  .gear:hover { color: var(--brand); background: color-mix(in srgb, var(--ink) 6%, transparent); }

  .section {
    font-size: 10px; font-weight: 600; letter-spacing: 1px; text-transform: uppercase;
    color: var(--ink-faint); padding: 16px 16px 7px;
  }
  .list { display: flex; flex-direction: column; gap: 3px; padding: 0 8px; overflow-y: auto; }
  .empty { color: var(--ink-faint); font-size: 12px; padding: 6px 10px; }

  .bot {
    position: relative;
    display: flex; align-items: center; gap: 11px;
    width: 100%; text-align: left; background: transparent; border: none;
    padding: 9px 11px; border-radius: 11px; color: var(--ink);
    transition: background 0.14s ease;
  }
  .bot:hover { background: color-mix(in srgb, var(--ink) 6%, transparent); }
  .bot.sel { background: color-mix(in srgb, var(--brand) 15%, transparent); }
  /* left accent bar on the selected bot */
  .bot.sel::before {
    content: ""; position: absolute; left: 3px; top: 9px; bottom: 9px; width: 3px;
    border-radius: 2px; background: var(--brand);
  }

  .dot {
    width: 9px; height: 9px; border-radius: 50%; flex: 0 0 9px;
    background: var(--ink-faint);
    box-shadow: 0 0 0 3px color-mix(in srgb, var(--ink-faint) 18%, transparent);
  }
  .dot.on {
    background: #5aa873;
    box-shadow: 0 0 0 3px color-mix(in srgb, #5aa873 22%, transparent), 0 0 7px color-mix(in srgb, #5aa873 70%, transparent);
  }

  .meta { display: flex; flex-direction: column; min-width: 0; gap: 1px; }
  .id { font-weight: 600; font-size: 13px; line-height: 1.2; }
  .bot.sel .id { color: var(--brand-strong); }
  .status { font-size: 11px; color: var(--ink-soft); line-height: 1.2; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
</style>
