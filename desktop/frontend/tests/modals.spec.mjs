// Modal affordance + visual regression baseline for the two top-level modals
// post-redesign: SettingsModal (per-bot, 4 sub-tabs) and TokenUsage. Drives the
// running vite dev server (`npm run dev`) via system Chrome and asserts header
// chrome, tab switching, destructive-action affordances (shared <Confirm>),
// and Esc/✕ semantics. Screenshots land in tests/baseline/ — commit them;
// future runs compare.
//
// Run:
// (term 1) cd desktop/frontend && npm run dev
// (term 2) cd desktop/frontend && node tests/modals.spec.mjs

import { chromium } from "playwright-core";
import { mkdir } from "node:fs/promises";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const __dir = dirname(fileURLToPath(import.meta.url));
const BASELINE = join(__dir, "baseline");
await mkdir(BASELINE, { recursive: true });

const BASE = process.env.OCTOBUDDY_TEST_URL ?? "http://127.0.0.1:9245";

let failed = 0;
const issues = [];
function check(cond, name) {
  if (cond) console.log("  ✓", name);
  else { failed++; issues.push(name); console.log("  ✗", name); }
}

const browser = await chromium.launch({ channel: "chrome", headless: true });
const ctx = await browser.newContext({
  viewport: { width: 1280, height: 800 },
  deviceScaleFactor: 2,
  colorScheme: "dark",
});

async function activeText(page) {
  return page.evaluate(() => (document.activeElement?.textContent ?? "").trim());
}

// --- Settings modal: header + 4 tab switches + destructive confirms ---

async function testSettings(tab, expectedTabLabel) {
  console.log(`\n--- settings (tab=${tab}) ---`);
  const page = await ctx.newPage();
  await page.goto(`${BASE}/?preview=1&theme=dark&settings=${tab}`);
  await page.waitForSelector('div[role="dialog"][aria-label="设置"]', { timeout: 4000 });

  const headerH2 = (await page.locator("header h2").first().textContent())?.trim();
  check(headerH2 === "设置", `settings(${tab}): header title "设置"`);

  const active = (await page.locator('.pane .seg [role="tab"][aria-selected="true"]').textContent())?.trim();
  check(active === expectedTabLabel, `settings(${tab}): active tab "${expectedTabLabel}" (got "${active}")`);

  const closeBtn = page.locator('header > button.x[aria-label="关闭"]');
  check((await closeBtn.count()) === 1, `settings(${tab}): header ✕ close button present`);

 // Bot rail present + at least one bot row (preview seeds 2).
  const railRows = await page.locator("aside.rail button.botrow").count();
  check(railRows >= 1, `settings(${tab}): bot rail has ${railRows} bot(s)`);

  await page.screenshot({ path: join(BASELINE, `settings-${tab}.png`) });
  console.log(`  ▸ saved → tests/baseline/settings-${tab}.png`);

  await page.close();
}

await testSettings("basic",     "基础信息");
await testSettings("octo",      "Octo 集成");
await testSettings("skills",    "技能");
await testSettings("workflows", "工作流");

// --- Settings basic tab: destructive 删除此 Bot ---

console.log("\n--- settings: 删除此 Bot 确认 ---");
{
  const page = await ctx.newPage();
  await page.goto(`${BASE}/?preview=1&theme=dark&settings=basic`);
  await page.waitForSelector('div[role="dialog"][aria-label="设置"]', { timeout: 4000 });
  await page.click("button.remove");
  const confirmDlg = page.locator('div[role="alertdialog"]').first();
  await confirmDlg.waitFor({ state: "visible", timeout: 1000 });
  check(await confirmDlg.isVisible(), "settings: 删除此 Bot opens shared <Confirm>");
  check((await activeText(page)) === "删除", "settings: confirm primary 删除 focused");
  await page.keyboard.press("Tab");
  check((await activeText(page)) === "取消", "settings: Tab cycles to 取消");
  await page.keyboard.press("Escape");
  await page.waitForTimeout(80);
  check(!(await confirmDlg.isVisible()), "settings: Esc dismisses confirm");
  check(await page.locator('div[role="dialog"][aria-label="设置"]').isVisible(), "settings: modal stays open after confirm Esc");
 // Re-open + screenshot for the baseline.
  await page.click("button.remove");
  await confirmDlg.waitFor({ state: "visible" });
  await page.screenshot({ path: join(BASELINE, "settings-confirm.png") });
  console.log("  ▸ saved → tests/baseline/settings-confirm.png");
  await page.close();
}

// --- Settings → ✕ closes the modal ---

console.log("\n--- settings: ✕ closes modal ---");
{
  const page = await ctx.newPage();
  await page.goto(`${BASE}/?preview=1&theme=dark&settings=basic`);
  await page.waitForSelector('div[role="dialog"][aria-label="设置"]', { timeout: 4000 });
  await page.locator('header > button.x[aria-label="关闭"]').click();
  await page.waitForTimeout(80);
  check((await page.locator('div[role="dialog"][aria-label="设置"]').count()) === 0, "settings: ✕ closes modal");
  await page.close();
}

// --- Token Usage standalone modal ---

console.log("\n--- usage ---");
{
  const page = await ctx.newPage();
  await page.goto(`${BASE}/?preview=1&theme=dark&usage`);
  await page.waitForSelector('div[role="dialog"][aria-label="Token 用量"]', { timeout: 4000 });

  const headerH2 = (await page.locator("header h2").first().textContent())?.trim();
  check(headerH2 === "Token 用量", `usage: header title "Token 用量"`);
  const rangeTabs = await page.locator("header .range button").count();
  check(rangeTabs === 5, `usage: 5 range tabs in header (got ${rangeTabs})`);
  const closeBtn = page.locator('header > button.x[aria-label="关闭"]');
  check((await closeBtn.count()) === 1, "usage: header ✕ close button present");

  await page.screenshot({ path: join(BASELINE, "usage.png") });
  console.log("  ▸ saved → tests/baseline/usage.png");

  await closeBtn.click();
  await page.waitForTimeout(80);
  check((await page.locator('div[role="dialog"][aria-label="Token 用量"]').count()) === 0, "usage: ✕ closes modal");
  await page.close();
}

await browser.close();
console.log("");
if (failed === 0) {
  console.log("ALL PASS");
  process.exit(0);
} else {
  console.log(`${failed} FAILED:`);
  for (const i of issues) console.log("  ·", i);
  process.exit(1);
}
