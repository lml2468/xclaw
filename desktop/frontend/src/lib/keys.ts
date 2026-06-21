// Keyboard-event helpers shared across components.
//
// isImeComposing tells you whether a `keydown` is the IME's commit/cancel
// keystroke for an in-flight composition (CJK Pinyin, Wubi, Kana, Hangul).
// Without this check, an Enter handler that runs `e.preventDefault(); send()`
// swallows the IME commit and ships the half-typed pinyin/romaji as the
// message — every other message for Chinese/Japanese/Korean users.
//
// Lives here as a one-liner because we keep getting this wrong site-by-site:
// round 10 fixed Composer + CommandPalette, round 11 found 3 more (SkillsPane
// new-skill, SkillsPane new-file path, WorkflowsPane new-workflow) that had
// the same bug. Centralizing the check makes "did the new input add an IME
// guard?" a grep-able question (any handler doing `.key === "Enter"` should
// also call `isImeComposing(e)`).
//
// `e.keyCode === 229` is the legacy fallback: older webviews (and some
// non-Chromium runtimes) don't set `isComposing` but emit keyCode 229 for
// every key during composition.
export function isImeComposing(e: KeyboardEvent): boolean {
  return e.isComposing || e.keyCode === 229;
}
