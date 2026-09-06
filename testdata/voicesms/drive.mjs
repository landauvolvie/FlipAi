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
await page.route('https://voice.google.com/**', async route => {
  const u = new URL(route.request().url());
  if (u.pathname.includes('/background-sync')) {
    const body = u.searchParams.get('body') || '';
    return route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({
        conversations: [
          {
            itemId: 't.+18455550142',
            contact: { name: 'Me' },
            messages: [{ text: body, direction: 'INCOMING' }]
          },
          {
            itemId: 't.+19995550123',
            contact: { name: 'Other' },
            messages: [{ text: 'unrelated message', direction: 'INCOMING' }]
          }
        ]
      })
    });
  }
  return route.fulfill({ status: 200, contentType: 'text/html', body: fixture });
});

await page.goto('https://voice.google.com/u/2/messages');
await page.waitForTimeout(1000);
const detectorReady = await page.evaluate(() => globalThis.__flipAiGoogleVoiceSMSDetectorReady === true);
const detectorRows = await page.evaluate(() => Number(globalThis.__flipAiGoogleVoiceSMSDetectorRows || 0));

const inbound = 'X: call 212-555-0199';
await page.evaluate(async body => {
  await fetch('/_/VoiceUi/data/background-sync?body=' + encodeURIComponent(body));
  document.getElementById('messageText').textContent = body;
}, inbound);
await page.waitForTimeout(2200);
const captured = await page.evaluate(() => globalThis.__captured || []);
const finalURL = page.url();
const rowClicks = await page.evaluate(() => Number(globalThis.__rowClicks || 0));
const selected = await page.locator('#threadRow').getAttribute('aria-selected') || '';

await page.locator('#messageText').evaluate(el => { el.textContent = 'You: reply'; });
await page.waitForTimeout(1000);
const afterOutgoing = await page.evaluate(() => globalThis.__captured || []);

console.log(JSON.stringify({ errors, captured, afterOutgoing, detectorReady, detectorRows, finalURL, rowClicks, selected }));
await browser.close();
