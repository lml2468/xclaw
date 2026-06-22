// Env keys the Settings UI manages in dedicated panes rather than in
// BasicInfoPane's free-form env editor. Single source of truth so renaming a
// reserved key doesn't break in two files (BasicInfoPane hides them; the
// dedicated pane re-injects on save). Stays the FRONTEND view of reserved-
// ness — the daemon doesn't care which pane wrote a key.
//
// OCTO_BOT_ID — Octo 集成 pane reads/writes (server-assigned robot id; the
// wizard sets it from OctoAddBot).
export const RESERVED_ENV_KEYS: ReadonlySet<string> = new Set(["OCTO_BOT_ID"]);
