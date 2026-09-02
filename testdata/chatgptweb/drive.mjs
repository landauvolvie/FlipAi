import fs from 'node:fs';
import process from 'node:process';

const mod = process.env.FLIPAI_PLAYWRIGHT_MODULE;
if (!mod) throw new Error('FLIPAI_PLAYWRIGHT_MODULE is required');
const { chromium } = await import(mod);
const initScript = fs.readFileSync(process.env.FLIPAI_CHATGPT_SCRIPT_FILE, 'utf8');
const browser = await chromium.launch({headless:true});
const page = await browser.newPage();
const errors = [];
page.on('pageerror', e => errors.push(String(e)));
await page.setContent(`<!doctype html><html><body>
<form id="chat-form">
  <div id="prompt-textarea" contenteditable="true"></div>
  <button type="submit" data-testid="send-button">Send</button>
</form>
<div id="turns"></div>
</body></html>`);
await page.evaluate(() => {
  globalThis.__test = {states:[],replies:[],submitted:[],errors:[],mode:'network'};
  globalThis.flipChatGPTState = async s => { globalThis.__test.states.push(s); };
  globalThis.flipChatGPTReply = async r => { globalThis.__test.replies.push(r); };
  globalThis.flipChatGPTSubmitted = async s => { globalThis.__test.submitted.push(s); };
  globalThis.flipChatGPTError = async e => { globalThis.__test.errors.push(e); };
  globalThis.fetch = async () => {
    const body = 'data: '+JSON.stringify({conversation_id:'conv-network-1',message:{author:{role:'assistant'},content:{parts:['NETWORK ANSWER']}}})+'\n\ndata: [DONE]\n';
    return new Response(body,{status:200,headers:{'content-type':'text/event-stream'}});
  };
  document.querySelector('#chat-form').addEventListener('submit', e => {
    e.preventDefault();
    if (globalThis.__test.mode === 'network') {
      setTimeout(()=>globalThis.fetch('https://chatgpt.com/backend-api/conversation'), 40);
      return;
    }
    setTimeout(() => {
      const n=document.createElement('div');
      n.setAttribute('data-message-author-role','assistant');
      n.textContent='DOM ANSWER';
      document.querySelector('#turns').appendChild(n);
    },80);
  });
});
await page.evaluate(script => { (0,eval)(script); }, initScript);

await page.evaluate(() => globalThis.__flipAiChatGPTSubmit('network-turn','hello network'));
await page.waitForFunction(() => globalThis.__test.replies.some(r => r.turnId==='network-turn'), null, {timeout:10000});
const network = await page.evaluate(() => globalThis.__test.replies.find(r => r.turnId==='network-turn'));

await page.evaluate(() => { globalThis.__test.mode='dom'; });
await page.evaluate(() => globalThis.__flipAiChatGPTSubmit('dom-turn','hello dom'));
await page.waitForFunction(() => globalThis.__test.replies.some(r => r.turnId==='dom-turn'), null, {timeout:10000});
const dom = await page.evaluate(() => globalThis.__test.replies.find(r => r.turnId==='dom-turn'));

const report = await page.evaluate(() => ({
  boundErrors: globalThis.__test.errors,
  submitted: globalThis.__test.submitted,
  composer: document.querySelector('#prompt-textarea').textContent,
  stateCount: globalThis.__test.states.length
}));
report.errors = errors;
report.network = network;
report.dom = dom;
console.log(JSON.stringify(report));
await browser.close();
