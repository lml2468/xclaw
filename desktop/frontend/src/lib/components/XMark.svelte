<script lang="ts">
 // The OctoBuddy wordmark — a stylized "X": a heavier back-slash and lighter
 // forward-slash with a slight italic shear. Geometry mirrors the macOS menu
 // bar template icon (desktop/trayicon.go) so the brand reads identically in
 // the tray, the sidebar, and as the agent's default avatar.
  let { size = 24 }: { size?: number } = $props();
</script>

<svg class="xmark" width={size} height={size} viewBox="0 0 44 44" fill="currentColor" aria-hidden="true">
  <polygon points="7.56,6 21.56,6 36.44,38 22.44,38" />
  <polygon points="33.06,6 41.56,6 10.94,38 2.44,38" />
</svg>

<style>
  .xmark { display: block; }
</style>
