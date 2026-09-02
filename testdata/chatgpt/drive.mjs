import fs from 'node:fs';
import path from 'node:path';
import { pathToFileURL } from 'node:url';

const pwPath = process.env.FLIPAI_PLAYWRIGHT_MODULE;
if (!pwPath) throw new Error('FLIPAI_PLAYWRIGHT_MODULE is required');
const { chromium } = await import(pathToFileURL(pwPath).href);

const source = fs.readFileSync(path.resolve('chatgpt_webview_windows.go'), 'utf8');
const marker = 'const chatGPTTurnJS = `';
const start = source.indexOf(marker);
if (start < 0) throw new Error('chatGPTTurnJS was not found');
const bodyStart = start + marker.length;
const bodyEnd = source.indexOf('`\n\ntype chatGPTTurnResult', bodyStart);
if (bodyEnd < 0) throw new Error('chatGPTTurnJS closing marker was not found');
const template = source.slice(bodyStart, bodyEnd);
const prompt = 'browser harness prompt';
const script = template.replace('%s', JSON.stringify(prompt));

const browser = await chromium.launch({headless:true});
const page = await browser.newPage();
const errors = [];
page.on('pageerror', e => errors.push(String(e)));
await page.setContent(`<!doctype html><html><body>
  <textarea id="prompt-textarea"></textarea>
  <button data-testid="send-button">Send</button>
  <div id="messages"><div id="old-assistant" data-message-author-role="assistant">OLD GMAIL RESPONSE</div></div>
  <script>
    document.querySelector('[data-testid="send-button"]').addEventListener('click',()=>{
      const value=document.querySelector('#prompt-textarea').value;
      const messages=document.querySelector('#messages');
      const user=document.createElement('div');user.setAttribute('data-message-author-role','user');user.textContent=value;messages.appendChild(user);
      const stop=document.createElement('button');stop.dataset.testid='stop-button';stop.textContent='Stop';document.body.appendChild(stop);
      const old=document.querySelector('#old-assistant');
      setTimeout(()=>old.textContent='OLD GMAIL RESPONSE (UI MUTATED)',120);
      setTimeout(()=>stop.remove(),520);
      setTimeout(()=>{
        const msg=document.createElement('div');msg.setAttribute('data-message-author-role','assistant');messages.appendChild(msg);
        msg.textContent='FLIPAI';
        setTimeout(()=>msg.textContent='FLIPAI browser response for '+value,280);
      },900);
    });
  </script>
</body></html>`);
const result = await page.evaluate(script);
const composer = await page.locator('#prompt-textarea').inputValue();
await browser.close();
console.log(JSON.stringify({result, composer, errors}));
