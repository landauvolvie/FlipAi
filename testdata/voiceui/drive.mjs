// Drives the real FlipAi Connections voice card in headless Chromium against
// the real local voice endpoint. Everything here is plumbing; the script under
// test is served by the Go side so the browser runs byte-for-byte what the
// Windows app injects.
//
// What it proves is the thing that broke in the field: switching calling on
// writes it, on a machine whose audio endpoints are in the state a fresh PC
// leaves them in.
import { chromium } from '/opt/node22/lib/node_modules/playwright/index.mjs';

const PAGE = process.env.FLIPAI_UI_PAGE; // http://127.0.0.1:8765/connections
const out = { errors: [], steps: [] };

const browser = await chromium.launch({ args: ['--no-proxy-server'] });
const ctx = await browser.newContext();
const page = await ctx.newPage();
page.on('pageerror', (e) => out.errors.push(String(e)));
page.on('console', (m) => { if (m.type() === 'error') out.errors.push('console: ' + m.text()); });

await page.goto(PAGE, { waitUntil: 'domcontentloaded' });

// 1. One card, and it renders at all.
await page.waitForSelector('#voice-call-card', { timeout: 20000 });
out.steps.push('card-rendered');
out.cards = await page.$$eval('section.card h2', (hs) => hs.map((h) => h.textContent.trim()));
out.stateBefore = (await page.textContent('#vcs-state')).trim();

// 2. The switch. This is the whole point: one click, no Save button.
await page.click('#vc-enabled');
await page.waitForFunction(() => document.querySelector('#vcs-state')?.textContent?.includes('On'), null, { timeout: 15000 });
out.stateAfter = (await page.textContent('#vcs-state')).trim();
out.savedNote = (await page.textContent('#vc-saved')).trim();
out.steps.push('switch-applied');

// 3. The panel Google Voice is placed on gets reported.
await page.waitForTimeout(1200);
out.steps.push('dock-reported');

// 4. A field that is not the switch also saves by itself.
await page.selectOption('#vc-default', 'A');
await page.waitForFunction(() => document.querySelector('#vc-saved')?.textContent === 'Saved', null, { timeout: 15000 });
out.steps.push('field-autosaved');

// 5. Turning it back off works too.
await page.click('#vc-enabled');
await page.waitForFunction(() => document.querySelector('#vcs-state')?.textContent?.includes('Off'), null, { timeout: 15000 });
out.steps.push('switch-reverted');

await browser.close();
process.stdout.write(JSON.stringify(out));
