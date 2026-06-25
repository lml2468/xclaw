<script lang="ts">
 // Basic Info pane: identity (Bot ID), model + gateway (URL/token), and the
 // persona/behavior prompts (SOUL/AGENTS). The power-user knobs (system-prompt
 // mode, setting sources, bot-level tools, env vars, MCP servers) live in the
 // sibling Runtime 信息 (RuntimeInfoPane). Octo-specific fields (apiUrl, octoToken,
 // OCTO_BOT_ID) live in the Octo 集成 pane.
  import type { BotConfig } from "../../../bindings/github.com/lml2468/octobuddy/desktop/internal/configstore/models";

  let { bot = $bindable<BotConfig>(), ondirty, ondelete }:
    { bot: BotConfig; ondirty: () => void; ondelete: () => void } = $props();
</script>

<div class="pane" oninput={ondirty} onchange={ondirty}>
  <label>Bot ID <input bind:value={bot.id} placeholder="my-bot" /></label>

  <fieldset>
    <legend>模型 &amp; 网关</legend>
    <label><span class="lbl">模型 <span class="req">*</span></span>
      <input bind:value={bot.model} placeholder="claude-opus-4-8" aria-required="true" />
    </label>
    <label>网关地址 <input bind:value={bot.gatewayBaseUrl} placeholder="https://gateway/v1" /></label>
    <label>网关 Token
      <input type="password" bind:value={bot.gatewayToken} placeholder="sk-…" />
      <small>存于系统钥匙串，绝不写入 config.json。网关地址/Token 可选——无独立网关时用环境变量 ANTHROPIC_* 即可。</small>
    </label>
  </fieldset>

  <fieldset>
    <legend>SOUL.md（身份）</legend>
    <textarea bind:value={bot.soul} rows="4" placeholder="身份、语气、价值观、底线。留空将写入带「Core Truths / Boundaries / Vibe」分节的默认模板。"></textarea>
  </fieldset>

  <fieldset>
    <legend>AGENTS.md（行为规范）</legend>
    <textarea bind:value={bot.agents} rows="4" placeholder="行为规范与红线。留空将写入带「红线 / 群聊何时发言 / 不可信输入」分节的默认模板。"></textarea>
  </fieldset>

  <div class="danger-row">
    <button class="remove" onclick={ondelete}>删除此 Bot</button>
    <small>下次「保存并重启」时该 Bot 的所有配置与会话数据会被一并清除。</small>
  </div>
</div>

<style>
  .pane { display: flex; flex-direction: column; gap: 14px; }
  label { display: flex; flex-direction: column; gap: 5px; font-size: 12px; font-weight: 550; color: var(--ink-soft); }
  fieldset { border: 1px solid var(--hairline); border-radius: 12px; padding: 14px 16px; display: flex; flex-direction: column; gap: 12px; margin: 0; }
  legend { font-size: 11px; font-weight: 600; color: var(--ink-soft); text-transform: uppercase; letter-spacing: 0.04em; padding: 0 6px; }
  .req { color: var(--danger); }
  .lbl { display: inline-flex; align-items: baseline; gap: 3px; }
  input, textarea { background: color-mix(in srgb, var(--ink) 4%, var(--surface)); border: 1px solid var(--hairline); border-radius: 10px; padding: 8px 11px; color: var(--ink); font-size: 13px; outline: none; transition: border-color .15s ease, box-shadow .15s ease; }
  input:focus, textarea:focus { border-color: color-mix(in srgb, var(--accent) 55%, var(--hairline)); box-shadow: 0 0 0 3px color-mix(in srgb, var(--accent) 16%, transparent); }
  textarea { resize: vertical; font-family: var(--ui); }
  small { color: var(--ink-faint); font-size: 11px; }

  .danger-row { display: flex; align-items: center; gap: 14px; padding-top: 4px; }
  .remove { color: var(--danger); background: transparent; border: 1px solid color-mix(in srgb, var(--danger) 40%, var(--hairline)); border-radius: 6px; padding: 6px 14px; font-size: 12px; font-weight: 550; flex: 0 0 auto; }
  .remove:hover { background: color-mix(in srgb, var(--danger) 10%, transparent); }
  .remove:focus-visible { outline: none; box-shadow: 0 0 0 3px color-mix(in srgb, var(--accent) 30%, transparent); }
</style>
