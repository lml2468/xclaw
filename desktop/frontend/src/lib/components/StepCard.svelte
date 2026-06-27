<script lang="ts">
 // StepCard renders the agent's process steps for one turn as a compact card —
 // a list of tool calls / thinking markers, each with a ✓ (done) or an animated
 // ◌ (running). Used two ways: LIVE in the Transcript while a turn is in flight
 // (steps stream in, the last one spins), and ATTACHED above an assistant bubble
 // after the turn (all steps done, persisted across reloads). Renders nothing
 // for an empty list so a step-less reply looks identical to before.
  import type { ProcStep } from "../store.svelte";

  let { steps, live = false }: { steps: ProcStep[]; live?: boolean } = $props();
</script>

{#if steps.length}
  <div class="card" class:live aria-label="处理步骤">
    {#each steps as step (step.id)}
      <div class="step" class:running={step.status === "running"}>
        {#if step.status === "done"}
          <svg class="ic ok" viewBox="0 0 16 16" width="14" height="14" aria-hidden="true">
            <path d="M3.5 8.5l3 3 6-7" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"/>
          </svg>
        {:else}
          <span class="ic spin" aria-label="进行中"></span>
        {/if}
        <span class="text" class:thinking={step.kind === "thinking"} title={step.text}>{step.text}</span>
      </div>
    {/each}
  </div>
{/if}

<style>
 /* The card sits left-aligned, sharing the bubble column's width cap when
    attached. A tinted surface + leading accent rail reads as "process", set
    apart from the answer bubble below it. */
  .card {
    display: flex;
    flex-direction: column;
    gap: 6px;
    padding: 10px 12px;
    background: color-mix(in srgb, var(--ink) 4%, var(--surface));
    border: 1px solid var(--hairline);
    border-left: 2px solid color-mix(in srgb, var(--accent) 55%, transparent);
    border-radius: 8px;
    box-shadow: var(--elev-1);
  }

  .step { display: flex; align-items: center; gap: 8px; min-width: 0; }

  .ic { flex: 0 0 auto; width: 14px; height: 14px; }
  .ic.ok { color: var(--success); }

 /* Running spinner — a small ring that rotates. The accent-tinted track with
    an accent top arc reads as "in progress". */
  .spin {
    display: inline-block;
    width: 12px; height: 12px;
    border-radius: 50%;
    border: 2px solid color-mix(in srgb, var(--accent) 25%, transparent);
    border-top-color: var(--accent);
    animation: spin 0.7s linear infinite;
  }

  .text {
    min-width: 0; flex: 1 1 auto;
    font-family: var(--mono);
    font-size: 12px; line-height: 1.4;
    color: var(--ink-soft);
    white-space: nowrap; overflow: hidden; text-overflow: ellipsis;
  }
 /* Thinking markers aren't code — render them as soft italic prose, not mono. */
  .text.thinking { font-family: inherit; font-style: italic; color: var(--ink-faint); }

  @keyframes spin { to { transform: rotate(360deg); } }
  @media (prefers-reduced-motion: reduce) {
    .spin { animation: none; }
  }
</style>
