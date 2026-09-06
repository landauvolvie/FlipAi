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

// The row shows a saved contact name. Its actual phone lives in dedicated
// contact metadata. A different, clickable tel: phone stays inside the SMS body
// throughout the mutation, proving body links cannot become sender identity.
await page.locator('#messageText').evaluate(el => { el.textContent = 'X: call'; });
await page.waitForTimeout(1000);
let captured = await page.evaluate(() => globalThis.__captured || []);

// Outgoing DOM updates must never be delivered back into FlipAi as inbound SMS.
await page.locator('#messageText').evaluate(el => { el.textContent = 'You: reply'; });
await page.waitForTimeout(1000);
const afterOutgoing = await page.evaluate(() => globalThis.__captured || []);

console.log(JSON.stringify({ errors, captured, afterOutgoing }));
await browser.close();
