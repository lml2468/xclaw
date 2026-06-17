<script lang="ts">
  import type { Message } from "../store.svelte";
  import { renderMarkdown, onMarkdownCopyClick } from "../markdown";
  import Avatar from "./Avatar.svelte";
  import Typewriter from "./Typewriter.svelte";

  let { message }: { message: Message } = $props();

  const isUser = $derived(message.role === "user");
  const isTool = $derived(message.role === "tool");
  const html = $derived(!isUser && !isTool && !message.streaming ? renderMarkdown(message.text) : "");

  function copy() { navigator.clipboard?.writeText(message.text); }
</script>

{#if isTool}
  <div class="tool" title={message.text}>
    <span class="dot"></span>{message.text}
  </div>
{:else}
  <div class="row" class:user={isUser}>
    <span class="av">
      {#if isUser}<Avatar name="You" size={36} />{:else}<Avatar octopus size={36} />{/if}
    </span>
    <div class="bubble" class:user={isUser} oncontextmenu={(e) => { e.preventDefault(); copy(); }} role="article">
      {#if isUser}
        <span class="plain">{message.text}</span>
      {:else if message.streaming}
        <Typewriter text={message.text} />
      {:else}
        <div class="md" onclick={onMarkdownCopyClick} role="presentation">{@html html}</div>
      {/if}
    </div>
  </div>
{/if}

<style>
  .row { display: flex; gap: 10px; align-items: flex-start; max-width: 100%; animation: rise 0.28s cubic-bezier(0.2, 0.7, 0.2, 1) both; }
  .row.user { flex-direction: row-reverse; }
  @keyframes rise { from { opacity: 0; transform: translateY(5px); } to { opacity: 1; transform: none; } }
  @media (prefers-reduced-motion: reduce) { .row { animation: none; } }

  .av { flex: 0 0 auto; margin-top: 1px; }

  .bubble {
    max-width: 74%;
    padding: 9px 13px;
    font-size: 14px; line-height: 1.5;
    background: var(--in-bubble); color: var(--in-ink);
    border-radius: var(--bubble-radius);
    border-top-left-radius: 3px;
    border: 1px solid var(--bubble-border);
    box-shadow: var(--elev-1);
  }
  .bubble.user {
    background: var(--out-bubble); color: var(--out-ink);
    border: 1px solid color-mix(in srgb, var(--out-bubble) 65%, #000 14%);
    border-top-left-radius: var(--bubble-radius);
    border-top-right-radius: 3px;
    box-shadow: 0 2px 10px color-mix(in srgb, var(--accent) 22%, transparent), var(--elev-1);
  }
  .plain { white-space: pre-wrap; word-break: break-word; }

  .tool {
    align-self: center; margin: 3px auto;
    display: inline-flex; align-items: center; gap: 6px;
    font-size: 11px; color: var(--ink-soft);
    background: color-mix(in srgb, var(--ink) 6%, transparent);
    padding: 4px 11px; border-radius: 4px;
    max-width: 80%; overflow: hidden; text-overflow: ellipsis; white-space: nowrap;
  }
  .tool .dot { width: 5px; height: 5px; border-radius: 50%; background: var(--accent); flex: 0 0 auto; }

  /* Markdown */
  .md :global(p) { margin: 0 0 7px; }
  .md :global(p:last-child) { margin-bottom: 0; }
  .md :global(code) { font-family: var(--mono); font-size: 0.88em; }
  .md :global(:not(pre) > code) { background: color-mix(in srgb, var(--ink) 8%, transparent); padding: 1px 5px; border-radius: 4px; }
  .md :global(a) { color: var(--accent-strong); }
  .md :global(ul), .md :global(ol) { margin: 0 0 7px; padding-left: 20px; }
  .md :global(blockquote) { margin: 0 0 7px; padding-left: 11px; border-left: 3px solid var(--hairline-strong); color: var(--ink-soft); }
  /* Code-block + syntax-token rules are shared via lib/styles/markdown.css. */
</style>
