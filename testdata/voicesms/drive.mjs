import fs from 'node:fs';
import { pathToFileURL } from 'node:url';

const modPath = process.env.FLIPAI_PLAYWRIGHT_MODULE;
if (!modPath) throw new Error('FLIPAI_PLAYWRIGHT_MODULE is required');
const { chromium } = await import(pathToFileURL(modPath).href);
const fixture = fs.readFileSync(process.env.FLIPAI_GV_SMS_FIXTURE, 'utf8');
const browser = await chromium.launch({ headless: true });
const page = await browser.newPage();
const errors = [];
page.on('pageerror', e => errors.push(String(e)));
await page.route('https://voice.google.com/**', route => route.fulfill({ status: 200, contentType: 'text/html', body: fixture }));
await page.goto('https://voice.google.com/u/2/messages');
await page.waitForTimeout(1000);

// The row intentionally shows only a saved contact name. The trusted phone
// number lives in a descendant title attribute, which mirrors the real Voice UI
// case that v0.46.33 missed.
await page.locator('#snippet').evaluate(el => { el.textContent = 'X: hi'; });
await page.waitForTimeout(1000);
let captured = await page.evaluate(() => globalThis.__captured || []);

// Outgoing DOM updates must never be delivered back into FlipAi as inbound SMS.
await page.locator('#snippet').evaluate(el => { el.textContent = 'You: reply'; });
await page.waitForTimeout(1000);
const afterOutgoing = await page.evaluate(() => globalThis.__captured || []);

console.log(JSON.stringify({ errors, captured, afterOutgoing }));
await browser.close();
