// Geometry probe for the per-conversation tool panel (E3): the chat-header tool
// icon opens a popover that scopes tools for the current conversation. Runs in
// preview mode (mock roster + session, no daemon). Asserts the icon appears,
// the popover opens with the follow-default / custom segmented control, custom
// reveals the tool grid, and the popover closes on overlay click. Requires the
// vite dev server (npm run dev) on :9245.
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
await page.goto(`${BASE}/?preview=1&theme=dark`, { waitUntil: "networkidle" });
await page.waitForTimeout(400);

// Select the first conversation in the list so the chat + header render.
const firstRow = page.locator("[class*='session'], [class*='convo'], .list button, .list [role='button']").first();
if (await firstRow.count()) { await firstRow.click().catch(() => {}); await page.waitForTimeout(300); }

const toolBtn = page.locator("button[aria-label='本会话可用工具']");
check(await toolBtn.count() > 0, "chat-header tool icon present");

if (await toolBtn.count() > 0) {
  await toolBtn.click();
  await page.waitForTimeout(250);
  const panel = page.locator(".panel[aria-label='本会话可用工具']");
  check(await panel.count() > 0, "tool panel opens");
  check(await page.locator(".modeseg button", { hasText: "跟随 Bot 默认" }).count() > 0, "has follow-default segment");
  check(await page.locator(".modeseg button", { hasText: "本会话自定义" }).count() > 0, "has custom segment");

  // Switch to custom → tool grid appears.
  await page.locator(".modeseg button", { hasText: "本会话自定义" }).click();
  await page.waitForTimeout(200);
  const checks = page.locator(".panel .toolgrid .chk");
  check(await checks.count() > 0, `custom reveals tool grid (${await checks.count()} tools)`);

  // Panel fits viewport (right edge on screen, not clipped).
  const box = await panel.boundingBox();
  check(!!box && box.x + box.width <= 1280 + 1, "panel within viewport width");
  check(!!box && box.y >= 0, "panel top on-screen");

  await page.screenshot({ path: "tests/baseline/channel-tools-e3.png" });

  // Overlay click closes it.
  await page.locator(".overlay").click({ position: { x: 5, y: 400 } }).catch(() => {});
  await page.waitForTimeout(200);
  check(await page.locator(".panel[aria-label='本会话可用工具']").count() === 0, "overlay click closes panel");
}

console.log(failed === 0 ? "\nALL PASS" : `\n${failed} FAILED`);
await browser.close();
process.exit(failed === 0 ? 0 : 1);
