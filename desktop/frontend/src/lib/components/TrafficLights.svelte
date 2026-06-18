<script lang="ts">
  // Custom macOS-style window controls for the frameless window. Wired to the
  // Wails window API; the cluster shows ×/−/+ glyphs on hover (like macOS).
  import { Window } from "@wailsio/runtime";
</script>

<div class="lights" style="--wails-draggable: no-drag;">
  <button class="dot close" onclick={() => Window.Close()} aria-label="Close window"><span>✕</span></button>
  <button class="dot min" onclick={() => Window.Minimise()} aria-label="Minimise window"><span>−</span></button>
  <button class="dot max" onclick={() => Window.ToggleMaximise()} aria-label="Zoom window"><span>+</span></button>
</div>

<style>
  .lights { display: flex; gap: 8px; align-items: center; }
  .dot {
    width: 12px; height: 12px; border-radius: 50%; border: none; padding: 0;
    display: grid; place-items: center; cursor: default;
    -webkit-app-region: no-drag;
  }
  .close { background: #ff5f57; }
  .min { background: #febc2e; }
  .max { background: #28c840; }
  .dot span {
    font-size: 9px; line-height: 1; font-weight: 700; color: rgba(0, 0, 0, 0.55);
    opacity: 0; transition: opacity 0.1s ease;
  }
  .lights:hover .dot span { opacity: 1; }
  .dot.min span { font-size: 12px; margin-top: -1px; }
  .dot.max span { font-size: 13px; }
  .dot:active { filter: brightness(0.85); }
</style>
