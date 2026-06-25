// errMsg coerces an unknown caught value into a human-readable message.
// Wails Go-binding rejections arrive as plain Error objects but third-party
// callers (postMessage, browser APIs) may throw strings, numbers, or
// {message: "..."} bags — this normalizes all of them. Centralized so we can
// upgrade the formatting (translation, stack trim) without grepping for
// `catch (e: any)` again.
export function errMsg(e: unknown): string {
  if (e instanceof Error) return e.message;
  if (typeof e === "string") return e;
  if (e && typeof e === "object" && "message" in e) {
    const m = (e as { message?: unknown }).message;
    if (typeof m === "string") return m;
  }
  return String(e);
}

// friendlyErr maps known backend (Go daemon / octo-cli) error fragments to
// actionable Chinese, so the user never sees a bare English technical string in
// the otherwise-Chinese UI. The raw English stays in the daemon logs; this only
// rewrites what surfaces in the UI. Matching is substring-based (the raw error
// is often wrapped, e.g. "octo-cli group list (id): exec: …"), and falls back to
// errMsg() for anything unrecognized. Order matters: more-specific first.
const FRIENDLY: ReadonlyArray<readonly [RegExp, string]> = [
  [/cron is not enabled/i, "未启用定时任务，请先点击「启用」并保存并重启。"],
  [/owner not resolved|bot owner not resolved/i, "Bot 主人身份尚未确认（需 IM 注册完成），请稍后重试。"],
  [/group\/thread target requires channelId/i, "请先选择一个群或话题作为目标。"],
  [/DM target requires fromUid/i, "私聊目标缺少对方 uid。"],
  [/octo-cli not installed/i, "octo-cli 未安装，请先在「Octo 集成」中登录以自动下载。"],
  [/has no OCTO_BOT_ID/i, "该 Bot 未配置 OCTO_BOT_ID，请在「Octo 集成」中设置。"],
  [/has no bf_ token/i, "该 Bot 缺少 bf_ token，请在「Octo 集成」中设置 Token 后保存。"],
  [/control bus not connected/i, "与后台连接已断开，正在重连，请稍后重试。"],
  [/not authenticated|profile.*not found|no profile/i, "该 Bot 的 octo-cli 未登录，请在「Octo 集成」中重新登录。"],
  [/invalid bot id/i, "Bot ID 仅允许字母、数字、点、下划线和连字符。"],
  [/invalid skill name/i, "技能名称仅允许字母、数字、点、下划线和连字符。"],
  [/invalid workflow name/i, "工作流名称仅允许字母、数字、点、下划线和连字符。"],
];

// friendlyErr returns a UI-ready Chinese message for a caught backend error,
// falling back to errMsg() when nothing matches. Use this at the boundary where
// a Go/octo-cli error is shown to the user (banners, inline field errors).
export function friendlyErr(e: unknown): string {
  const raw = errMsg(e);
  for (const [re, msg] of FRIENDLY) {
    if (re.test(raw)) return msg;
  }
  return raw;
}
