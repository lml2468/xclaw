<script lang="ts">
 // Basic Info pane: identity (Bot ID, model), gateway (URL/token),
 // non-Octo env vars, and the persona/behavior prompts (SOUL/AGENTS).
 // Octo-specific fields (apiUrl, octoToken, OCTO_BOT_ID) live in the
 // sibling Octo 集成 pane.
 // Env editor: filter out the keys other panes manage. Keep a local pairs
 // array so the user can add/rename keys without weird proxy-mutation
 // gymnastics, then commit back to bot.env on every change. RESERVED_ENV_KEYS
 // is the single source of truth — see lib/reservedEnv.ts.
  import type { BotConfig } from "../../../bindings/github.com/lml2468/xclaw/desktop/internal/configstore/models";
  import { RESERVED_ENV_KEYS } from "../reservedEnv";

  let { bot = $bindable<BotConfig>(), ondirty, ondelete }:
    { bot: BotConfig; ondirty: () => void; ondelete: () => void } = $props();

 // Stable per-row id so Svelte's keyed each preserves DOM nodes when the
 // user deletes a row — without it, every subsequent row's <input> remounts
 // and the user's caret jumps out.
 //
 // `secret` is the UI flag: should this row's value be stored in the secret
 // backend with only a secretRef in config.json? `locked` means the backend has
 // an existing value that the GUI intentionally cannot read; unlocking clears
 // the field so the user can type a replacement.
  type Row = { id: number; k: string; v: string; secret: boolean; locked: boolean };
  let rowSeq = 0;
  function newRow(k: string, v: string, secret?: boolean, locked?: boolean): Row {
    rowSeq += 1;
    return { id: rowSeq, k, v, secret: secret ?? false, locked: locked ?? false };
  }
 // Seed once at mount. The parent (SettingsModal) wraps this pane in
 // `{#key sel}` so a bot switch remounts the whole component — no
 // reactive re-seed is needed, and reading bot.env reactively here
 // would re-fire on every keystroke in the Bot ID input (bot.id is
 // also a dep when read inside an effect with bot.env), silently
 // wiping any uncommitted blank env row.
  let rows = $state<Row[]>(
    Object.entries(bot.env ?? {})
      .filter(([k]) => !RESERVED_ENV_KEYS.has(k))
      .map(([k, v]) => newRow(k, String(v ?? ""), !!bot.secretEnv?.[k], !!bot.secretEnv?.[k] && !v))
  );
  function commitEnv() {
 // Preserve reserved keys (owned by other panes) by passing them through
 // unchanged; rebuild the free-form half from `rows`. Values are written
 // verbatim; secret rows are routed to the backend by SaveConfig and leave
 // only a secretRef in config.json.
    const next: { [k: string]: string } = {};
    const secretNext: { [k: string]: boolean } = {};
    for (const k of RESERVED_ENV_KEYS) {
      const v = bot.env?.[k];
      if (v !== undefined) next[k] = v;
      if (bot.secretEnv?.[k]) secretNext[k] = true;
    }
    for (const r of rows) {
      const k = r.k.trim();
      if (!k) continue;
      next[k] = r.locked ? "" : r.v;
      if (r.secret) secretNext[k] = true;
    }
    bot.env = next;
    bot.secretEnv = secretNext;
    ondirty();
  }
 // unlock starts an edit cycle on a sealed row: wipes the ciphertext (the
 // GUI deliberately cannot decrypt — the renderer's read access is
 // write-only by design) so the user can type a replacement. The next
 // Save stores the replacement in the secret backend.
  function unlock(i: number) {
    rows[i] = { ...rows[i], v: "", secret: true, locked: false };
    commitEnv();
  }
 // toggleSecret flips the row's secret flag. Plain → secret seals current
 // value on Save. Secret → plain on a locked row is a no-op (we can't read
 // the backend value); user should replace or delete the row.
  function toggleSecret(i: number) {
    const r = rows[i];
    if (!r.secret) {
      rows[i] = { ...r, secret: true, locked: false };
      commitEnv();
      return;
    }
    if (!r.locked) {
      rows[i] = { ...r, secret: false };
      commitEnv();
    }
  }
</script>

<div class="pane" oninput={ondirty} onchange={ondirty}>
  <div class="grid2">
    <label>Bot ID <input bind:value={bot.id} placeholder="my-bot" /></label>
    <label>模型 <input bind:value={bot.model} placeholder="claude-opus-4-8" /></label>
  </div>

  <fieldset>
    <legend>模型网关（可选）</legend>
    <label>网关地址 <input bind:value={bot.gatewayBaseUrl} placeholder="https://gateway/v1" /></label>
    <label>网关 Token
      <input type="password" bind:value={bot.gatewayToken} placeholder="sk-…" />
      <small>存于系统钥匙串，绝不写入 config.json。</small>
    </label>
  </fieldset>

  <fieldset>
    <legend>环境变量</legend>
    {#each rows as row, i (row.id)}
      <div class="envrow">
        <input class="k" bind:value={row.k} oninput={commitEnv} placeholder="KEY" aria-label="环境变量名" />
        <span>=</span>
        {#if row.secret && row.locked}
          <input class="v locked" value="••••••••" readonly aria-label="加密的环境变量值" />
          <button class="iconbtn" onclick={() => unlock(i)} title="替换为新值（无法查看现有值）" aria-label="替换">✎</button>
        {:else if row.secret}
          <input class="v" type="password" bind:value={row.v} oninput={commitEnv} placeholder="粘贴敏感值，保存后进入 secret backend" aria-label="待保存的敏感环境变量值" />
          <button class="iconbtn" onclick={() => toggleSecret(i)} title="改为明文" aria-label="改为明文">🔓</button>
        {:else}
          <input class="v" bind:value={row.v} oninput={commitEnv} placeholder="value" aria-label="环境变量值" />
          <button class="iconbtn" onclick={() => toggleSecret(i)} title="加密保存（敏感值如 Token）" aria-label="加密">🔒</button>
        {/if}
        <button class="del" onclick={() => { rows = rows.filter((_, x) => x !== i); commitEnv(); }} aria-label="删除">−</button>
      </div>
    {/each}
    <button class="add sm" onclick={() => { rows = [...rows, newRow("", "")]; }}>+ 添加变量</button>
    <small class="hint">🔒 敏感值进入 secret backend；config.json 只保存 secretRef。OCTO_BOT_ID 在「Octo 集成」中管理，不出现在这里。</small>
  </fieldset>

  <fieldset>
    <legend>SOUL.md（身份）</legend>
    <textarea bind:value={bot.soul} rows="4" placeholder="身份、语气、角色"></textarea>
  </fieldset>

  <fieldset>
    <legend>AGENTS.md（行为规范）</legend>
    <textarea bind:value={bot.agents} rows="4" placeholder="规范、可做与不可做"></textarea>
  </fieldset>

  <div class="danger-row">
    <button class="remove" onclick={ondelete}>删除此 Bot</button>
    <small>下次「保存并重启」时该 Bot 的所有配置与会话数据会被一并清除。</small>
  </div>
</div>

<style>
  .pane { display: flex; flex-direction: column; gap: 14px; }
  .grid2 { display: grid; grid-template-columns: 1fr 1fr; gap: 14px; }
  label { display: flex; flex-direction: column; gap: 5px; font-size: 12px; font-weight: 550; color: var(--ink-soft); }
  fieldset { border: 1px solid var(--hairline); border-radius: 12px; padding: 14px 16px; display: flex; flex-direction: column; gap: 12px; margin: 0; }
  legend { font-size: 11px; font-weight: 600; color: var(--ink-soft); text-transform: uppercase; letter-spacing: 0.04em; padding: 0 6px; }
  input, textarea { background: color-mix(in srgb, var(--ink) 4%, var(--surface)); border: 1px solid var(--hairline); border-radius: 10px; padding: 8px 11px; color: var(--ink); font-size: 13px; outline: none; transition: border-color .15s ease, box-shadow .15s ease; }
  input:focus, textarea:focus { border-color: color-mix(in srgb, var(--accent) 55%, var(--hairline)); box-shadow: 0 0 0 3px color-mix(in srgb, var(--accent) 16%, transparent); }
  input.locked { font-family: var(--mono); letter-spacing: 0.1em; color: var(--ink-faint); cursor: default; }
  textarea { resize: vertical; font-family: var(--ui); }
  small { color: var(--ink-faint); font-size: 11px; }
  small.hint { padding-top: 4px; }

  .envrow { display: flex; align-items: center; gap: 6px; }
  .envrow .k { width: 160px; font-family: var(--mono); font-size: 12px; }
  .envrow .v { flex: 1; }
  .iconbtn { width: 26px; height: 26px; border-radius: 8px; border: 1px solid var(--hairline); background: color-mix(in srgb, var(--ink) 4%, var(--surface)); color: var(--ink-soft); font-size: 12px; display: grid; place-items: center; transition: background .14s ease, color .14s ease; }
  .iconbtn:hover { background: color-mix(in srgb, var(--accent) 10%, transparent); color: var(--accent); }
  .del { width: 26px; height: 26px; border-radius: 8px; border: 1px solid var(--hairline); background: color-mix(in srgb, var(--ink) 4%, var(--surface)); color: var(--ink-soft); }
  .add { text-align: center; padding: 7px 10px; border: 1px dashed var(--hairline); background: transparent; border-radius: 9px; color: var(--ink-soft); }
  .add.sm { font-size: 12px; padding: 5px 8px; text-align: left; align-self: flex-start; }
  .add:hover { border-color: color-mix(in srgb, var(--accent) 45%, var(--hairline)); color: var(--accent-strong, var(--accent)); }

  .danger-row { display: flex; align-items: center; gap: 14px; padding-top: 4px; }
  .remove { color: var(--danger); background: transparent; border: 1px solid color-mix(in srgb, var(--danger) 40%, var(--hairline)); border-radius: 6px; padding: 6px 14px; font-size: 12px; font-weight: 550; flex: 0 0 auto; }
  .remove:hover { background: color-mix(in srgb, var(--danger) 10%, transparent); }
  .remove:focus-visible, .del:focus-visible, .add:focus-visible, .iconbtn:focus-visible { outline: none; box-shadow: 0 0 0 3px color-mix(in srgb, var(--accent) 30%, transparent); }
</style>
