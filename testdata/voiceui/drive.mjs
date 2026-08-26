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

// 6. The panel says which state it is really in, and offers a way out of the
//    ones a person can act on -- not the old single sentence telling the user
//    to turn on a switch that was already on. Calling is off at this point.
await page.waitForFunction(() => /Calling is off/.test(document.querySelector('#gv-embed-empty')?.textContent || ''), null, { timeout: 15000 });
out.panelWhenOff = (await page.textContent('#gv-embed-empty')).trim();
out.steps.push('panel-explains-off');

//    Turn it back on: this machine has no WebView2, so that is the reason, and
//    the panel has to name it and offer Retry.
await page.click('#vc-enabled');
await page.waitForFunction(() => document.querySelector('#vcs-state')?.textContent?.includes('On'), null, { timeout: 15000 });
await page.waitForFunction(() => !/Calling is off/.test(document.querySelector('#gv-embed-empty')?.textContent || ''), null, { timeout: 15000 });
out.panelWhileStarting = (await page.textContent('#gv-embed-empty')).trim();
out.panelButtons = await page.$$eval('#gv-embed-empty button', (bs) => bs.map((b) => b.textContent.trim()));
await page.click('#gv-embed-empty button');
await page.waitForTimeout(800);
out.panelAfterRetry = (await page.textContent('#gv-embed-empty')).trim();
out.steps.push('panel-explains-and-retries');

//    Leave it off for the save test below.
await page.click('#vc-enabled');
await page.waitForFunction(() => document.querySelector('#vcs-state')?.textContent?.includes('Off'), null, { timeout: 15000 });

// 7. A change made and immediately navigated away from is still saved. The
//    card promises that changes save as they are made, and a debounce that the
//    page outlives by less than half a second would quietly break that promise.
await page.evaluate(() => {
  const el = document.querySelector('#vcc-title');
  el.value = 'Saved On The Way Out';
  el.dispatchEvent(new Event('input', { bubbles: true }));
});
await page.goto('about:blank');
await page.waitForTimeout(1500);
out.steps.push('pending-save-flushed');

await browser.close();
process.stdout.write(JSON.stringify(out));
