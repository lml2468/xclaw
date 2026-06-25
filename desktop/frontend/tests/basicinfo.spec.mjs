// Geometry probe for the BasicInfo per-bot config sections added in E2:
// System Prompt mode, Setting Sources, tool picker, and the MCP editor. Opens
// SettingsModal in preview mode (mock data, no daemon) and asserts the new
// fieldsets render, fit their container (no horizontal overflow), and the
// segmented control + checkboxes are present and tappable. Run with the vite
// dev server up (npm run dev) on :9245.
import { chromium } from "playwright-core";

const BASE = process.env.OCTOBUDDY_TEST_URL ?? "http://127.0.0.1:9245";
let failed = 0;
const check = (cond, name) => {
  if (cond) console.log("  ✓", name);
  else { failed++; console.log("  ✗", name); }
};

const browser = await chromium.launch({ channel: "chrome", headless: true });
const ctx = await browser.newContext({ viewport: { width: 1280, height: 900 }, deviceScaleFactor: 2, colorScheme: "dark" });
const page = await ctx.newPage();
await page.goto(`${BASE}/?preview=1&theme=dark&settings=basic`, { waitUntil: "networkidle" });
await page.waitForTimeout(400);

const modal = page.locator(".pane").first();
check(await modal.count() > 0, "BasicInfo pane rendered");

// New section legends present.
for (const legend of ["System Prompt 模式", "配置来源", "可用工具", "MCP 服务器"]) {
  check(await page.locator("legend", { hasText: legend }).count() > 0, `section present: ${legend}`);
}

// Segmented prompt-mode control: two buttons, one active.
const seg = page.locator(".modeseg button");
check(await seg.count() === 2, "prompt-mode has 2 segments");
check(await page.locator(".modeseg button.active").count() === 1, "exactly one prompt-mode active");

// Tool picker: in preview the mock toolset is scoped-off → shows the "限定" button.
check(await page.locator("button", { hasText: "限定可用工具" }).count() > 0, "tool picker offers scoping");

// MCP editor textarea present and monospace.
check(await page.locator("textarea.mono").count() > 0, "MCP json editor present");
check(await page.locator("button", { hasText: "保存并测试连接" }).count() > 0, "MCP test button present");

// No horizontal overflow in the scroll container holding the pane.
const overflow = await modal.evaluate((el) => {
  const sc = el.closest("[class*='body'],[class*='scroll']") ?? el.parentElement ?? el;
  return sc.scrollWidth - sc.clientWidth;
});
check(overflow <= 1, `no horizontal overflow (Δ=${overflow}px)`);

// Toggle project setting-source → warning appears.
const projChk = page.locator(".chk", { hasText: "project" }).locator("input");
await projChk.check();
await page.waitForTimeout(150);
check(await page.locator(".warn", { hasText: "提示词注入" }).count() > 0, "project opt-in shows injection warning");

await page.screenshot({ path: "tests/baseline/basicinfo-e2.png", fullPage: true });
console.log(failed === 0 ? "\nALL PASS" : `\n${failed} FAILED`);
await browser.close();
process.exit(failed === 0 ? 0 : 1);
