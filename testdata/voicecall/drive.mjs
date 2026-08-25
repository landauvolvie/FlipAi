// Drives the real FlipAi Google Voice injection script in headless Chromium
// against a stand-in Google Voice page, with the real Go call-bridge behind the
// window.flipVoice* bindings. Everything here is plumbing; the logic under test
// is served by the Go side at /flipai-init.js and is byte-for-byte what the
// Windows app injects.
//
// Chromium supplies two fake audio inputs and two fake audio outputs, which
// stand in for the two virtual audio cables the feature needs on a real PC. The
// microphone capture and the media-element sink are genuinely applied by the
// browser, so the routing assertions are real rather than mocked.
import { chromium } from '/opt/node22/lib/node_modules/playwright/index.mjs';

const BASE = process.env.FLIPAI_TEST_BASE;   // https://voice.google.com/ (browser-visible)
const SHIM = process.env.FLIPAI_TEST_SHIM;   // http://127.0.0.1:PORT/ (this script only)
const MAP = process.env.FLIPAI_TEST_MAP;     // Chromium host-resolver rule
const results = { scenarios: [] };

// The script under test is served by the Go side so the browser runs exactly
// the string the Windows app injects, with no copy to drift out of date.
const initScript = await (await fetch(SHIM + 'flipai-init.js')).text();

const browser = await chromium.launch({
  args: [
    '--no-proxy-server',
    `--host-resolver-rules=${MAP}`,
    '--ignore-certificate-errors',
    '--use-fake-device-for-media-stream',
    '--use-fake-ui-for-media-stream',
    '--autoplay-policy=no-user-gesture-required',
  ],
});

async function scenario(name, config, body) {
  const ctx = await browser.newContext({ ignoreHTTPSErrors: true, permissions: ['microphone'] });
  const page = await ctx.newPage();
  const calls = [];
  const errors = [];
  page.on('pageerror', (e) => errors.push(String(e)));

  const post = async (method, params) => {
    const r = await fetch(SHIM + method, {
      method: 'POST',
      headers: { 'content-type': 'application/json', 'x-flipai-scenario': name },
      body: JSON.stringify(params),
    });
    if (!r.ok) throw new Error(`${method}: ${r.status} ${await r.text()}`);
    return r.json();
  };
  const bind = (jsName, method, shape) =>
    page.exposeFunction(jsName, async (...args) => {
      calls.push({ method: jsName, args });
      return (await post(method, shape(...args))).result;
    });

  await bind('flipVoiceAudioSettings', 'audio-settings', () => ({}));
  await bind('flipVoiceIncoming', 'incoming', (number, label) => ({ number, label }));
  await bind('flipVoiceAnswered', 'answered', (number, label) => ({ number, label }));
  await bind('flipVoiceEnded', 'ended', () => ({}));
  await bind('flipVoiceDevices', 'devices', (raw) => ({ raw }));
  await bind('flipVoicePage', 'page', (href, signedIn) => ({ href, signedIn }));

  // Recorded before FlipAi installs its own wrapper, so it observes the
  // constraints FlipAi produced rather than the ones the page asked for.
  // Init scripts also run on the initial empty document, where mediaDevices is
  // absent, so this has to tolerate not being able to install itself.
  await page.addInitScript(() => {
    try {
      const md = navigator.mediaDevices;
      if (!md || !md.getUserMedia) return;
      const inner = md.getUserMedia.bind(md);
      window.__flipRecordedCalls = [];
      md.getUserMedia = function (c) {
        // FlipAi opens the microphone itself at startup to unlock endpoint
        // names, so more than one capture is expected; every one is kept.
        window.__flipRecordedCalls.push(JSON.parse(JSON.stringify(c || null)));
        return inner(c);
      };
    } catch (_) {}
  });
  await page.addInitScript({ content: initScript });

  await post('configure', config);
  await page.goto(BASE, { waitUntil: 'load' });
  await page.waitForFunction(() => typeof window.__flipVoiceTick === 'function', null, { timeout: 10000 })
    .catch(() => { throw new Error(`FlipAi script never installed in ${name}: ${errors.join(' | ') || 'no page error reported'}`); });
  // The injected script also ticks on its own timer and single-flights itself,
  // so a driven tick can legitimately be a no-op. Each step is therefore given
  // a moment to settle rather than assuming one call does one unit of work.
  const tick = async (times = 1) => {
    for (let i = 0; i < times; i++) {
      await page.evaluate(() => window.__flipVoiceTick());
      await page.waitForTimeout(150);
    }
  };

  const devices = await page.evaluate(async () =>
    (await navigator.mediaDevices.enumerateDevices())
      .filter((d) => d.kind === 'audioinput' || d.kind === 'audiooutput')
      .map((d) => ({ kind: d.kind, deviceId: d.deviceId, label: d.label })));

  const out = await body({ page, tick, calls, devices });
  results.scenarios.push({
    name,
    calls,
    errors,
    devices,
    observed: await page.evaluate(() => window.gv.observed()),
    ...out,
  });
  await ctx.close();
}

const CABLE_IN = 'Fake Audio Input 2';    // stands in for the cable feeding Google Voice's mic
const CABLE_OUT = 'Fake Audio Output 2';  // stands in for the cable carrying the caller onward

// Who may call is decided by the agents, exactly as who may text is; the voice
// half only says how the call is bridged.
const baseConfig = (agents = {}) => ({
  voice: {
    enabled: true,
    autoAnswer: true,
    defaultAgent: 'C',
    googleVoiceInput: CABLE_IN,
    googleVoiceOutput: CABLE_OUT,
    codex: { enabled: true, appTitle: 'ChatGPT' },
    claude: { enabled: false, appTitle: 'Claude' },
  },
  agents: {
    defaultAgent: 'C',
    codex: { phones: [{ number: '8455551000', access: 'all' }] },
    claude: {},
    ...agents,
  },
});

// 1. The whole point of the feature: an approved number calls, FlipAi answers,
//    bridges the audio onto the cables, and tears down on hang-up.
await scenario('authorized-number', baseConfig(), async ({ page, tick }) => {
  await tick();
  await page.evaluate(() => window.gv.ring('(845) 555-1000\nMobile'));
  await tick();
  await page.waitForFunction(() => !!document.getElementById('remote'), null, { timeout: 5000 });
  await tick(2);
  const midCall = await page.evaluate(() => window.gv.observed());
  await page.evaluate(() => window.gv.hangup());
  await tick();
  return { midCall };
});

// 2. The caller is in Google Contacts, so Google Voice shows a name and there is
//    no number to match. Nothing may be answered, and FlipAi has to say why.
await scenario('contact-name-not-allowed', baseConfig(), async ({ page, tick }) => {
  await tick();
  await page.evaluate(() => window.gv.ring('Jane Appleseed\nMobile'));
  await tick(2);
  await page.waitForTimeout(1500);
  return { answered: await page.evaluate(() => !!document.getElementById('remote')) };
});

// 3. The same call, once the user has approved that displayed name.
await scenario('contact-name-allowed', baseConfig({
  codex: { phones: [{ number: '8455551000', access: 'all' }], callerNames: 'Jane Appleseed' },
}), async ({ page, tick }) => {
  await tick();
  await page.evaluate(() => window.gv.ring('Jane Appleseed\nMobile'));
  await tick();
  await page.waitForFunction(() => !!document.getElementById('remote'), null, { timeout: 5000 });
  await tick();
  return { answered: true };
});

// 3b. Google sometimes names the caller only on the Answer control itself.
await scenario('caller-named-on-answer-button', baseConfig(), async ({ page, tick }) => {
  await tick();
  await page.evaluate(() =>
    window.gv.ring('Incoming call', null, 'Answer call from (845) 555-1000'));
  await tick();
  await page.waitForFunction(() => !!document.getElementById('remote'), null, { timeout: 5000 });
  await tick();
  return { answered: true };
});

// 4. A number that is not on the list must not be answered at all.
await scenario('unauthorized-number', baseConfig(), async ({ page, tick }) => {
  await tick();
  await page.evaluate(() => window.gv.ring('(845) 555-9999\nMobile'));
  await tick(2);
  await page.waitForTimeout(1500);
  return { answered: await page.evaluate(() => !!document.getElementById('remote')) };
});

// 5. A number sitting elsewhere in the Google Voice UI must never be mistaken
//    for the caller. Here the ringing call has no caller ID at all while an
//    approved number is on screen in the thread list.
await scenario('decoy-number-on-page', baseConfig({
  codex: { phones: [{ number: '2125550000', access: 'all' }] },
}), async ({ page, tick }) => {
  await tick();
  await page.evaluate(() => window.gv.ring('Unknown caller'));
  await tick(2);
  await page.waitForTimeout(1500);
  return { answered: await page.evaluate(() => !!document.getElementById('remote')) };
});

// 6. Overlapping ticks must not answer the same call twice.
await scenario('no-double-answer', baseConfig(), async ({ page, tick }) => {
  await tick();
  await page.evaluate(() => window.gv.ring('(845) 555-1000'));
  await page.evaluate(async () => {
    await Promise.all([
      window.__flipVoiceTick(), window.__flipVoiceTick(),
      window.__flipVoiceTick(), window.__flipVoiceTick(),
    ]);
  });
  await page.waitForFunction(() => !!document.getElementById('remote'), null, { timeout: 5000 });
  await tick(3);
  return { answered: true };
});

// 7. The window must not become a general-purpose browser, and the page must not
//    keep the capabilities FlipAi took away.
await scenario('capabilities-removed', baseConfig(), async ({ page, tick }) => {
  await tick();
  return {
    capabilities: await page.evaluate(async () => {
      const out = {};
      try { await navigator.mediaDevices.getUserMedia({ video: true }); out.camera = 'allowed'; }
      catch (e) { out.camera = e.name; }
      out.geolocation = await new Promise((res) =>
        navigator.geolocation.getCurrentPosition(() => res('allowed'), () => res('denied')));
      try { await navigator.clipboard.readText(); out.clipboard = 'allowed'; }
      catch (e) { out.clipboard = e.name; }
      try { await navigator.mediaDevices.getDisplayMedia({ video: true }); out.screen = 'allowed'; }
      catch (e) { out.screen = e.name; }
      return out;
    }),
  };
});

await browser.close();
process.stdout.write(JSON.stringify(results));
