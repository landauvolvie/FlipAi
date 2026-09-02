from pathlib import Path


def replace_once(path, old, new):
    p = Path(path)
    text = p.read_text(encoding='utf-8')
    if old not in text:
        raise SystemExit(f'expected block not found in {path}')
    if text.count(old) != 1:
        raise SystemExit(f'expected exactly one block in {path}, found {text.count(old)}')
    p.write_text(text.replace(old, new, 1), encoding='utf-8')

# 1) Bind ChatGPT extraction to the new turn structurally. Old assistant nodes
# may mutate while ChatGPT renders controls/images; they must never count as the
# reply to the SMS that was just sent.
old_js = '''const chatGPTTurnJS = `(async(input)=>{
  const sleep=ms=>new Promise(r=>setTimeout(r,ms));
  const text=n=>(n&&n.innerText||n&&n.textContent||'').trim();
  const assistants=()=>Array.from(document.querySelectorAll('[data-message-author-role="assistant"]'));
  const current=()=>{const a=assistants();return a.length?text(a[a.length-1]):''};
  const composer=()=>document.querySelector('#prompt-textarea,textarea[data-testid="prompt-textarea"],[data-testid="prompt-textarea"],[contenteditable="true"][data-virtualkeyboard],[contenteditable="true"]');
  const send=()=>document.querySelector('button[data-testid="send-button"],button[aria-label="Send prompt"],button[aria-label^="Send" i]');
  const stop=()=>document.querySelector('button[data-testid="stop-button"],button[aria-label^="Stop" i]');
  let c=null;
  for(let i=0;i<100&&!c;i++){c=composer();if(!c)await sleep(200);}
  if(!c)return {ok:false,detail:'ChatGPT is loaded but FlipAi could not find the message composer. The site layout may have changed.',href:location.href};
  const before=current();
  c.focus();
  if(c.tagName==='TEXTAREA'||c.tagName==='INPUT'){
    const setter=Object.getOwnPropertyDescriptor(c.tagName==='TEXTAREA'?HTMLTextAreaElement.prototype:HTMLInputElement.prototype,'value').set;
    setter.call(c,input); c.dispatchEvent(new Event('input',{bubbles:true}));
  }else{
    c.innerHTML='';
    const p=document.createElement('p');p.textContent=input;c.appendChild(p);
    c.dispatchEvent(new InputEvent('input',{bubbles:true,inputType:'insertText',data:input}));
  }
  await sleep(120);
  let b=null;
  for(let i=0;i<50&&!b;i++){b=send();if(!b||b.disabled){b=null;await sleep(100);}}
  if(!b)return {ok:false,detail:'FlipAi filled the ChatGPT composer but the Send button never became ready.',href:location.href};
  b.click();
  let last='',stable=0,started=false;
  const deadline=Date.now()+90000;
  while(Date.now()<deadline){
    await sleep(250);
    const now=current();
    if(now&&now!==before)started=true;
    if(started&&now){
      if(now===last)stable++;else{last=now;stable=0;}
      if(!stop()&&stable>=5)return {ok:true,reply:now,href:location.href};
    }
  }
  return {ok:false,detail:started?'ChatGPT started answering but did not finish within 90 seconds.':'ChatGPT did not produce an assistant response within 90 seconds.',href:location.href};
})(%s)`'''
new_js = '''const chatGPTTurnJS = `(async(input)=>{
  const sleep=ms=>new Promise(r=>setTimeout(r,ms));
  const text=n=>(n&&n.innerText||n&&n.textContent||'').trim();
  const users=()=>Array.from(document.querySelectorAll('[data-message-author-role="user"]'));
  const assistants=()=>Array.from(document.querySelectorAll('[data-message-author-role="assistant"]'));
  const composer=()=>document.querySelector('#prompt-textarea,textarea[data-testid="prompt-textarea"],[data-testid="prompt-textarea"],[contenteditable="true"][data-virtualkeyboard],[contenteditable="true"]');
  const send=()=>document.querySelector('button[data-testid="send-button"],button[aria-label="Send prompt"],button[aria-label^="Send" i]');
  const stop=()=>document.querySelector('button[data-testid="stop-button"],button[aria-label^="Stop" i]');
  let c=null;
  for(let i=0;i<100&&!c;i++){c=composer();if(!c)await sleep(200);}
  if(!c)return {ok:false,detail:'ChatGPT is loaded but FlipAi could not find the message composer. The site layout may have changed.',href:location.href};
  const beforeUserCount=users().length;
  const beforeAssistantCount=assistants().length;
  const assistantForThisTurn=()=>{
    const us=users();
    const as=assistants();
    const newUser=us.length>beforeUserCount?us[us.length-1]:null;
    if(newUser){
      for(let i=as.length-1;i>=0;i--){
        if(newUser.compareDocumentPosition(as[i])&Node.DOCUMENT_POSITION_FOLLOWING)return as[i];
      }
    }
    return as.length>beforeAssistantCount?as[as.length-1]:null;
  };
  c.focus();
  if(c.tagName==='TEXTAREA'||c.tagName==='INPUT'){
    const setter=Object.getOwnPropertyDescriptor(c.tagName==='TEXTAREA'?HTMLTextAreaElement.prototype:HTMLInputElement.prototype,'value').set;
    setter.call(c,input); c.dispatchEvent(new Event('input',{bubbles:true}));
  }else{
    c.innerHTML='';
    const p=document.createElement('p');p.textContent=input;c.appendChild(p);
    c.dispatchEvent(new InputEvent('input',{bubbles:true,inputType:'insertText',data:input}));
  }
  await sleep(120);
  let b=null;
  for(let i=0;i<50&&!b;i++){b=send();if(!b||b.disabled){b=null;await sleep(100);}}
  if(!b)return {ok:false,detail:'FlipAi filled the ChatGPT composer but the Send button never became ready.',href:location.href};
  b.click();
  let last='',stable=0,started=false;
  const deadline=Date.now()+90000;
  while(Date.now()<deadline){
    await sleep(250);
    const node=assistantForThisTurn();
    if(node){
      started=true;
      const now=text(node);
      if(now===last)stable++;else{last=now;stable=0;}
      if(!stop()&&stable>=5)return {ok:true,reply:now||'I generated the image.',href:location.href};
    }
  }
  return {ok:false,detail:started?'ChatGPT started answering but did not finish within 90 seconds.':'ChatGPT did not produce an assistant response within 90 seconds.',href:location.href};
})(%s)`'''
replace_once('chatgpt_webview_windows.go', old_js, new_js)

# 2) ChatGPT should show the user's plain message, then the shared short SMS
# instruction. Do not expose the internal XML-style fence in the web chat.
old_sms = '''func (b *Bridge) runChatGPTSMS(ctx context.Context, command string) (string, error) {
\tdataDir := filepath.Dir(b.statePath)
\treturn chatGPTBrowserSend(ctx, dataDir, b.composePrompt("G", command))
}'''
new_sms = '''func (b *Bridge) composeChatGPTSMSPrompt(command string) string {
\tcommand = strings.TrimSpace(command)
\thint := strings.TrimSpace(b.cfg.replyStyleHintFor("G"))
\tif hint == "" {
\t\thint = defaultReplyStyleHint
\t}
\tif hint == "" {
\t\treturn command
\t}
\treturn command + "\\n\\n" + hint
}

func (b *Bridge) runChatGPTSMS(ctx context.Context, command string) (string, error) {
\tdataDir := filepath.Dir(b.statePath)
\treturn chatGPTBrowserSend(ctx, dataDir, b.composeChatGPTSMSPrompt(command))
}'''
replace_once('sms_sticky_chatgpt.go', old_sms, new_sms)

# 3) Make the real-browser harness reproduce the live bug: an old assistant
# response mutates before the new assistant node exists. The driver must ignore
# that old node and return only the new turn.
p = Path('testdata/chatgpt/drive.mjs')
text = p.read_text(encoding='utf-8')
old_html = '''await page.setContent(`<!doctype html><html><body>
  <textarea id="prompt-textarea"></textarea>
  <button data-testid="send-button">Send</button>
  <div id="messages"></div>
  <script>
    document.querySelector('[data-testid="send-button"]').addEventListener('click',()=>{
      const value=document.querySelector('#prompt-textarea').value;
      const stop=document.createElement('button');stop.dataset.testid='stop-button';stop.textContent='Stop';document.body.appendChild(stop);
      const msg=document.createElement('div');msg.setAttribute('data-message-author-role','assistant');document.querySelector('#messages').appendChild(msg);
      setTimeout(()=>msg.textContent='FLIPAI',120);
      setTimeout(()=>msg.textContent='FLIPAI browser response for '+value,280);
      setTimeout(()=>stop.remove(),520);
    });
  </script>
</body></html>`);'''
new_html = '''await page.setContent(`<!doctype html><html><body>
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
</body></html>`);'''
if old_html not in text:
    raise SystemExit('browser harness block not found')
p.write_text(text.replace(old_html, new_html, 1), encoding='utf-8')

# 4) Regression test for the clean ChatGPT SMS prompt.
Path('chatgpt_sms_prompt_v04618_test.go').write_text('''package main

import (
    "strings"
    "testing"
)

func TestChatGPTSMSPromptIsPlainAndShort(t *testing.T) {
    cfg := defaultConfig(t.TempDir())
    b := &Bridge{cfg: cfg}
    got := b.composeChatGPTSMSPrompt("Generate me an image of a nice waterfall")
    want := "Generate me an image of a nice waterfall\\n\\n" + defaultReplyStyleHint
    if got != want {
        t.Fatalf("unexpected ChatGPT SMS prompt:\\n%q\\nwant:\\n%q", got, want)
    }
    if strings.Contains(got, "<sms_command>") || strings.Contains(got, "</sms_command>") {
        t.Fatalf("internal SMS wrapper leaked into ChatGPT: %q", got)
    }
}
''', encoding='utf-8')

# 5) Version + release notes.
replace_once('config.go', 'const version = "0.46.17"', 'const version = "0.46.18"')
Path('VERSION').write_text('0.46.18\\n', encoding='utf-8')
Path('docs/RELEASE-NOTES.md').write_text('''# FlipAi v0.46.18

This hotfix fixes two regular ChatGPT SMS regressions visible in the ChatGPT and Google Voice screenshots.

## Only the new ChatGPT turn is returned

FlipAi previously identified a new ChatGPT answer by checking whether the text of the last assistant element had changed. ChatGPT can update an older assistant element while it renders controls or an image, so a later SMS could accidentally reuse the previous turn's answer. That is why an image request could return the earlier Gmail summary followed by FlipAi's image-delivery notice.

v0.46.18 records the user and assistant message boundaries before sending the SMS and accepts only an assistant node created for the new turn. Mutations to older assistant messages are ignored. Image-only turns are also recognized as the new turn even when their assistant node has no text.

## Clean SMS prompts in ChatGPT

Regular ChatGPT no longer sees the internal `<sms_command>` wrapper. The web chat now shows only the user's message, followed by the shared short SMS instruction, for example:

`Generate me an image of a nice waterfall`

`Reply for SMS. Keep it brief and plain text.`

The same shared instruction remains editable in FlipAi settings.

## Verification

The real Chromium browser test now deliberately mutates an old assistant response before creating the new response. The test fails with the old extraction rule and passes only when FlipAi returns the new turn. A Go regression test also verifies that ChatGPT SMS prompts contain no internal wrapper.
''', encoding='utf-8')

print('v0.46.18 patch applied')
