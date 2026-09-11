//go:build windows

package main

import (
	"encoding/json"
	"errors"
	"strings"
	"time"
)

const browserLongTurnSnapshotMarker = "__FLIPAI_BROWSER_LONG_TURN_SNAPSHOT__"

func browserLongTurnProviderFromExpression(expression string) string {
	s := strings.ToLower(expression)
	switch {
	case strings.Contains(s, "chatgpt is loaded") || strings.Contains(s, "chatgpt completed the turn"):
		return "chatgpt"
	case strings.Contains(s, "claude is loaded") || strings.Contains(s, "claude completed the turn"):
		return "claude-chat"
	case strings.Contains(s, "gemini is loaded") || strings.Contains(s, "gemini started answering"):
		return "gemini"
	case strings.Contains(s, "grok is loaded") || strings.Contains(s, "grok completed the turn"):
		return "grok"
	case strings.Contains(s, "microsoft copilot is loaded") || strings.Contains(s, "microsoft copilot completed the turn"):
		return "copilot"
	default:
		return ""
	}
}

func browserLongTurnDataDir() string {
	dataDir, _, _, _, err := appPaths()
	if err != nil {
		return ""
	}
	return dataDir
}

func beginBrowserLongTurn(expression string) string {
	provider := browserLongTurnProviderFromExpression(expression)
	if provider == "" {
		return ""
	}
	if dataDir := browserLongTurnDataDir(); dataDir != "" {
		clearBrowserLongTurnState(dataDir, provider)
	}
	return provider
}

type browserTurnResultValue struct {
	OK     bool   `json:"ok"`
	Reply  string `json:"reply"`
	Detail string `json:"detail"`
	Href   string `json:"href"`
}

// browserTurnValueFromDevTools unwraps Runtime.evaluate's protocol envelope and
// then the page script's JSON value. This is deliberately provider-neutral: all
// browser drivers return the same ok/reply/detail/href shape.
func browserTurnValueFromDevTools(raw string) (browserTurnResultValue, bool) {
	var envelope struct {
		Result struct {
			Value json.RawMessage `json:"value"`
		} `json:"result"`
	}
	if json.Unmarshal([]byte(raw), &envelope) != nil || len(envelope.Result.Value) == 0 {
		return browserTurnResultValue{}, false
	}
	var value browserTurnResultValue
	if json.Unmarshal(envelope.Result.Value, &value) != nil {
		return browserTurnResultValue{}, false
	}
	return value, true
}

// This snapshot deliberately reads only final assistant output and provider
// failure/busy state. It must never inspect aria-live/status/progress UI because
// those nodes routinely contain model thinking, tool status, confirmation-card
// controls, and other transient text that must not be forwarded to the phone.
const browserLongTurnSnapshotJS = `/*` + browserLongTurnSnapshotMarker + `*/(()=>{
  const roots=()=>{const out=[document],seen=new Set(out);for(let i=0;i<out.length;i++){for(const n of out[i].querySelectorAll('*')){if(n.shadowRoot&&!seen.has(n.shadowRoot)){seen.add(n.shadowRoot);out.push(n.shadowRoot)}}}return out};
  const visible=n=>{if(!n)return false;const r=n.getBoundingClientRect?.();if(r&&r.width<=0&&r.height<=0)return false;const s=getComputedStyle(n);return s.display!=='none'&&s.visibility!=='hidden'};
  const text=n=>String(n&&n.innerText||n&&n.textContent||'').replace(/\s+/g,' ').trim();
  const all=sel=>{const out=[];for(const r of roots())for(const n of r.querySelectorAll(sel))if(visible(n))out.push(n);return Array.from(new Set(out))};
  const assistantSelectors=[
    '[data-message-author-role="assistant"]','[data-content="ai-message"]','[data-author="assistant"]','[data-author="bot"]',
    '[data-testid="assistant-message"]','[data-testid*="assistant" i]','[data-testid*="bot" i]',
    'model-response','[data-test-id="model-response"]','[data-testid="model-response"]','.model-response-text',
    '.font-claude-response-body','.font-claude-message','[data-testid="grokResponse"]',
    '[class*="response-message" i]','[class*="assistant-message" i]'
  ];
  const assistants=[];for(const sel of assistantSelectors)assistants.push(...all(sel));
  let reply='';for(let i=assistants.length-1;i>=0;i--){const t=text(assistants[i]);if(t){reply=t;break}}
  const controls=all('button,[role="button"]');
  const ctl=n=>((n.getAttribute('aria-label')||'')+' '+(n.getAttribute('title')||'')+' '+text(n)).toLowerCase();
  const working=controls.some(n=>/\b(stop|cancel generation|cancel response|cancel task)\b/.test(ctl(n)));
  const errorNodes=[...all('[role="alert"]'),...all('[aria-live="assertive"]'),...all('[data-testid*="error" i]'),...all('[data-test-id*="error" i]'),...all('[class*="error-message" i]')];
  let failure='';
  const failureRE=/(something went wrong|network error|connection (?:was )?(?:lost|failed)|failed to (?:respond|generate|complete|send)|response failed|generation failed|task failed|server error|service unavailable|try again)/i;
  for(let i=errorNodes.length-1;i>=0;i--){const t=text(errorNodes[i]);if(t&&t.length<=500&&failureRE.test(t)){failure=t;break}}
  return {reply,working,failure,href:String(location.href||'')};
})()`

type browserLongTurnSnapshot struct {
	Reply   string `json:"reply"`
	Working bool   `json:"working"`
	Failure string `json:"failure"`
	Href    string `json:"href"`
}

func readBrowserLongTurnSnapshot(d voiceDevTools) (browserLongTurnSnapshot, error) {
	if d == nil {
		return browserLongTurnSnapshot{}, errors.New("browser control channel is unavailable")
	}
	var snapshot browserLongTurnSnapshot
	if err := voiceEval(d, browserLongTurnSnapshotJS, true, &snapshot); err != nil {
		return browserLongTurnSnapshot{}, err
	}
	return snapshot, nil
}

// continueBrowserLongTurn is started only after the provider's legacy 90-second
// page checkpoint fires. It never has an elapsed-time deadline. The WebView is
// sampled until the provider visibly finishes or visibly fails. Only final
// assistant output is retained; intermediate/thought/status text is ignored.
func continueBrowserLongTurn(d voiceDevTools, provider string, started bool) {
	provider = browserLongTurnProvider(provider)
	dataDir := browserLongTurnDataDir()
	if provider == "" || dataDir == "" || d == nil {
		return
	}
	_ = saveBrowserLongTurnState(dataDir, browserLongTurnState{Provider: provider, Status: browserLongTurnPending})

	baseline := ""
	seenResponse := started
	lastReply := ""
	stable := 0
	consecutiveControlErrors := 0
	if snap, err := readBrowserLongTurnSnapshot(d); err == nil && !started {
		baseline = strings.TrimSpace(snap.Reply)
	}

	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for range ticker.C {
		snap, err := readBrowserLongTurnSnapshot(d)
		if err != nil {
			consecutiveControlErrors++
			if consecutiveControlErrors >= 3 {
				_ = saveBrowserLongTurnState(dataDir, browserLongTurnState{Provider: provider, Status: browserLongTurnFailed, Detail: "The browser session stopped responding while the model was working: " + err.Error()})
				return
			}
			continue
		}
		consecutiveControlErrors = 0
		if failure := strings.TrimSpace(snap.Failure); failure != "" {
			_ = saveBrowserLongTurnState(dataDir, browserLongTurnState{Provider: provider, Status: browserLongTurnFailed, Reply: snap.Reply, Detail: failure})
			return
		}

		reply := strings.TrimSpace(snap.Reply)
		if !seenResponse && reply != "" && reply != baseline {
			seenResponse = true
		}
		if seenResponse && reply != "" {
			if reply == lastReply {
				stable++
			} else {
				lastReply = reply
				stable = 0
			}
		} else {
			stable = 0
		}

		// Provider image placeholders are explicitly non-final even if their Stop
		// control temporarily disappears while the image tool swaps UI states.
		pendingImage := browserChatReplySuggestsPendingImage(reply)
		if seenResponse && reply != "" && !snap.Working && !pendingImage && stable >= 2 {
			_ = saveBrowserLongTurnState(dataDir, browserLongTurnState{Provider: provider, Status: browserLongTurnDone, Reply: reply})
			return
		}
	}
}
