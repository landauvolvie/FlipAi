// Drives each provider's real turn script against synthetic chat pages in a
// real browser.
//
// The page deliberately uses none of the attributes the drivers look for by
// name -- no data-message-author-role, no data-testid, no "assistant" class,
// no <main>. That is the situation the Muse driver was actually in: Muse
// answered, the answer was on screen, and every selector missed it, so the
// turn reported that the model had stopped without producing anything.
import fs from 'node:fs';
import path from 'node:path';
import { pathToFileURL } from 'node:url';

const pwPath = process.env.FLIPAI_PLAYWRIGHT_MODULE;
if (!pwPath) throw new Error('FLIPAI_PLAYWRIGHT_MODULE is required');
const { chromium } = await import(pathToFileURL(pwPath).href);

function turnScript(file, constant, prompt) {
  const source = fs.readFileSync(path.resolve(file), 'utf8');
  const marker = `const ${constant} = \``;
  const start = source.indexOf(marker);
  if (start < 0) throw new Error(`${constant} was not found`);
  const bodyStart = start + marker.length;
  const bodyEnd = source.indexOf('`', bodyStart);
  if (bodyEnd < 0) throw new Error(`${constant} closing marker was not found`);
  return source.slice(bodyStart, bodyEnd).replace('%s', JSON.stringify(prompt));
}

// An unremarkable chat app: anonymous divs, no recognizable hooks anywhere.
// `withSendButton` false removes the send control entirely, so the only way to
// submit is the Enter key -- the case that stranded Copilot with the prompt
// typed and never sent.
//
// `withActivityPanel` adds the running tool/step log these assistants show
// beside the answer. Reading that panel instead of the answer is what sent
// "Find today's last email Opening today's latest email 3:19 pm Review
// proactive preferences ..." to the phone in place of the real answer.
//
// `withInterimStatus` shows a short status line -- "Searching sources" -- in the
// conversation before the answer arrives. Taking that as the answer is what
// texted "Searching sources" instead of what the model said.
//
// `withActionBar` appends the reply's action row ("Edit in a page", "Copy"),
// which is page furniture and not part of the message.
//
// `withCitationCards` appends the source cards a web-searching assistant puts
// under an answer ("CBS News", "www.thephoto-news.com", "Show all"). They are
// references the page renders, not sentences the model wrote.
//
// The same scenario also writes a sentence whose phrases are links back to
// those sources -- "AI regulation", "climate rules", "immigration lawsuits".
// Those phrases are the answer. Removing anything the page called a citation
// or a source deleted them along with the cards, and the text message then
// arrived with holes in its sentences.
//
// `withNamedWrapper` gives the scroll container a class the drivers look for by
// name -- "responses" matches `[class*="response" i]` -- and gives no message
// its own hook. Nothing dropped that wrapper, so the entire conversation came
// back as one reply: weeks of old messages, plus the running tool log, arriving
// as a single text message.
//
// `withPreamble` is the shape of a tool-using turn: the model says what it is
// about to do, runs tools for several seconds, offers some connectors, and only
// then writes the answer and attaches a file. FlipAi texted the opening line and
// nothing else -- that line sat unchanged for a second while the work was still
// going, and it looked finished.
//
// `withPageFurniture` puts a saved task in a sidebar and a suggestion chip under
// the composer, and has the sidebar change while the turn runs. Neither is
// anything the model said. One of them -- "Let me know when a new Dell with
// Intel, 32 GB RAM, touchscreen, and built-in 5G appears" -- was texted as the
// answer to "hi my friend".
//
// `withBusyLabel` shows the page's own busy line, split across two elements the
// way a real one is, so reading them together runs the name into the next word:
// "Museis working". That was texted in place of the reply.
//
// `withLongHistory` builds a conversation of a realistic size. A driver that
// scans the whole page on every poll cannot finish inside its own deadline
// there, and one that treats a scroll container as a block sends the entire
// history -- every message joined together -- as the answer.
function pageHTML({ withSendButton, withActivityPanel, withInterimStatus, withActionBar, withLongHistory, withCitationCards, withNamedWrapper, withPreamble, withPageFurniture, withBusyLabel }) {
  const history = withLongHistory
    ? Array.from({ length: 700 }, (_, i) =>
        `<div class="x1"><p>Earlier turn ${i}</p><p>A paragraph of an older answer that is already on screen and should never be mistaken for this turn's reply.</p></div>`).join('')
    : '<div class="x1">an earlier answer that was already on screen</div>';
  return `<!doctype html><html><body>
    ${withPageFurniture ? `<nav id="side"><div class="task">\u{1F4BB} Let me know when a new Dell with Intel, 32 GB RAM, touchscreen, and built-in 5G appears</div></nav>` : ''}
    <main><div id="log" class="${withNamedWrapper ? 'responses assistant-message markdown' : 'plain'}">${history}</div></main>
    ${withActivityPanel ? `<aside id="steps">
      <div class="s">Search tool initialization Loaded device tools and initialized 4 functions 3:14 pm</div>
      <div class="s">Review proactive preferences Updated preferences and memory files 3:18 pm</div>
    </aside>` : ''}
    <div id="composer-wrap">
      <textarea id="box" placeholder="Ask anything"></textarea>
      ${withSendButton ? '<button id="go">Go</button>' : ''}
    </div>
    ${withPageFurniture ? '<div id="chips"><div class="suggestion">Summarize a document for me</div></div>' : ''}
    <script>
      const submit = () => {
        const box = document.querySelector('#box');
        const value = box.value;
        if (!value) return;
        window.__flipaiSubmitted = value;
        if (${JSON.stringify(!!withPageFurniture)}) {
          // The sidebar keeps changing while the turn runs, the way a live one does.
          let n = 0;
          const side = setInterval(() => {
            const task = document.querySelector('#side .task');
            if (task) task.textContent = '\u{1F4BB} Let me know when a new Dell with Intel, 32 GB RAM, touchscreen, and built-in 5G appears (' + (++n) + ')';
            // It updates a few times and then sits still, which is what made it look
            // like a finished answer long before the real one arrived.
            if (n > 3) clearInterval(side);
          }, 300);
        }
        if (${JSON.stringify(!!withBusyLabel)}) {
          const busyRow = document.createElement('div');
          busyRow.className = 'x3';
          busyRow.innerHTML = '<span>Muse</span><span>is working</span>';
          document.querySelector('#log').appendChild(busyRow);
          setTimeout(() => busyRow.remove(), 5900);
        }
        const log = document.querySelector('#log');
        const mine = document.createElement('div');
        mine.className = 'x2';
        mine.textContent = value;
        log.appendChild(mine);
        const busy = document.createElement('button');
        busy.textContent = 'Stop';
        busy.setAttribute('aria-label', 'Stop');
        // No stop control in these: the real page showed FlipAi none, which is
        // exactly why a busy line and a sidebar item looked like finished answers.
        if (!${JSON.stringify(!!(withInterimStatus || withBusyLabel || withPageFurniture))}) document.body.appendChild(busy);
        if (${JSON.stringify(!!withInterimStatus)}) {
          const status = document.createElement('div');
          status.className = 'x9';
          status.textContent = 'Searching sources';
          log.appendChild(status);
          setTimeout(() => status.remove(), 2600);
        }
        setTimeout(() => {
          const reply = document.createElement('div');
          reply.className = 'x3';
          log.appendChild(reply);
          reply.textContent = 'FLIPAI';
          if (${JSON.stringify(!!withPreamble)}) {
            // Says something, works for a while, then answers.
            // Claude reads its own turn container by name; this scenario is about
            // what the driver does with a turn, not about finding one.
            reply.setAttribute('data-testid', 'assistant-message');
            reply.textContent = 'This will take a few minutes - pulling calendar and email first.';
            let done = 0;
            const tick = setInterval(() => {
              done++;
              let log = reply.querySelector('.tool-use');
              if (!log) { log = document.createElement('div'); log.className = 'tool-use'; reply.appendChild(log); }
              log.textContent = 'Ran ' + done + ' commands, used 2 integrations';
              if (done >= 5) {
                clearInterval(tick);
                const offer = document.createElement('div');
                offer.innerHTML = '<span>Connectors that could help</span>'
                  + '<div><span>Microsoft 365</span> <button>Connect</button></div>'
                  + '<div><span>Slack</span> <button>Connect</button></div>';
                reply.appendChild(offer);
                const answer = document.createElement('p');
                answer.textContent = 'FLIPAI answered: ' + value;
                reply.appendChild(answer);
                const tile = document.createElement('div');
                tile.innerHTML = '<span>Morning brief</span><span>Code - HTML</span><button>Download</button>';
                reply.appendChild(tile);
                busy.remove();
              }
            }, 900);
            return;
          }
          setTimeout(() => { reply.textContent = 'FLIPAI answered: ' + value; }, 200);
          setTimeout(() => busy.remove(), 700);
          if (${JSON.stringify(!!withCitationCards)}) {
            setTimeout(() => {
              const linked = document.createElement('p');
              linked.innerHTML = 'The cases involve <a class="citation-link" href="#">AI regulation</a>, '
                + '<span class="source-chip">climate rules</span>, or '
                + '<a class="reference-link" href="#">immigration lawsuits</a>.'
                + '<sup class="citation-marker">1</sup>';
              reply.appendChild(linked);
              const cards = document.createElement('div');
              cards.className = 'citation-row';
              cards.innerHTML = '<div class="card">CBS News U.S. News: Latest news, breaking news</div>'
                + '<div class="card">www.thephoto-news.com The Photo News | The local newspaper</div>'
                + '<div class="card">Show all</div>';
              reply.appendChild(cards);
            }, 900);
          }
          if (${JSON.stringify(!!withActionBar)}) {
            setTimeout(() => {
              const bar = document.createElement('div');
              bar.className = 'actions';
              bar.textContent = 'Edit in a page';
              reply.appendChild(bar);
            }, 900);
          }
          const steps = document.querySelector('#steps');
          if (steps) {
            setTimeout(() => {
              const step = document.createElement('div');
              step.className = 's';
              step.textContent = "Find today's last email Opening today's latest email 3:19 pm";
              steps.appendChild(step);
            }, 500);
          }
        }, ${JSON.stringify(withInterimStatus ? 3000 : (withPageFurniture || withBusyLabel) ? 6000 : 400)});
      };
      const go = document.querySelector('#go');
      if (go) go.addEventListener('click', submit);
      document.querySelector('#box').addEventListener('keydown', e => {
        if (e.key === 'Enter') { e.preventDefault(); submit(); }
      });
    </script>
  </body></html>`;
}

const drivers = [
  ['Muse', 'muse_chat_webview_windows.go', 'museChatTurnJS'],
  ['Microsoft Copilot', 'copilot_chat_webview_windows.go', 'copilotChatTurnJS'],
  ['ChatGPT', 'chatgpt_webview_windows.go', 'chatGPTTurnJS'],
];

// Gemini stopped sending altogether -- "FlipAi filled the Gemini prompt box but
// the Send button never became ready" -- because it was the last driver with no
// way to submit other than a button it could name. These three are covered for
// exactly that: a composer and no send control at all.
// This scenario checks one thing only: the prompt leaves the composer. These
// drivers read their provider's own reply layout, which this anonymous page
// deliberately does not have, so what they make of the answer is not what is
// under test here.
const enterOnlyDrivers = [
  ['Gemini', 'gemini_chat_webview_windows.go', 'geminiChatTurnJS'],
  ['Claude', 'claude_chat_webview_windows.go', 'claudeChatTurnJS'],
  ['Grok', 'grok_chat_webview_windows.go', 'grokChatTurnJS'],
];

const prompt = 'browser harness prompt';
const browser = await chromium.launch({ headless: true });
const failures = [];
const report = [];

// Every scenario runs at once. A driver that cannot find the answer sits out
// its own 90-second deadline, and serially that is six minutes of CI for a
// result each scenario reaches independently.
const scenarios = [];
for (const [name, file, constant] of drivers) {
  for (const withSendButton of [true, false]) {
    scenarios.push({ name, file, constant, withSendButton, withActivityPanel: false });
  }
  scenarios.push({ name, file, constant, withSendButton: true, withActivityPanel: true });
  scenarios.push({ name, file, constant, withSendButton: true, withInterimStatus: true });
  scenarios.push({ name, file, constant, withSendButton: true, withActionBar: true });
  scenarios.push({ name, file, constant, withSendButton: true, withLongHistory: true });
  scenarios.push({ name, file, constant, withSendButton: true, withCitationCards: true });
  scenarios.push({ name, file, constant, withSendButton: true, withNamedWrapper: true, withActivityPanel: true });
  scenarios.push({ name, file, constant, withSendButton: true, withPageFurniture: true });
  scenarios.push({ name, file, constant, withSendButton: true, withBusyLabel: true });
}
for (const [name, file, constant] of enterOnlyDrivers) {
  scenarios.push({ name, file, constant, withSendButton: false, sendOnly: true });
}
// The tool-using turn, across every driver that reads a reply out of the page.
for (const [name, file, constant] of drivers.concat([['Claude', 'claude_chat_webview_windows.go', 'claudeChatTurnJS']])) {
  scenarios.push({ name, file, constant, withSendButton: true, withPreamble: true });
}

await Promise.all(scenarios.map(async ({ name, file, constant, withSendButton, withActivityPanel, withInterimStatus, withActionBar, withLongHistory, withCitationCards, withNamedWrapper, withPreamble, withPageFurniture, withBusyLabel, sendOnly }) => {
  const label = `${name} (${withPageFurniture ? 'page furniture' : withBusyLabel ? 'busy label' : withPreamble ? 'tool-using turn' : withNamedWrapper ? 'named wrapper' : withActivityPanel ? 'activity panel' : withInterimStatus ? 'interim status' : withActionBar ? 'action bar' : withLongHistory ? 'long history' : withCitationCards ? 'citation cards' : withSendButton ? 'send button' : 'Enter only'})`;
  const page = await browser.newPage();
  const pageErrors = [];
  page.on('pageerror', e => pageErrors.push(String(e)));
  await page.setContent(pageHTML({ withSendButton, withActivityPanel, withInterimStatus, withActionBar, withLongHistory, withCitationCards, withNamedWrapper, withPreamble, withPageFurniture, withBusyLabel }));
  const startedAt = Date.now();
  if (sendOnly) {
    // The driver keeps polling for a reply it will never recognize here, so do
    // not wait it out: watch the page for the submit instead.
    const running = page.evaluate(turnScript(file, constant, prompt)).catch(e => ({ threw: String(e) }));
    let sent = null;
    while (Date.now() - startedAt < 25000) {
      sent = await page.evaluate(() => window.__flipaiSubmitted || null).catch(() => null);
      if (sent) break;
      await new Promise(r => setTimeout(r, 200));
    }
    if (sent !== prompt) failures.push(`${label}: the prompt was never sent (composer filled, nothing submitted)`);
    else if (pageErrors.length) failures.push(`${label}: page errors: ${pageErrors.join(' | ')}`);
    else report.push(`${label}: ok`);
    void running;
    await page.close().catch(() => {});
    return;
  }
  let result;
  try {
    result = await page.evaluate(turnScript(file, constant, prompt));
  } catch (e) {
    failures.push(`${label}: driver threw: ${e}`);
    await page.close();
    return;
  }
  if (pageErrors.length) failures.push(`${label}: page errors: ${pageErrors.join(' | ')}`);
  if (!result || !result.ok) {
    failures.push(`${label}: turn failed: ${result && result.detail}`);
  } else if (!String(result.reply).includes(prompt)) {
    failures.push(`${label}: reply did not carry the answer: ${JSON.stringify(result.reply)}`);
  } else if (String(result.reply).trim() === prompt) {
    failures.push(`${label}: the prompt was echoed back as the answer: ${JSON.stringify(result.reply)}`);
  } else if (/\d:\d\d ?[ap]m/i.test(String(result.reply))) {
    failures.push(`${label}: the tool/step panel was sent instead of the answer: ${JSON.stringify(result.reply)}`);
  } else if (/searching sources/i.test(String(result.reply))) {
    failures.push(`${label}: an interim status was sent instead of the answer: ${JSON.stringify(result.reply)}`);
  } else if (/edit in a page/i.test(String(result.reply))) {
    failures.push(`${label}: the reply's action bar was included: ${JSON.stringify(result.reply)}`);
  } else if (/Earlier turn \d|an earlier answer that was already on screen/.test(String(result.reply))) {
    failures.push(`${label}: the conversation history was sent as the answer (${String(result.reply).length} chars)`);
  } else if (/CBS News|thephoto-news|Show all/i.test(String(result.reply))) {
    failures.push(`${label}: source cards were included in the answer: ${JSON.stringify(result.reply)}`);
  } else if (/new Dell with Intel|Summarize a document for me/.test(String(result.reply))) {
    failures.push(`${label}: page furniture was sent as the answer: ${JSON.stringify(result.reply)}`);
  } else if (/\bMuse\s?is working\b/i.test(String(result.reply))) {
    failures.push(`${label}: the page's busy line was sent as the answer: ${JSON.stringify(result.reply)}`);
  } else if (withPreamble && /pulling calendar and email/.test(String(result.reply)) && !/FLIPAI answered/.test(String(result.reply))) {
    failures.push(`${label}: only the opening line was sent, while the answer was still being written: ${JSON.stringify(result.reply)}`);
  } else if (withPreamble && /Connectors that could help|Morning brief|Code - HTML/.test(String(result.reply))) {
    failures.push(`${label}: a connector offer or file tile was sent as part of the answer: ${JSON.stringify(result.reply)}`);
  } else if (withPreamble && /Ran \d+ commands/.test(String(result.reply))) {
    failures.push(`${label}: the running tool log was sent as part of the answer: ${JSON.stringify(result.reply)}`);
  } else if (withCitationCards && !/AI regulation/.test(String(result.reply))) {
    failures.push(`${label}: a linked phrase was stripped out of the answer: ${JSON.stringify(result.reply)}`);
  } else if (withCitationCards && !/climate rules/.test(String(result.reply))) {
    failures.push(`${label}: a linked phrase was stripped out of the answer: ${JSON.stringify(result.reply)}`);
  } else if (withCitationCards && !/immigration lawsuits/.test(String(result.reply))) {
    failures.push(`${label}: a linked phrase was stripped out of the answer: ${JSON.stringify(result.reply)}`);
  } else if (Date.now() - startedAt > 30000) {
    failures.push(`${label}: the turn took ${Math.round((Date.now() - startedAt) / 1000)}s, which will not finish inside its own deadline`);
  } else {
    report.push(`${label}: ok`);
  }
  await page.close();
}));

report.sort();
failures.sort();
await browser.close();
console.log(JSON.stringify({ ok: failures.length === 0, report, failures }, null, 2));
if (failures.length) process.exit(1);
