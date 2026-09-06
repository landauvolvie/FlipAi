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
const detectorReady = await page.evaluate(() => globalThis.__flipAiGoogleVoiceSMSDetectorReady === true);
const detectorRows = await page.evaluate(() => Number(globalThis.__flipAiGoogleVoiceSMSDetectorRows || 0));

await page.locator('#messageText').evaluate(el => { el.textContent = 'X: call'; });
await page.waitForTimeout(1800);
let captured = await page.evaluate(() => globalThis.__captured || []);
const finalURL = page.url();

await page.locator('#messageText').evaluate(el => { el.textContent = 'You: reply'; });
await page.waitForTimeout(1000);
const afterOutgoing = await page.evaluate(() => globalThis.__captured || []);

console.log(JSON.stringify({ errors, captured, afterOutgoing, detectorReady, detectorRows, finalURL }));
await browser.close();
