package main

import "strings"

// This is the final Agents-page presentation pass. The older agent panes keep
// their stored internal parser prefixes for backward compatibility, while the
// UI shows only the fixed public shortcuts users actually text.
func init() {
	registerPage("agents", smsRouteAgentsUI(museChatDirectUI(copilotChatDirectUI(exactWebAgentsHTML()))))
}

func smsRouteAgentsUI(body string) string {
	const oldHeader = `<p>C: selects Codex, A: selects Claude, G: selects ChatGPT Chat, H: selects Claude Chat, M: selects Gemini Chat, X: selects Grok Chat, and P: selects Microsoft Copilot Chat. After a selection, unprefixed follow-up texts stay with that agent until you switch again.</p>`
	const newHeader = `<p><b>SMS shortcuts:</b> O = ChatGPT Chat · OW = ChatGPT Work · OC = Codex · A = Claude Chat · AW = Claude Cowork · AC = Claude Code Web · AL = Claude Code Local · G = Gemini · M = Microsoft Copilot · MU = Muse · X = Grok. Add <b>NEW</b> after any shortcut to start fresh, for example <b>OW NEW: research this</b>.</p>`
	body = strings.Replace(body, oldHeader, newHeader, 1)

	replacements := []struct{ old, new string }{
		{`Answers {{.S.CodexPrefix}}: messages`, `Answers OC: messages`},
		{`Answers {{.S.ClaudePrefix}}: messages`, `Answers AL: messages`},
		{`Answers {{.ChatGPTAccess.Prefix}}: messages`, `O = Chat · OW = Work`},
		{`Answers {{.ClaudeChatAccess.Prefix}}: messages`, `A = Chat · AW = Cowork · AC = Code Web`},
		{`Answers {{.GeminiChatAccess.Prefix}}: messages`, `Answers G: messages`},
		{`Answers {{.GrokChatAccess.Prefix}}: messages`, `Answers X: messages`},
		{`Answers {{.CopilotChatAccess.Prefix}}: messages`, `Answers M: messages`},
		{`Answers {{.MuseChatAccess.Prefix}}: messages`, `Answers MU: messages`},
	}
	for _, r := range replacements {
		body = strings.ReplaceAll(body, r.old, r.new)
	}

	const script = `
<style>
  .public-shortcut-input[readonly]{background:var(--panel-soft,#f7f7fb);cursor:default;font-weight:650}
</style>
<script>
(function(){
  function ready(fn){if(document.readyState==='loading'){document.addEventListener('DOMContentLoaded',fn)}else{fn()}}
  ready(function(){
    var header=document.querySelector('.page-head > div:first-child > p');
    if(header){header.textContent='SMS shortcuts: O = ChatGPT Chat · OW = ChatGPT Work · OC = Codex · A = Claude Chat · AW = Claude Cowork · AC = Claude Code Web · AL = Claude Code Local · G = Gemini · M = Microsoft Copilot · MU = Muse · X = Grok. Add NEW after any shortcut to start fresh, for example OW NEW: research this.'}

    function rail(id,text){var el=document.querySelector('.agent-item[for="'+id+'"] .agent-item-copy > span:last-child');if(el)el.textContent=text}
    rail('agent-codex','Answers OC: messages');
    rail('agent-claude','Answers AL: messages');
    rail('agent-chatgpt','O = Chat · OW = Work');
    rail('agent-claude-chat','A = Chat · AW = Cowork · AC = Code Web');
    rail('agent-gemini-chat','Answers G: messages');
    rail('agent-grok-chat','Answers X: messages');
    rail('agent-copilot-chat','Answers M: messages');
    rail('agent-muse-chat','Answers MU: messages');

    function renameWithStatus(selector,name){var el=document.querySelector(selector);if(!el)return;for(var i=0;i<el.childNodes.length;i++){if(el.childNodes[i].nodeType===3&&el.childNodes[i].nodeValue.trim()){el.childNodes[i].nodeValue=name+' ';return}}}
    renameWithStatus('.agent-item[for="agent-claude"] b','Claude Code Local');
    renameWithStatus('#claude-pane h2','Claude Code Local');

    var firstNew=document.querySelector('input[name="newSessionCommand"]');
    var newWord=firstNew&&firstNew.value.trim()?firstNew.value.trim():'NEW';
    var fields={
      codexPrefix:{value:'OC',hint:'Text OC: to use Codex. Start a new conversation and run the first task with OC '+newWord+': your task.'},
      claudePrefix:{value:'AL',hint:'Text AL: to use Claude Code Local. Start a new local session with AL '+newWord+': your task.'},
      chatgptPrefix:{value:'O = Chat · OW = Work',hint:'Use O: for ChatGPT Chat or OW: for ChatGPT Work. Start fresh with O '+newWord+': or OW '+newWord+':.'},
      claudeChatPrefix:{value:'A = Chat · AW = Cowork · AC = Code Web',hint:'Use A: for Claude Chat, AW: for Claude Cowork, or AC: for Claude Code Web. Add '+newWord+' after any shortcut to start fresh.'},
      geminiChatPrefix:{value:'G',hint:'Text G: to use Gemini. Start fresh with G '+newWord+': your task.'},
      grokChatPrefix:{value:'X',hint:'Text X: to use Grok. Start fresh with X '+newWord+': your task.'},
      copilotChatPrefix:{value:'M',hint:'Text M: to use Microsoft Copilot. Start fresh with M '+newWord+': your task.'},
      museChatPrefix:{value:'MU',hint:'Text MU: to use Muse. Start fresh with MU '+newWord+': your task.'}
    };
    Object.keys(fields).forEach(function(id){
      var old=document.getElementById(id);if(!old)return;
      var field=old.closest('.field');if(!field)return;
      old.type='hidden';old.style.display='none';
      var visible=document.createElement('input');
      visible.type='text';visible.readOnly=true;visible.className='public-shortcut-input';visible.id=id+'Public';visible.value=fields[id].value;
      old.insertAdjacentElement('afterend',visible);
      var label=field.querySelector('label[for="'+id+'"]');if(label){label.htmlFor=visible.id;label.textContent='SMS shortcut'}
      var hint=field.querySelector('.hint');if(hint)hint.textContent=fields[id].hint;
    });

    document.querySelectorAll('input[name="newSessionCommand"]').forEach(function(input){
      var field=input.closest('.field');if(!field)return;
      var hint=field.querySelector('.hint');
      if(hint)hint.textContent='Put '+newWord+' after any shortcut to start a new chat/session and optionally run the first task. Example: OW '+newWord+': research this.';
    });

    function securityHint(pane,shortcut){
      document.querySelectorAll(pane+' .hint').forEach(function(h){
        if(h.textContent.indexOf('yourcode')>=0){h.textContent='Security-code example: yourcode '+shortcut+': check the build.'}
      });
    }
    securityHint('#codex-pane','OC');
    securityHint('#claude-pane','AL');
    securityHint('#chatgpt-pane','O');
    securityHint('#claude-chat-pane','A');
    securityHint('#gemini-chat-pane','G');
    securityHint('#grok-chat-pane','X');
    securityHint('#copilot-chat-pane','M');
    securityHint('#muse-chat-pane','MU');
  });
})();
</script>
`
	idx := strings.LastIndex(body, "{{end}}")
	if idx < 0 {
		panic("FlipAi Agents template changed around public SMS route UI insertion")
	}
	return body[:idx] + script + body[idx:]
}
