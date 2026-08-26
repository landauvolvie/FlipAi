// Drives the real FlipAi voice UI in headless Chromium against the real local
// voice endpoint. Everything here is plumbing; the script under test is served
// by the Go side so the browser runs byte-for-byte what the Windows app
// injects.
//
// The UI is two cards on two pages: Settings carries the switch, the sign-ins
// and the status checks; Connections carries the live Google Voice preview.
// This walks both.
import { chromium } from '/opt/node22/lib/node_modules/playwright/index.mjs';

const SETTINGS = process.env.FLIPAI_UI_SETTINGS;      // http://127.0.0.1:8765/settings
const CONNECTIONS = process.env.FLIPAI_UI_CONNECTIONS; // http://127.0.0.1:8765/connections
const out = { errors: [], steps: [] };

const browser = await chromium.launch({ args: ['--no-proxy-server'] });
const ctx = await browser.newContext();
const page = await ctx.newPage();
page.on('pageerror', (e) => out.errors.push(String(e)));
page.on('console', (m) => { if (m.type() === 'error') out.errors.push('console: ' + m.text()); });
page.on('dialog', (d) => d.accept());

/* ---------- Settings: the whole setup ---------- */

await page.goto(SETTINGS, { waitUntil: 'domcontentloaded' });
await page.waitForSelector('#voice-call-card', { timeout: 20000 });
out.steps.push('settings-card-rendered');
out.cards = await page.$$eval('section.card h2', (hs) => hs.map((h) => h.textContent.trim()));
out.stateBefore = (await page.textContent('#vcs-state')).trim();

// 1. The switch. This is the whole point: one click, no Save button.
await page.click('#vc-enabled');
await page.waitForFunction(() => document.querySelector('#vcs-state')?.textContent?.includes('On'), null, { timeout: 15000 });
out.stateAfter = (await page.textContent('#vcs-state')).trim();
out.savedNote = (await page.textContent('#vc-saved')).trim();
out.steps.push('switch-applied');

// 2. A field that is not the switch also saves by itself.
await page.selectOption('#vc-default', 'A');
await page.waitForFunction(() => document.querySelector('#vc-saved')?.textContent === 'Saved', null, { timeout: 15000 });
out.steps.push('field-autosaved');

// 3. Sign out asks, then calls the endpoint (the dialog is auto-accepted).
await page.click('#vc-signout');
await page.waitForFunction(() => /Signed out/.test(document.querySelector('#voice-call-toast')?.textContent || ''), null, { timeout: 15000 });
out.steps.push('signed-out');

// 4. The ChatGPT sign-in for the built-in Codex voice window.
await page.click('#vc-codex-signin');
await page.waitForFunction(() => /Codex voice window/.test(document.querySelector('#voice-call-toast')?.textContent || ''), null, { timeout: 15000 });
out.steps.push('codex-signin-opened');

// 5. The status rows exist and answer.
out.statusRows = {};
for (const id of ['vcs-state','vcs-window','vcs-google','vcs-codex','vcs-audio','vcs-agents','vcs-ring','vcs-webview2','vcs-permissions']) {
  out.statusRows[id] = ((await page.textContent('#' + id)) || '').trim();
}
out.steps.push('status-rows-answer');

// 6. Turning it back off works too.
await page.click('#vc-enabled');
await page.waitForFunction(() => document.querySelector('#vcs-state')?.textContent?.includes('Off'), null, { timeout: 15000 });
out.steps.push('switch-reverted');

/* ---------- Connections: the live preview ---------- */

await page.goto(CONNECTIONS, { waitUntil: 'domcontentloaded' });
await page.waitForSelector('#voice-preview-card', { timeout: 20000 });
out.steps.push('preview-card-rendered');
out.previewCards = await page.$$eval('section.card h2', (hs) => hs.map((h) => h.textContent.trim()));

// 7. With calling off, the panel says so and points at Settings.
await page.waitForFunction(() => /Calling is off/.test(document.querySelector('#gv-embed-empty')?.textContent || ''), null, { timeout: 15000 });
out.panelWhenOff = (await page.textContent('#gv-embed-empty')).trim();
out.steps.push('panel-explains-off');

// 8. Turn calling on from Settings, come back: this machine has no WebView2,
//    so that is the reason Google Voice is not in the panel, and the panel has
//    to name it and offer Retry. The panel rectangle is also being reported
//    while this page is open (the Go side watches for it).
await page.goto(SETTINGS, { waitUntil: 'domcontentloaded' });
await page.waitForSelector('#vc-enabled', { timeout: 20000 });
await page.click('#vc-enabled');
await page.waitForFunction(() => document.querySelector('#vcs-state')?.textContent?.includes('On'), null, { timeout: 15000 });
await page.goto(CONNECTIONS, { waitUntil: 'domcontentloaded' });
await page.waitForSelector('#gv-embed-empty', { timeout: 20000 });
await page.waitForFunction(() => !/Calling is off/.test(document.querySelector('#gv-embed-empty')?.textContent || ''), null, { timeout: 15000 });
await page.waitForTimeout(1200); // let the dock report a few times
out.panelWhileStarting = (await page.textContent('#gv-embed-empty')).trim();
out.panelButtons = await page.$$eval('#gv-embed-empty button', (bs) => bs.map((b) => b.textContent.trim()));
out.steps.push('dock-reported');
await page.click('#gv-embed-empty button');
await page.waitForTimeout(800);
out.steps.push('panel-explains-and-retries');

// 9. Turn calling back off for the flush test, then make a change on Settings
//    and leave immediately: the debounced save still has to land.
await page.goto(SETTINGS, { waitUntil: 'domcontentloaded' });
await page.waitForSelector('#vc-enabled', { timeout: 20000 });
await page.click('#vc-enabled');
await page.waitForFunction(() => document.querySelector('#vcs-state')?.textContent?.includes('Off'), null, { timeout: 15000 });
await page.evaluate(() => {
  const el = document.querySelector('#vca-title');
  el.value = 'Saved On The Way Out';
  el.dispatchEvent(new Event('input', { bubbles: true }));
});
await page.goto('about:blank');
await page.waitForTimeout(1500);
out.steps.push('pending-save-flushed');

await browser.close();
process.stdout.write(JSON.stringify(out));
