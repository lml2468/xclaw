<script lang="ts">
  // ⌘K command palette — search Spaces (bots) + conversations and jump, or run
  // a command (open settings / skills / workflows / token usage). Wired to the
  // real store; no mock data.
  import { store } from "../store.svelte";
  import Avatar from "./Avatar.svelte";

  let { onclose, onedit, onskills, onworkflows, onusage }:
    { onclose: () => void; onedit: () => void; onskills: () => void; onworkflows: () => void; onusage: () => void } = $props();

  let q = $state("");
  let input: HTMLInputElement;

  $effect(() => { setTimeout(() => input?.focus(), 40); });

  const ql = $derived(q.trim().toLowerCase());
  const bots = $derived(store.bots.filter((b) => !ql || b.id.toLowerCase().includes(ql)));
  const sessions = $derived(
    store.sessions
      .filter((s) => !ql || s.title.toLowerCase().includes(ql) || (s.preview ?? "").toLowerCase().includes(ql))
      .slice(0, 6),
  );
  const commands = $derived(
    [
      { label: "编辑 Bot", run: onedit },
      { label: "管理技能", run: onskills },
      { label: "管理工作流", run: onworkflows },
      { label: "Token 用量", run: onusage },
    ].filter((c) => !ql || c.label.toLowerCase().includes(ql)),
  );

  function gotoBot(id: string) { store.selectBot(id); onclose(); }
  function gotoSession(botId: string, key: string) { if (store.selectedBotId !== botId) store.selectBot(botId); store.selectSession(key); onclose(); }
  function runCmd(fn: () => void) { onclose(); fn(); }
  function onScrimKey(e: KeyboardEvent) { if (e.key === "Escape") onclose(); }
</script>

<div class="scrim" onclick={onclose} onkeydown={onScrimKey} role="presentation">
  <div class="palette" role="dialog" aria-label="命令面板" onclick={(e) => e.stopPropagation()}>
    <div class="pinput">
      <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" style="color:var(--ink-faint)"><circle cx="11" cy="11" r="7"/><path d="m21 21-4.3-4.3"/></svg>
      <input bind:this={input} bind:value={q} placeholder="搜索会话、切换 Space 或执行命令…" />
      <span class="esc">esc</span>
    </div>
    <div class="plist">
      {#if bots.length}
        <div class="psec">Spaces</div>
        {#each bots as b (b.id)}
          <button class="pitem" onclick={() => gotoBot(b.id)}>
            <Avatar name={b.id} size={24} />
            <span class="pl">{b.id}</span>
            <span class="ph">{b.connected ? "在线" : "离线"}</span>
          </button>
        {/each}
      {/if}
      {#if sessions.length}
        <div class="psec">会话</div>
        {#each sessions as s (s.botId + s.key)}
          <button class="pitem" onclick={() => gotoSession(s.botId, s.key)}>
            <Avatar name={s.title} size={24} />
            <span class="pl">{s.title}</span>
            <span class="ph">{s.botId}</span>
          </button>
        {/each}
      {/if}
      {#if commands.length}
        <div class="psec">命令</div>
        {#each commands as c (c.label)}
          <button class="pitem" onclick={() => runCmd(c.run)}>
            <span class="cmd-ico"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><path d="m9 18 6-6-6-6"/></svg></span>
            <span class="pl">{c.label}</span>
          </button>
        {/each}
      {/if}
      {#if !bots.length && !sessions.length && !commands.length}
        <div class="psec">无匹配结果</div>
      {/if}
    </div>
  </div>
</div>

<style>
  .scrim { position: fixed; inset: 0; z-index: 200; background: color-mix(in srgb, var(--grad-b) 45%, transparent); backdrop-filter: blur(8px); -webkit-backdrop-filter: blur(8px); display: grid; place-items: start center; padding-top: 14vh; }
  .palette { width: min(580px, 90vw); background: var(--glass-active, var(--surface)); backdrop-filter: blur(40px) saturate(180%); -webkit-backdrop-filter: blur(40px) saturate(180%); border: 1px solid var(--glass-border); border-radius: 16px; box-shadow: 0 24px 60px rgba(0,0,0,0.22); overflow: hidden; }
  .pinput { display: flex; align-items: center; gap: 12px; padding: 16px 18px; border-bottom: 1px solid var(--border-soft, var(--hairline)); }
  .pinput svg { width: 20px; height: 20px; flex: 0 0 auto; }
  .pinput input { flex: 1; border: none; outline: none; background: none; font-size: 18px; color: var(--ink); }
  .pinput input::placeholder { color: var(--ink-faint); }
  .esc { font-family: var(--mono); font-size: 11px; color: var(--ink-faint); background: color-mix(in srgb, var(--ink) 6%, transparent); padding: 2px 7px; border-radius: 6px; }
  .plist { max-height: 340px; overflow-y: auto; padding: 8px; }
  .psec { font-size: 11px; font-weight: 600; text-transform: uppercase; letter-spacing: 0.05em; color: var(--ink-faint); padding: 8px 10px 4px; }
  .pitem { display: flex; align-items: center; gap: 12px; width: 100%; text-align: left; padding: 9px 10px; border: none; background: transparent; border-radius: 8px; color: var(--ink); transition: background .12s ease; }
  .pitem:hover { background: color-mix(in srgb, var(--accent) 10%, transparent); }
  .cmd-ico { width: 24px; height: 24px; flex: 0 0 24px; border-radius: 7px; display: grid; place-items: center; background: color-mix(in srgb, var(--accent) 12%, transparent); color: var(--accent-strong, var(--accent)); }
  .cmd-ico svg { width: 14px; height: 14px; }
  .pl { flex: 1; font-size: 14px; }
  .ph { font-family: var(--mono); font-size: 11px; color: var(--ink-faint); }
</style>
