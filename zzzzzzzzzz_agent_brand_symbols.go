package main

import (
	"html/template"
	"strings"
)

// Final brand pass: use the symbols supplied by the user everywhere agent
// identity is shown, without changing updater behavior or agent execution.
func agentBrandSVG(bg, body string) template.HTML {
	return template.HTML(`<svg viewBox="0 0 96 96" aria-hidden="true"><rect width="96" height="96" fill="` + bg + `"/>` + body + `</svg>`)
}

var suppliedAgentBrandMarks = map[string]template.HTML{
	"chatgpt": agentBrandSVG("#fff", `<path d="M39 7L38 8L33 9L31 11L30 11L27 14L27 15L25 17L25 18L23 21L16 24L10 30L10 31L8 34L8 37L7 38L7 45L8 46L8 48L9 49L10 52L12 54L12 58L11 59L11 63L12 64L12 67L13 68L13 70L14 71L14 72L21 79L22 79L23 80L25 80L26 81L30 81L31 82L36 81L40 85L41 85L44 87L47 87L48 88L55 88L56 87L58 87L59 86L64 84L67 81L67 80L69 78L70 75L72 73L74 73L75 72L78 71L84 65L84 64L86 61L86 58L87 57L87 50L86 49L86 47L85 46L84 43L82 41L82 37L83 36L83 32L82 31L82 28L81 27L81 25L80 24L80 23L73 16L72 16L71 15L69 15L68 14L65 14L64 13L61 13L60 14L58 14L54 10L53 10L50 8L48 8L47 7ZM56 58L56 64L54 66L53 66L52 67L51 67L49 69L48 69L47 70L46 70L44 72L43 72L42 73L41 73L40 74L39 74L38 75L37 75L36 76L28 76L27 75L25 75L23 73L22 73L20 71L20 70L18 68L18 67L17 66L17 60L18 59L19 60L20 60L22 62L23 62L24 63L25 63L26 64L27 64L29 66L30 66L31 67L32 67L33 68L37 68L38 67L39 67L41 65L42 65L43 64L44 64L46 62L47 62L48 61L49 61L50 60L51 60L53 58L54 58L55 57ZM60 45L61 46L62 46L63 47L64 47L66 49L66 72L65 73L65 74L64 75L64 76L60 80L59 80L58 81L57 81L56 82L53 82L52 83L50 83L49 82L46 82L45 81L44 81L42 79L43 78L44 78L45 77L46 77L48 75L49 75L50 74L51 74L52 73L53 73L55 71L56 71L57 70L58 70L59 69L59 46ZM46 38L48 38L49 39L50 39L51 40L52 40L53 41L54 41L56 43L56 52L55 53L54 53L52 55L51 55L50 56L49 56L48 57L46 57L45 56L44 56L43 55L42 55L40 53L39 53L38 52L38 43L39 42L40 42L42 40L43 40L44 39L45 39ZM52 34L53 34L54 33L55 33L56 32L59 32L60 33L61 33L62 34L63 34L65 36L66 36L67 37L68 37L69 38L70 38L72 40L73 40L74 41L75 41L79 45L79 46L80 47L80 48L81 49L81 52L82 53L82 55L81 56L81 59L80 60L80 61L78 63L78 64L77 65L76 65L74 67L73 67L72 66L72 48L70 46L69 46L68 45L67 45L65 43L64 43L63 42L62 42L61 41L60 41L58 39L57 39L56 38L55 38L53 36L52 36L51 35ZM20 28L21 28L22 29L22 47L24 49L25 49L26 50L27 50L29 52L30 52L31 53L32 53L33 54L34 54L36 56L37 56L38 57L39 57L40 58L41 58L42 59L42 60L40 62L39 62L38 63L35 63L34 62L33 62L32 61L31 61L29 59L28 59L27 58L26 58L25 57L24 57L22 55L21 55L20 54L19 54L15 50L15 49L14 48L14 47L13 46L13 43L12 42L12 40L13 39L13 36L14 35L14 34L16 32L16 31L17 30L18 30ZM77 29L77 35L76 36L75 35L74 35L72 33L71 33L70 32L69 32L67 30L66 30L65 29L64 29L63 28L62 28L61 27L60 27L59 26L58 27L57 27L56 28L55 28L53 30L52 30L51 31L50 31L49 32L48 32L46 34L45 34L44 35L43 35L41 37L40 37L39 38L38 37L38 31L40 29L41 29L43 27L44 27L45 26L46 26L47 25L48 25L50 23L51 23L52 22L53 22L54 21L55 21L56 20L57 20L58 19L66 19L67 20L69 20L71 22L72 22L74 24L74 25L76 27L76 28ZM48 13L49 14L50 14L52 16L51 17L50 17L49 18L48 18L46 20L45 20L44 21L43 21L42 22L41 22L39 24L38 24L37 25L36 25L35 26L35 49L34 50L33 49L32 49L31 48L30 48L28 46L28 23L29 22L29 21L30 20L30 19L34 15L35 15L36 14L37 14L38 13L40 13L41 12L44 12L45 13Z" fill="#111" fill-rule="evenodd"/>`),
	"codex": agentBrandSVG("#fff", `<defs><radialGradient id="codexBrandGradient" cx="42%" cy="28%" r="76%"><stop offset="0" stop-color="#cbbaff"/><stop offset=".45" stop-color="#8b8cff"/><stop offset="1" stop-color="#3657ff"/></radialGradient></defs><path d="M28 14L25 22L18 25L13 33L10 32L13 35L10 36L11 50L9 52L11 51L14 55L16 71L22 79L51 85L60 85L65 82L66 84L65 82L85 58L85 46L78 24L70 18L70 15L69 18L58 16L49 10ZM18 26L19 25L20 26L19 27ZM27 17L28 16L29 17L28 18Z" fill="url(#codexBrandGradient)" fill-rule="evenodd"/><path d="M30 34l8 14-8 14" fill="none" stroke="#fff" stroke-width="7" stroke-linecap="round" stroke-linejoin="round"/><path d="M49 59h16" fill="none" stroke="#fff" stroke-width="7" stroke-linecap="round"/>`),
	"claude-code": agentBrandSVG("#0b0d0f", `<path d="M10 43L10 52L19 53L19 61L24 63L24 71L29 71L30 62L34 63L34 71L38 71L39 62L56 62L57 71L62 70L61 63L65 62L66 71L71 71L71 63L76 61L76 53L85 52L85 43L76 42L75 24L20 24L19 42ZM62 34L65 34L66 35L66 42L65 43L63 43L62 42L62 36L61 35ZM30 34L33 34L34 35L33 36L33 42L32 43L30 43L29 42L29 35Z" fill="#dd7a57" fill-rule="evenodd"/>`),
	"claude": agentBrandSVG("#fff", `<path d="M31 13L28 18L38 35L22 25L18 31L38 45L13 48L39 51L20 66L38 58L29 77L45 61L45 82L51 62L66 77L63 66L72 71L75 68L62 56L82 56L79 51L65 49L81 45L81 40L62 42L72 21L66 21L55 34L56 15L52 14L47 35L37 15ZM62 65L63 64L64 65L63 66ZM61 64L62 63L63 64L62 65ZM60 63L61 62L62 63L61 64ZM45 60L46 59L47 60L46 61ZM61 55L62 54L63 55L62 56ZM37 36L38 35L39 36L38 37ZM53 35L54 34L55 35L54 36Z" fill="#dd7958" fill-rule="evenodd"/>`),
	"grok": agentBrandSVG("#111214", `<path d="M58 30L48 27L37 30L31 35L27 43L27 53L29 57L29 62L19 74L36 59L32 52L32 45L35 39L41 34L44 33L54 34L59 32ZM74 22L41 55L60 41L63 46L61 56L53 63L40 63L36 66L43 69L52 69L59 66L65 60L68 54L67 34Z" fill="#fff" fill-rule="evenodd"/>`),
	"gemini": template.HTML(`<svg viewBox="0 0 96 96" aria-hidden="true"><rect width="96" height="96" fill="#fff"/><defs><clipPath id="geminiBrandClip"><path d="M46 13L39 29L29 39L13 46L13 48L29 56L41 69L46 82L49 82L55 67L66 56L80 50L82 47L66 39L55 28L49 13Z"/></clipPath><radialGradient id="geminiBrandRed" cx="50%" cy="0%" r="78%"><stop offset="0" stop-color="#f04444"/><stop offset="1" stop-color="#f04444" stop-opacity="0"/></radialGradient><radialGradient id="geminiBrandYellow" cx="0%" cy="48%" r="72%"><stop offset="0" stop-color="#fbbc04"/><stop offset="1" stop-color="#fbbc04" stop-opacity="0"/></radialGradient><radialGradient id="geminiBrandGreen" cx="48%" cy="100%" r="75%"><stop offset="0" stop-color="#19b96f"/><stop offset="1" stop-color="#19b96f" stop-opacity="0"/></radialGradient></defs><g clip-path="url(#geminiBrandClip)"><rect width="96" height="96" fill="#4285f4"/><rect width="96" height="96" fill="url(#geminiBrandRed)"/><rect width="96" height="96" fill="url(#geminiBrandYellow)"/><rect width="96" height="96" fill="url(#geminiBrandGreen)"/></g></svg>`),
}

func addBrandSprite(name string) {
	for _, existing := range uiBrandSprites {
		if existing == name {
			return
		}
	}
	uiBrandSprites = append(uiBrandSprites, name)
}

func installSuppliedAgentBrandMarks() {
	for name, mark := range suppliedAgentBrandMarks {
		uiBrandMarks[name] = mark
		addBrandSprite(name)
	}
}

func correctedAgentsBrandHTML() string {
	body := grokChatDirectUI(geminiChatDirectUI(claudeChatDirectUI(chatGPTDirectUI(agentConnectFirstRunHTML(agentsPageHTML)))))

	// The first Claude marks are the local Claude CLI, i.e. Claude Code.
	body = strings.Replace(body, `<span class="bmark claude">{{brand "claude"}}</span>`, `<span class="bmark claude-code">{{brand "claude-code"}}</span>`, 1)
	body = strings.Replace(body, `<span class="bmark lg claude">{{brand "claude"}}</span>`, `<span class="bmark lg claude-code">{{brand "claude-code"}}</span>`, 1)

	// ChatGPT Chat was borrowing Codex.
	body = strings.Replace(body, `<label class="agent-item" for="agent-chatgpt">
        <span class="bmark codex">{{brand "codex"}}</span>`, `<label class="agent-item" for="agent-chatgpt">
        <span class="bmark chatgpt">{{brand "chatgpt"}}</span>`, 1)
	body = strings.Replace(body, `<span class="bmark lg codex">{{brand "codex"}}</span>
          <div>
            <h2>ChatGPT Chat`, `<span class="bmark lg chatgpt">{{brand "chatgpt"}}</span>
          <div>
            <h2>ChatGPT Chat`, 1)

	// Gemini was borrowing the generic Google G.
	body = strings.Replace(body, `<label class="agent-item" for="agent-gemini-chat">
        <span class="bmark google">{{brand "google"}}</span>`, `<label class="agent-item" for="agent-gemini-chat">
        <span class="bmark gemini">{{brand "gemini"}}</span>`, 1)
	body = strings.Replace(body, `<span class="bmark lg google">{{brand "google"}}</span>
          <div>
            <h2>Gemini Chat`, `<span class="bmark lg gemini">{{brand "gemini"}}</span>
          <div>
            <h2>Gemini Chat`, 1)

	// Grok was a generic X glyph.
	body = strings.Replace(body, `<span class="bmark grok">𝕏</span>`, `<span class="bmark grok">{{brand "grok"}}</span>`, 1)
	body = strings.Replace(body, `<span class="bmark lg grok">𝕏</span>`, `<span class="bmark lg grok">{{brand "grok"}}</span>`, 1)
	return body
}

const activityLogoBefore = `  function logo(agent){
    var m=AGENTS[agent];
    if(!m)return '<span class="activity2-logo"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8"><path d="M5 7h14v10H5zM8 4v3m8-3v3M8 12h.01M16 12h.01"/></svg></span>';
    if(m.kind==='codex')return '<span class="activity2-logo codex">&gt;_</span>';
    if(m.kind==='claude')return '<span class="activity2-logo claude"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M12 2.8v18.4M2.8 12h18.4M5.5 5.5l13 13M18.5 5.5l-13 13M8.2 3.8l7.6 16.4M20.2 8.2 3.8 15.8"/></svg></span>';
    if(m.kind==='grok')return '<span class="activity2-logo grok"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.4"><path d="M5 5l14 14M19 5 5 19"/><path d="M15.5 4.2h4.3v4.3"/></svg></span>';
    if(m.kind==='gemini')return '<span class="activity2-logo gemini"><svg viewBox="0 0 24 24"><path fill="currentColor" d="M12 1.8c.7 5.7 4.5 9.5 10.2 10.2-5.7.7-9.5 4.5-10.2 10.2C11.3 16.5 7.5 12.7 1.8 12 7.5 11.3 11.3 7.5 12 1.8z"/></svg></span>';
    return '<span class="activity2-logo chatgpt"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7"><path d="M12 3.1a4.2 4.2 0 0 1 4.1 3.2 4.2 4.2 0 0 1 2.1 7.4 4.2 4.2 0 0 1-6.1 5.7 4.2 4.2 0 0 1-7-4.2 4.2 4.2 0 0 1 .9-7.8A4.2 4.2 0 0 1 12 3.1z"/><path d="m8 8.2 4-2.3 4 2.3v4.6l-4 2.3-4-2.3zM12 15.1v4.1M8 12.8l-3.5 2M16 12.8l3.5 2"/></svg></span>';
  }`

const activityLogoAfter = `  function brandIcon(name){var el=document.getElementById('brand-'+name);return el?el.innerHTML:'';}
  function logo(agent){
    var m=AGENTS[agent];
    if(!m)return '<span class="activity2-logo"></span>';
    return '<span class="activity2-logo '+m.kind+'">'+brandIcon(m.kind)+'</span>';
  }`

func correctedActivityBrandHTML() string {
	body := activityRedesignHTML

	// Preserve the existing stage-filter compatibility pass.
	body = strings.Replace(body, `    <select data-activity-range aria-label="Filter by time">`, `    <select data-activity-stage aria-label="Filter by stage">
      <option value="">All stages</option>
      <option value="gmail">Gmail</option>
      <option value="routing">Routing</option>
      <option value="agent">Agent</option>
      <option value="reply">Reply</option>
      <option value="security">Security</option>
      <option value="bridge">Bridge</option>
      <option value="host">Host</option>
      <option value="startup">Startup</option>
    </select>
    <select data-activity-range aria-label="Filter by time">`, 1)
	body = strings.Replace(body, `.activity2-search-row{display:grid;grid-template-columns:minmax(0,1fr) 145px;gap:10px;margin-bottom:12px}`, `.activity2-search-row{display:grid;grid-template-columns:minmax(0,1fr) 135px 145px;gap:10px;margin-bottom:12px}`, 1)
	body = strings.Replace(body, `<p class="activity2-privacy">FlipAi logs message flow and status only.`, `<p class="activity2-privacy"><b>Privacy:</b> FlipAi logs message flow and status only.`, 1)
	body = strings.Replace(body, `var state={events:[],agent:'',query:'',hours:0,page:1,perPage:12};`, `var state={events:[],agent:'',stage:'',query:'',hours:0,page:1,perPage:12};`, 1)
	body = strings.Replace(body, `      if(state.agent&&eventAgent(e)!==state.agent)return false;
      if(cutoff&&new Date(e.time).getTime()<cutoff)return false;`, `      if(state.agent&&eventAgent(e)!==state.agent)return false;
      if(state.stage&&e.stage!==state.stage)return false;
      if(cutoff&&new Date(e.time).getTime()<cutoff)return false;`, 1)
	body = strings.Replace(body, `  var range=root.querySelector('[data-activity-range]');if(range)range.addEventListener('change',function(){state.hours=parseFloat(range.value)||0;state.page=1;render();});`, `  var stage=root.querySelector('[data-activity-stage]');if(stage)stage.addEventListener('change',function(){state.stage=stage.value||'';state.page=1;render();});
  var range=root.querySelector('[data-activity-range]');if(range)range.addEventListener('change',function(){state.hours=parseFloat(range.value)||0;state.page=1;render();});`, 1)

	// Claude Code and Claude Chat get separate symbols.
	body = strings.Replace(body, `A:{name:'Claude Code',company:'Anthropic',kind:'claude'}`, `A:{name:'Claude Code',company:'Anthropic',kind:'claude-code'}`, 1)
	body = strings.Replace(body, activityLogoBefore, activityLogoAfter, 1)
	return body
}

func init() {
	installSuppliedAgentBrandMarks()
	registerPage("agents", correctedAgentsBrandHTML())
	registerPage("activity", correctedActivityBrandHTML())
}
