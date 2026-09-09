import assert from 'node:assert/strict';
const {chromium}=await import(process.env.FLIPAI_PLAYWRIGHT_MODULE);
const scripts=await (await fetch(process.env.FLIPAI_VOICE_NOTE_TEST_URL)).json();
const browser=await chromium.launch({args:['--no-sandbox']});
try {
 const page=await browser.newPage();
 const cdp=await page.context().newCDPSession(page);
 const evaluate=async expression=>(await cdp.send('Runtime.evaluate',{expression,returnByValue:true,awaitPromise:true})).result.value;
 const patterns=[['ChatGPT',''],['Claude','.m4a,.mp3'],['Grok','audio/*'],['Copilot','audio/mp4'],['Muse','*/*'],['Gemini','.m4a,.wav,.mp3']];
 // These fixtures exercise picker patterns, not provider account permissions.
 for(const [provider,accept] of patterns){
  await page.setContent(`<main><h1>${provider}</h1><div><input id="images" type="file" accept="image/*"><input id="files" type="file" accept="${accept}"><div id="receipt"></div><textarea></textarea></div></main>`);
  const picker=await cdp.send('Runtime.evaluate',{expression:scripts.picker,returnByValue:false});
  assert.equal((await cdp.send('Runtime.callFunctionOn',{objectId:picker.result.objectId,functionDeclaration:'function(){return this.id}',returnByValue:true})).result.value,'files',provider+' must not use image-only input');
  await page.evaluate(name=>{
   document.querySelector('#files').addEventListener('change',()=>{
    const receipt=document.querySelector('#receipt');
    receipt.textContent=name;
    receipt.setAttribute('aria-busy','true');
    setTimeout(()=>receipt.removeAttribute('aria-busy'),1100);
   });
  },scripts.name);
  await evaluate(scripts.begin);
  await cdp.send('DOM.setFileInputFiles',{objectId:picker.result.objectId,files:[scripts.path]});
  assert.equal((await evaluate(scripts.status)).ready,false,'a pending upload must not be sent');
  await page.waitForTimeout(1200);
  let result;
  for(let i=0;i<4;i++){result=await evaluate(scripts.status);await page.waitForTimeout(100);}
  assert.equal(result.ready,true,provider+' upload receipt');
  assert.equal(await page.locator('#files').evaluate(async input=>await input.files[0].text()),'test recording bytes','original recording must reach file input');
 }
 // A chat that only permits images must not receive a disguised audio file.
 await page.setContent('<input type="file" accept="image/*"><textarea></textarea>');
 assert.equal(await evaluate(scripts.picker),null);
 // Opening a menu should find its extension-based file picker.
 await page.setContent('<button id="attach">Attach</button><textarea></textarea>');
 await page.evaluate(()=>document.querySelector('#attach').onclick=()=>{const input=document.createElement('input');input.type='file';input.accept='.m4a';input.id='audio';document.body.append(input)});
 const menuPicker=await cdp.send('Runtime.evaluate',{expression:scripts.picker,returnByValue:false});
 assert.ok(menuPicker.result.objectId);
 // A rejected upload must stop the turn, even if a filename receipt is present.
 await page.setContent('<div id="receipt"></div><textarea></textarea>');
 await evaluate(scripts.begin);
 await page.evaluate(name=>{document.querySelector('#receipt').textContent=name;const error=document.createElement('div');error.role='alert';error.textContent='File type not supported';document.body.append(error)},scripts.name);
 assert.match((await evaluate(scripts.status)).error,/not supported/);
 // Selection without an accepted receipt must never become ready.
 await page.setContent('<input type="file"><textarea></textarea>');
 await evaluate(scripts.begin);
 await page.waitForTimeout(1100);
 for(let i=0;i<4;i++)assert.equal((await evaluate(scripts.status)).ready,false);
 // MMS audio links often have an opaque URL and generic response type.
 await page.setContent('<div style="height:40px">MMS Received</div><div><a id="note">recording.amr</a></div><textarea style="margin-top:30px"></textarea>');
 await page.evaluate(()=>document.querySelector('#note').href=URL.createObjectURL(new Blob(['#!AMR\nvoice-data'],{type:'application/octet-stream'})));
 let capture=await evaluate(scripts.capture);
 assert.equal(capture.ok,true,JSON.stringify(capture));
 assert.equal(capture.attachments[0].mediaType,'audio/amr');
 assert.equal(Buffer.from(capture.attachments[0].data,'base64').toString(),'#!AMR\nvoice-data');
 // A hidden audio element may be paired with visible custom player controls.
 await page.setContent('<div>MMS Received</div><div style="height:60px"><button>Voice recording</button><audio style="display:none" id="note"></audio></div><textarea></textarea>');
 await page.evaluate(()=>document.querySelector('#note').src=URL.createObjectURL(new Blob(['OggSvoice-data'],{type:'application/octet-stream'})));
 capture=await evaluate(scripts.capture);
 assert.equal(capture.ok,true,JSON.stringify(capture));
 assert.equal(capture.attachments[0].mediaType,'audio/ogg');
 console.log('PASS: recording types, compatible pickers, upload completion, rejection, binary MMS, hidden audio');
} finally {await browser.close();}
