<script lang="ts">
  // The XClaw octopus mark: a smooth domed mantle over five hanging tentacles,
  // with two glossy eyes. The body is currentColor (drawn white on the brand
  // tile / avatars); eyes are dark with a tiny specular highlight so the glyph
  // reads as alive and crisp at small sizes.
  let { size = 24 }: { size?: number } = $props();
</script>

<svg width={size} height={size} viewBox="0 0 100 100" aria-hidden="true">
  <path
    fill="currentColor"
    d="M50 12
       C31 12 19.5 25 19.5 43
       c0 7.8 1.9 14 5.8 18.8
       q4.82 31 9.64 0 q4.82 31 9.64 0 q4.82 31 9.64 0 q4.82 31 9.64 0 q4.82 31 9.64 0
       c3.9 -4.8 5.8 -11 5.8 -18.8
       C80.5 25 69 12 50 12 Z"
  />
  <!-- eyes: dark iris + specular highlight -->
  <circle cx="40.5" cy="40" r="4.7" fill="rgba(0,0,0,0.34)" />
  <circle cx="59.5" cy="40" r="4.7" fill="rgba(0,0,0,0.34)" />
  <circle cx="42.2" cy="38.3" r="1.55" fill="rgba(255,255,255,0.88)" />
  <circle cx="61.2" cy="38.3" r="1.55" fill="rgba(255,255,255,0.88)" />
</svg>
