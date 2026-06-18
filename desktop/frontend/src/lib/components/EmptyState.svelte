<script lang="ts">
  import XMark from "./XMark.svelte";

  let { onpick }: { onpick: (prompt: string) => void } = $props();

  const prompts = [
    "What can you help me with?",
    "Summarize the latest messages in this channel.",
    "Draft a concise status update for the team.",
  ];
</script>

<div class="empty">
  <div class="hero" aria-hidden="true"><XMark size={40} /></div>
  <h1>Talk to your agent</h1>
  <p class="sub">Ask anything below, or start with one of these.</p>
  <div class="chips">
    {#each prompts as p}
      <button class="chip" onclick={() => onpick(p)}>{p}</button>
    {/each}
  </div>
</div>

<style>
  .empty {
    width: 100%; max-width: 400px; margin: 0 auto; min-height: 62vh;
    display: flex; flex-direction: column; align-items: center; justify-content: center;
    text-align: center; gap: 12px;
  }
  .hero {
    width: 72px; height: 72px; border-radius: 18px;
    display: grid; place-items: center; color: #fff;
    background: linear-gradient(135deg, var(--grad-a), var(--grad-b));
    box-shadow: 0 8px 24px color-mix(in srgb, var(--grad-a) 40%, transparent), inset 0 0 0 0.5px rgba(255, 255, 255, 0.14);
  }
  h1 { font-size: 22px; font-weight: 600; letter-spacing: -0.2px; margin: 6px 0 0; }
  .sub { color: var(--ink-soft); margin: 0; font-size: 13px; }
  .chips { display: flex; flex-direction: column; align-items: stretch; gap: 6px; width: 100%; margin-top: 8px; }
  .chip {
    width: 100%; text-align: left; padding: 10px 14px; font-size: 13px;
    background: color-mix(in srgb, var(--ink) 5%, transparent); color: var(--ink);
    border: 1px solid var(--hairline); border-radius: 4px;
    transition: background 0.14s ease, border-color 0.14s ease, transform 0.12s ease;
  }
  .chip:hover {
    background: color-mix(in srgb, var(--accent) 12%, transparent);
    border-color: color-mix(in srgb, var(--accent) 40%, transparent);
    color: var(--accent-strong); transform: translateY(-1px);
  }
</style>
