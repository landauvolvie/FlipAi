// Drives the real file-picker finder against composers that hide their file
// input the way a web-component UI does.
//
// Gemini refused a photo with "This chat has no file picker that accepts these
// attachments" while the picker was there: it lived inside a shadow root,
// behind an upload menu, and a plain document query could not see it.
import fs from 'node:fs';
import { pathToFileURL } from 'node:url';

const pwPath = process.env.FLIPAI_PLAYWRIGHT_MODULE;
if (!pwPath) throw new Error('FLIPAI_PLAYWRIGHT_MODULE is required');
const scriptPath = process.env.FLIPAI_FIND_INPUT_JS;
if (!scriptPath) throw new Error('FLIPAI_FIND_INPUT_JS is required');
const { chromium } = await import(pathToFileURL(pwPath).href);
const findJS = fs.readFileSync(scriptPath, 'utf8');

const layouts = {
  // The plain case that already worked.
  'plain input': `<input type="file" accept="image/*">`,

  // The input exists only after an upload menu is opened.
  'behind an upload menu': `
    <button aria-label="Open upload menu" onclick="
      if(!document.querySelector('#late')){
        const i=document.createElement('input');i.type='file';i.id='late';i.accept='image/*';document.body.appendChild(i);
      }">+</button>`,

  // The input lives inside a shadow root: invisible to document.querySelectorAll.
  'inside a shadow root': `
    <div id="host"></div>
    <script>
      const root = document.querySelector('#host').attachShadow({mode:'open'});
      root.innerHTML = '<input type="file" accept="image/*">';
    </script>`,

  // Both at once, which is what a component-built composer actually looks like.
  'shadow root behind a menu': `
    <div id="host2"></div>
    <script>
      const root = document.querySelector('#host2').attachShadow({mode:'open'});
      root.innerHTML = '<button aria-label="Add files">+</button>';
      root.querySelector('button').addEventListener('click', () => {
        if (!root.querySelector('input')) {
          const i = document.createElement('input');
          i.type = 'file'; i.accept = 'image/*';
          root.appendChild(i);
        }
      });
    </script>`,
};

const browser = await chromium.launch({ headless: true });
const failures = [];
const report = [];

for (const [name, markup] of Object.entries(layouts)) {
  const page = await browser.newPage();
  await page.setContent(`<!doctype html><html><body><textarea></textarea>${markup}</body></html>`);
  let found = false;
  // The finder opens one control per call, exactly as FlipAi retries it.
  for (let i = 0; i < 24 && !found; i++) {
    found = await page.evaluate(`(() => { const el = ${findJS}; return !!el; })()`);
    if (!found) await page.waitForTimeout(25);
  }
  if (found) report.push(`${name}: ok`);
  else failures.push(`${name}: the file picker was not found`);
  await page.close();
}

await browser.close();
report.sort();
failures.sort();
console.log(JSON.stringify({ ok: failures.length === 0, report, failures }, null, 2));
if (failures.length) process.exit(1);
