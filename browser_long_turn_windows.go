//go:build windows

package main

import (
	"encoding/json"
	"errors"
	"strings"
	"sync"
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
	// Muse was missing here, so its turn expression resolved to no provider at
	// all: the continuation never started, and the SMS side then waited out its
	// whole extended window on a state file nothing was ever going to write.
	case strings.Contains(s, "muse is loaded") || strings.Contains(s, "muse completed the turn"):
		return "muse"
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

// browserLongTurnGeneration counts turns per provider. A continuation belongs
// to the turn that started it; when the next turn on that provider begins, the
// old sampler is superseded and must stop. Left running it kept polling the
// page underneath the new turn, and its DevTools calls collided with the page
// driver's -- "Overlapped I/O operation is in progress" -- failing a turn that
// was otherwise fine.
var (
	browserLongTurnGenMu sync.Mutex
	browserLongTurnGen   = map[string]uint64{}
)

func nextBrowserLongTurnGeneration(provider string) uint64 {
	browserLongTurnGenMu.Lock()
	defer browserLongTurnGenMu.Unlock()
	browserLongTurnGen[provider]++
	return browserLongTurnGen[provider]
}

func currentBrowserLongTurnGeneration(provider string) uint64 {
	browserLongTurnGenMu.Lock()
	defer browserLongTurnGenMu.Unlock()
	return browserLongTurnGen[provider]
}

func beginBrowserLongTurn(expression string) string {
	provider := browserLongTurnProviderFromExpression(expression)
	if provider == "" {
		return ""
	}
	nextBrowserLongTurnGeneration(provider)
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

// continueBrowserLongTurn is started after the provider's 90-second page
// checkpoint fires, or when that checkpoint itself does not come back in time.
// It keeps no elapsed-time deadline while the response is still changing, but a
// response that has stopped changing is delivered even if the page still claims
// to be working, and a page that reports nothing at all fails rather than
// leaving FlipAi to text "still working" forever. Only final assistant output
// is retained; intermediate/thought/status text is ignored.
func continueBrowserLongTurn(d voiceDevTools, provider string, started bool) {
	provider = browserLongTurnProvider(provider)
	dataDir := browserLongTurnDataDir()
	if provider == "" || dataDir == "" || d == nil {
		return
	}
	_ = saveBrowserLongTurnState(dataDir, browserLongTurnState{Provider: provider, Status: browserLongTurnPending})
	generation := currentBrowserLongTurnGeneration(provider)

	baseline := ""
	seenResponse := started
	lastReply := ""
	stable := 0
	consecutiveControlErrors := 0
	var idleSince time.Time
	if snap, err := readBrowserLongTurnSnapshot(d); err == nil && !started {
		baseline = strings.TrimSpace(snap.Reply)
	}

	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	watchUntil := time.Now().Add(browserLongTurnWatchCap)
	for range ticker.C {
		// A newer turn on this provider owns the page now. Stop without writing
		// state: this sampler's answer belongs to a turn nobody is waiting on,
		// and its DevTools calls would collide with the new turn's driver.
		if currentBrowserLongTurnGeneration(provider) != generation {
			return
		}
		// Working is page-inferred, so a page that always reports working would
		// otherwise keep this goroutine sampling for the life of the process.
		if time.Now().After(watchUntil) {
			_ = saveBrowserLongTurnState(dataDir, browserLongTurnState{
				Provider: provider,
				Status:   browserLongTurnFailed,
				Reply:    lastReply,
				Detail:   "The browser model was still reported as working long after the turn should have ended. Open the model in FlipAi, reconnect if needed, and try again.",
			})
			return
		}
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
		replyChanged := false
		if seenResponse && reply != "" {
			if reply == lastReply {
				stable++
			} else {
				lastReply = reply
				stable = 0
				replyChanged = true
			}
		} else {
			stable = 0
		}

		// Provider image placeholders are explicitly non-final even if their Stop
		// control temporarily disappears while the image tool swaps UI states.
		pendingImage := browserChatReplySuggestsPendingImage(reply)
		if seenResponse && reply != "" && !pendingImage {
			if !snap.Working && stable >= 2 {
				_ = saveBrowserLongTurnState(dataDir, browserLongTurnState{Provider: provider, Status: browserLongTurnDone, Reply: reply})
				return
			}
			// A stable answer is a finished answer. Working is inferred from the
			// page's controls, and a provider that leaves any stop-like control
			// on screen -- voice mode, a stale streaming affordance, a control
			// the site renames -- reads as working forever. That is what left a
			// completed answer sitting in the browser while FlipAi texted
			// "still working" until the turn was abandoned. Text that has not
			// changed for this long is not being streamed any more.
			if stable >= browserLongTurnSettledSamples {
				_ = saveBrowserLongTurnState(dataDir, browserLongTurnState{Provider: provider, Status: browserLongTurnDone, Reply: reply})
				return
			}
		}

		if snap.Working || replyChanged {
			idleSince = time.Time{}
			continue
		}
		if idleSince.IsZero() {
			idleSince = time.Now()
			continue
		}
		if time.Since(idleSince) >= 15*time.Second {
			_ = saveBrowserLongTurnState(dataDir, browserLongTurnState{
				Provider: provider,
				Status:   browserLongTurnFailed,
				Reply:    reply,
				Detail:   "The browser model stopped without producing a final response. Open the model in FlipAi, reconnect if needed, and try again.",
			})
			return
		}
	}
}
