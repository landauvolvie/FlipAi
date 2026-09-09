import assert from 'node:assert/strict';
const { chromium } = await import(process.env.FLIPAI_PLAYWRIGHT_MODULE);
const browser = await chromium.launch({args:['--no-sandbox'],executablePath:process.env.FLIPAI_TEST_CHROMIUM || undefined});
try {
 const page = await browser.newPage();
 const errors=[];
 page.on('pageerror',e=>errors.push(String(e)));
 page.on('dialog',async d=>{errors.push('Unexpected dialog: '+d.message()); await d.dismiss();});
 let status={available:true, targetVersion:'99.0.0', downloading:true, ready:false, percent:45};
 let installs=0;
 let failInstall=false;
 await page.route('**/update/status.json',r=>r.fulfill({json:status}));
 await page.route('**/update/install',async r=>{
  installs++;
  await new Promise(resolve=>setTimeout(resolve,1400));
  await r.fulfill({status:failInstall?500:200,json:{installing:!failInstall}});
 });
 await page.goto(process.env.FLIPAI_UPDATE_TEST_URL);
 const icon=page.locator('#flipai-update-install');
 await icon.waitFor();
 assert.equal(await icon.isDisabled(),true);
 assert.match(await icon.getAttribute('title'),/45%/);
 assert.equal((await icon.textContent()).trim(),'');
 status={...status,downloading:false,ready:true,percent:100};
 await page.waitForFunction(()=>!document.querySelector('#flipai-update-install')?.disabled);
 if (process.env.FLIPAI_UPDATE_SCREENSHOT) await page.screenshot({path:process.env.FLIPAI_UPDATE_SCREENSHOT});
 const box=await icon.boundingBox();
 assert.ok(box.width<=32 && box.height<=32,JSON.stringify(box));
 await icon.focus();
 await page.waitForTimeout(1200);
 assert.equal(await icon.evaluate(el=>document.activeElement===el),true,'polling must preserve keyboard focus');
 await icon.click();
 await page.waitForTimeout(2200);
 assert.equal(installs,1);
 assert.equal(await icon.isDisabled(),true,'polling must not re-enable install');
 assert.equal(await icon.getAttribute('aria-busy'),'true');
 assert.equal(page.url(),process.env.FLIPAI_UPDATE_TEST_URL+'/','installation must not navigate');
 for (const path of ['/agents','/connections','/activity','/settings']) {
  await page.goto(process.env.FLIPAI_UPDATE_TEST_URL+path);
  await icon.waitFor();
  assert.equal(await page.locator('#flipai-update-install').count(),1);
  assert.equal(await page.locator('.banner.update,form[action="/update/install"],form[action="/update/check"]').count(),0);
 }
 // A failed launch quietly restores the same icon and permits a retry.
 failInstall=true;
 await icon.click();
 await page.waitForFunction(()=>!document.querySelector('#flipai-update-install')?.disabled);
 assert.equal(installs,2);
 assert.equal((await icon.textContent()).trim(),'');
 status={available:false};
 await page.waitForFunction(()=>!document.querySelector('#flipai-update-install'));
 assert.deepEqual(errors,[]);
 console.log('PASS: icon-only progress, ready, focus, single install, quiet retry, all pages');
} finally { await browser.close(); }
