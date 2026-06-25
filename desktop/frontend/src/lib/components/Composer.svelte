<script lang="ts">
  import { store } from "../store.svelte";
  import { isImeComposing } from "../keys";
  import { SessionAttachment } from "../../../bindings/github.com/lml2468/octobuddy/core/control/wire/models";

  let draft = $state("");
  let ta: HTMLTextAreaElement;
  let fileInput: HTMLInputElement;

 // Caps are mirrored from gateway/media.go (MaxImageBytes / MaxFileBytes /
 // MaxImagesPerSend) + composerAttachmentLimit in cmd/octobuddy-daemon.
 // Client-side enforcement is UX only — the daemon re-validates.
  const MAX_IMAGE_BYTES = 5 * 1024 * 1024;
  const MAX_FILE_BYTES = 5 * 1024 * 1024;
  const MAX_IMAGES = 6;
  const MAX_ATTACHMENTS = 10;

 // A single attached file as the Composer tracks it pre-send. Bytes are
 // base64-stringified ONCE on add (cheap at our 5 MB cap) so render +
 // remove cost stays constant and the send path doesn't re-read FileReader.
  type PendingAttachment = {
    id: string;
    name: string;
    kind: "image" | "file";
    mime: string;
    size: number;
    data: string;          // base64 of the file bytes
    error?: string;        // non-empty when over cap / unreadable — chip turns red, blocks send
  };
  let pending = $state<PendingAttachment[]>([]);
  let dropActive = $state(false);
  let dragDepth = 0;       // nested dragenter/leave counter — overlay only clears on the outermost dragleave

  const hasReady = $derived(pending.length > 0 && pending.every((p) => !p.error));
  const canSend = $derived(
    (draft.trim().length > 0 || hasReady) &&
    !!store.selectedBotId &&
    !store.currentSession?.awaiting &&
    !pending.some((p) => !!p.error)
  );

  export function setDraft(text: string) {
    draft = text;
    ta?.focus();
 // Wait for Svelte to commit the new bind:value before measuring
 // ta.scrollHeight — running autogrow synchronously here measured the
 // OLD textarea content, so a long EmptyState prompt landed at 1 row
 // until the next keystroke. Matches the send
 // pattern below.
    requestAnimationFrame(autogrow);
  }
  function autogrow() {
    if (!ta) return;
    ta.style.height = "auto";
    ta.style.height = Math.min(ta.scrollHeight, 140) + "px";
  }
  function send() {
    if (!canSend) return;
    const atts = pending.map((p) => new SessionAttachment({ name: p.name, kind: p.kind, mime: p.mime, data: p.data }));
    const chips = pending.map((p) => ({ name: p.name, kind: p.kind, size: p.size }));
    store.send(draft, atts, chips);
    draft = "";
    pending = [];
    requestAnimationFrame(autogrow);
  }
  function onKey(e: KeyboardEvent) {
 // Skip during IME composition (CJK Pinyin / Wubi / Kana / Hangul commit
 // candidates with Enter, delivered as keydown with isComposing=true).
 // Without this guard the handler swallows the commit and ships a
 // half-typed pinyin/romaji string — every other message for Chinese,
 // Japanese, Korean users.
    if (isImeComposing(e)) return;
 // ⌘↩ / Ctrl-↩ is the canonical "force send" — bypass the Shift-Enter
 // newline path and send even if a newline character is present
 // (matches Slack/Discord/Linear muscle memory).
    if (e.key === "Enter" && (e.metaKey || e.ctrlKey) && !e.shiftKey && !e.altKey) {
      e.preventDefault();
      send();
      return;
    }
 // Plain Enter sends; Shift / Alt / Option-Enter insert a newline.
    if (e.key === "Enter" && !e.shiftKey && !e.altKey) { e.preventDefault(); send(); }
  }

 // ── attachments ────────────────────────────────────────────────────
  function classifyKind(mime: string): "image" | "file" {
    return mime.startsWith("image/") ? "image" : "file";
  }
  function capFor(kind: "image" | "file") {
    return kind === "image" ? MAX_IMAGE_BYTES : MAX_FILE_BYTES;
  }
  function fmtBytes(n: number): string {
    if (n < 1024) return `${n} B`;
    if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} KB`;
    return `${(n / 1024 / 1024).toFixed(1)} MB`;
  }
 // Convert ArrayBuffer → base64 via chunked btoa to avoid the call-stack
 // explosion plain `btoa(String.fromCharCode(...new Uint8Array(buf)))`
 // hits past a few hundred KB of input.
  function toBase64(buf: ArrayBuffer): string {
    const bytes = new Uint8Array(buf);
    const CHUNK = 0x8000;
    let bin = "";
    for (let i = 0; i < bytes.length; i += CHUNK) {
      bin += String.fromCharCode(...bytes.subarray(i, i + CHUNK));
    }
    return btoa(bin);
  }
  function nextId(): string {
    return `a${Date.now().toString(36)}${Math.random().toString(36).slice(2, 6)}`;
  }
  async function readAndAdd(file: File) {
    const kind = classifyKind(file.type || "");
    const cap = capFor(kind);
    const imageCount = pending.filter((p) => p.kind === "image").length;
    let error: string | undefined;
    if (file.size > cap) {
      error = `超过 ${fmtBytes(cap)} 上限`;
    } else if (kind === "image" && imageCount >= MAX_IMAGES) {
      error = `单次最多 ${MAX_IMAGES} 张图片`;
    }
    let data = "";
    if (!error) {
      try {
        const buf = await file.arrayBuffer();
        data = toBase64(buf);
      } catch (e) {
        error = `读取失败: ${(e as Error).message}`;
      }
    }
    pending = [...pending, { id: nextId(), name: file.name, kind, mime: file.type || "application/octet-stream", size: file.size, data, error }];
  }
  async function addFiles(files: FileList | File[]) {
    const arr = Array.from(files);
    for (const f of arr) {
      if (pending.length >= MAX_ATTACHMENTS) {
        pending = [...pending, { id: nextId(), name: f.name, kind: classifyKind(f.type), mime: f.type, size: f.size, data: "", error: `单次最多 ${MAX_ATTACHMENTS} 个附件` }];
        break;
      }
      await readAndAdd(f);
    }
  }
  function openPicker() {
    fileInput?.click();
  }
  function onFileInput(e: Event) {
    const t = e.target as HTMLInputElement;
    if (t.files && t.files.length) addFiles(t.files);
    t.value = "";  // reset so picking the same file twice still fires onChange
  }
  function removeAt(id: string) {
    pending = pending.filter((p) => p.id !== id);
  }

 // ── drag-and-drop ──────────────────────────────────────────────────
 // Track nesting depth so the overlay only clears on the OUTER dragleave
 // (HTML5 fires leave-events when the cursor moves between child elements).
  function onDragEnter(e: DragEvent) {
    if (!hasFiles(e)) return;
    dragDepth += 1;
    dropActive = true;
  }
  function onDragLeave() {
    dragDepth = Math.max(0, dragDepth - 1);
    if (dragDepth === 0) dropActive = false;
  }
  function onDragOver(e: DragEvent) {
    if (!hasFiles(e)) return;
    e.preventDefault();
    if (e.dataTransfer) e.dataTransfer.dropEffect = "copy";
  }
  function onDrop(e: DragEvent) {
    e.preventDefault();
    dragDepth = 0;
    dropActive = false;
    if (e.dataTransfer?.files?.length) addFiles(e.dataTransfer.files);
  }
  function hasFiles(e: DragEvent): boolean {
    const types = e.dataTransfer?.types;
    if (!types) return false;
    for (let i = 0; i < types.length; i++) if (types[i] === "Files") return true;
    return false;
  }
</script>

<div
  class="composer"
  role="region"
  aria-label="消息撰写区"
  ondragenter={onDragEnter}
  ondragleave={onDragLeave}
  ondragover={onDragOver}
  ondrop={onDrop}
>
  {#if pending.length > 0}
    <div class="chips" aria-label="待发送附件">
      {#each pending as p (p.id)}
        <div class="chip" class:err={!!p.error} title={p.error ?? `${p.name} · ${fmtBytes(p.size)}`}>
          <span class="ic" aria-hidden="true">
            {#if p.kind === "image"}
              <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><rect x="3" y="3" width="18" height="18" rx="2"/><circle cx="9" cy="9" r="2"/><path d="m21 15-5-5L5 21"/></svg>
            {:else}
              <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z"/><polyline points="14 2 14 8 20 8"/></svg>
            {/if}
          </span>
          <span class="name">{p.name}</span>
          <span class="size">{p.error ? p.error : fmtBytes(p.size)}</span>
          <button class="x" onclick={() => removeAt(p.id)} aria-label="移除附件">×</button>
        </div>
      {/each}
    </div>
  {/if}
  <div class="field">
    <button class="attach" onclick={openPicker} aria-label="添加附件" title="添加附件">
      <svg width="17" height="17" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="m21 12-9.5 9.5a5 5 0 1 1-7-7L13 5.5a3.5 3.5 0 0 1 5 5L9 19"/></svg>
    </button>
    <input bind:this={fileInput} type="file" multiple hidden onchange={onFileInput} />
    <textarea
      bind:this={ta}
      bind:value={draft}
      rows="1"
      placeholder="给 Agent 发消息…"
      aria-label="消息"
      maxlength="32768"
      oninput={autogrow}
      onkeydown={onKey}
    ></textarea>
    <button class="send" class:on={canSend} onclick={send} disabled={!canSend} aria-label="Send">
      <svg width="17" height="17" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.4" stroke-linecap="round" stroke-linejoin="round"><path d="M12 19V5M5 12l7-7 7 7"/></svg>
    </button>
  </div>
  {#if dropActive}
    <div class="drop-overlay" aria-hidden="true">拖入以添加附件</div>
  {/if}
</div>

<style>
  .composer {
    position: relative;
    background: color-mix(in srgb, var(--surface) 68%, transparent);
    backdrop-filter: blur(20px) saturate(160%); -webkit-backdrop-filter: blur(20px) saturate(160%);
    border-top: 1px solid var(--hairline);
    padding: 12px var(--gutter) 14px;
  }
  .field {
    max-width: var(--content-max); margin: 0 auto;
    display: flex; align-items: center; gap: 9px;
  }
  .chips {
    max-width: var(--content-max); margin: 0 auto 8px;
    display: flex; flex-wrap: wrap; gap: 6px;
  }
  .chip {
    display: inline-flex; align-items: center; gap: 6px;
    padding: 4px 4px 4px 8px;
    background: color-mix(in srgb, var(--ink) 8%, transparent);
    border: 1px solid var(--hairline);
    border-radius: 999px;
    font-size: 12px; color: var(--ink);
    max-width: 240px;
  }
  .chip .ic { display: inline-flex; color: var(--ink-soft); flex: 0 0 auto; }
  .chip .name { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; flex: 1 1 auto; min-width: 0; }
  .chip .size { font-size: 11px; color: var(--ink-faint); flex: 0 0 auto; font-family: var(--mono); }
  .chip .x {
    width: 18px; height: 18px; border-radius: 50%; border: none;
    background: transparent; color: var(--ink-faint); cursor: pointer;
    display: grid; place-items: center; font-size: 14px; line-height: 1;
    flex: 0 0 auto;
  }
  .chip .x:hover { background: color-mix(in srgb, var(--ink) 12%, transparent); color: var(--ink); }
  .chip.err { border-color: color-mix(in srgb, #d33 60%, var(--hairline)); color: #c33; background: color-mix(in srgb, #d33 8%, transparent); }
  .chip.err .size { color: #c33; }
  textarea {
    flex: 1; border: 1px solid var(--hairline); outline: none; resize: none;
    background: color-mix(in srgb, var(--ink) 6%, transparent);
    border-radius: var(--radius-control); padding: 10px 13px; line-height: 1.45; max-height: 140px;
    color: var(--ink); font-size: 14px;
    transition: border-color 0.15s ease, background 0.15s ease, box-shadow 0.15s ease;
  }
  textarea:focus {
    border-color: color-mix(in srgb, var(--accent) 65%, transparent);
    background: color-mix(in srgb, var(--ink) 3%, transparent);
    box-shadow: 0 0 0 3px color-mix(in srgb, var(--accent) 16%, transparent);
  }
  textarea::placeholder { color: var(--ink-faint); }
  .attach, .send {
    flex: 0 0 34px; width: 34px; height: 34px; border-radius: var(--radius-control); border: none;
    display: grid; place-items: center;
    background: color-mix(in srgb, var(--ink) 10%, transparent); color: var(--ink-faint);
    transition: background 0.15s ease, color 0.15s ease, transform 0.12s ease, box-shadow 0.15s ease;
    cursor: pointer;
  }
  .attach:hover { background: color-mix(in srgb, var(--ink) 16%, transparent); color: var(--ink); }
  .send.on { background: var(--accent-grad); color: #fff; box-shadow: var(--accent-glow); }
  .send.on:hover { transform: translateY(-1px); box-shadow: 0 7px 20px color-mix(in srgb, var(--accent) 55%, transparent); }
  .send:disabled { cursor: default; }
  @media (prefers-reduced-motion: reduce) { .send.on:hover { transform: none; } }
  .drop-overlay {
    position: absolute; inset: 0; z-index: 5;
    display: grid; place-items: center;
    background: color-mix(in srgb, var(--accent) 14%, var(--surface) 80%);
    border: 2px dashed color-mix(in srgb, var(--accent) 65%, transparent);
    border-radius: var(--radius);
    color: var(--accent-strong); font-size: 14px; font-weight: 550;
    pointer-events: none;
  }
</style>
