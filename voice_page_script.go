package main

// googleVoiceInitScript is injected into the dedicated Google Voice window
// before every document. It is the only part of the call bridge that runs
// inside Google's page, and it does four jobs:
//
//   - keep the window on Google Voice and strip the capabilities FlipAi does
//     not want the page to have (see the permission note in
//     voice_call_windows.go for why that removal happens here);
//   - notice a ringing call, work out who is calling, and ask FlipAi whether
//     to answer it;
//   - pin Google Voice's microphone and speaker to the virtual endpoints the
//     user chose, so the call is wired to the AI app rather than the PC's own
//     headset;
//   - tell FlipAi when the call starts and ends, so desktop voice mode is
//     switched on and off around it.
//
// It deliberately lives in a platform-independent file: the harness in
// voice_page_test.go runs this exact string in headless Chromium against a
// stand-in Google Voice page, which is the only way any of this is testable
// without a phone line.
const googleVoiceInitScript = `
(() => {
  if (window.__flipVoiceInstalled) return;
  window.__flipVoiceInstalled = true;

  const TICK_MS = 700;
  const SETTINGS_TTL_MS = 3000;
  const DEVICE_REPORT_MS = 5000;

  /* ---------- keep this window a Google Voice window ---------- */

  const allowedTopLevel = (href) => {
    try {
      const h = new URL(href, location.href).hostname.toLowerCase();
      return h === 'voice.google.com' || h === 'accounts.google.com';
    } catch (_) { return false; }
  };
  document.addEventListener('click', (e) => {
    const a = e.target && e.target.closest ? e.target.closest('a[href]') : null;
    if (a && !allowedTopLevel(a.href)) e.preventDefault();
  }, true);

  // WebView2 grants this window's permissions globally, because per-permission
  // grants are broken in the browser binding FlipAi uses. Everything except the
  // microphone is therefore taken away here, before Google Voice can ask.
  const denied = (msg) => Promise.reject(new DOMException(msg, 'NotAllowedError'));
  try {
    const geo = {
      getCurrentPosition: (_ok, err) => { if (err) err({code: 1, message: 'Location is disabled in FlipAi'}); },
      watchPosition: (_ok, err) => { if (err) err({code: 1, message: 'Location is disabled in FlipAi'}); return 0; },
      clearWatch: () => {}
    };
    Object.defineProperty(navigator, 'geolocation', {value: geo, configurable: true});
  } catch (_) {}
  try {
    if (navigator.clipboard) {
      navigator.clipboard.read = () => denied('Clipboard access is disabled in FlipAi');
      navigator.clipboard.readText = () => denied('Clipboard access is disabled in FlipAi');
    }
  } catch (_) {}
  try {
    if (navigator.mediaDevices && navigator.mediaDevices.getDisplayMedia) {
      navigator.mediaDevices.getDisplayMedia = () => denied('Screen sharing is disabled in FlipAi');
    }
  } catch (_) {}

  /* ---------- talking to FlipAi ---------- */

  let settings = {input: '', output: '', ring: ''};
  let settingsAt = 0;
  async function audioSettings() {
    const now = Date.now();
    if (now - settingsAt < SETTINGS_TTL_MS) return settings;
    try {
      const next = await window.flipVoiceAudioSettings();
      if (next) settings = next;
    } catch (_) {}
    settingsAt = now;
    return settings;
  }

  /* ---------- audio endpoints ---------- */

  // Every lookup below is cached. The previous version resolved the endpoint
  // from scratch for every audio element on every DOM mutation, and Google
  // Voice mutates continuously, so it issued thousands of host round-trips a
  // second and left the page too busy to answer a call.
  const idCache = new Map();
  let deviceList = [];
  let sinkId = '';
  // micId is resolved ahead of time only to warm idCache, so the getUserMedia
  // interception below can substitute the endpoint without a lookup mid-call.
  let micId = '';

  async function currentDevices() {
    try { deviceList = await navigator.mediaDevices.enumerateDevices(); } catch (_) { deviceList = []; }
    return deviceList;
  }
  function matchDevice(list, kind, wanted) {
    const want = String(wanted).toLowerCase();
    const exact = list.find(d => d.kind === kind && d.label === wanted);
    if (exact) return exact.deviceId;
    const loose = list.find(d => d.kind === kind && d.label && d.label.toLowerCase().includes(want));
    return loose ? loose.deviceId : '';
  }
  async function deviceIdFor(kind, wanted) {
    if (!wanted) return '';
    const key = kind + ' ' + wanted;
    if (idCache.has(key)) return idCache.get(key);
    const id = matchDevice(await currentDevices(), kind, wanted);
    if (id) idCache.set(key, id);
    return id;
  }
  function forgetDevices() { idCache.clear(); settingsAt = 0; }

  async function refreshEndpoints() {
    const s = await audioSettings();
    sinkId = await deviceIdFor('audiooutput', s.output);
    micId = await deviceIdFor('audioinput', s.input);
  }

  let lastDeviceJSON = '';
  let lastDeviceReport = 0;
  async function reportDevices(force) {
    const now = Date.now();
    if (!force && now - lastDeviceReport < DEVICE_REPORT_MS) return;
    lastDeviceReport = now;
    const out = (await currentDevices())
      .filter(d => d.kind === 'audioinput' || d.kind === 'audiooutput')
      .map(d => ({kind: d.kind, deviceId: d.deviceId || '', label: d.label || ''}));
    const raw = JSON.stringify(out);
    if (raw === lastDeviceJSON) return;
    lastDeviceJSON = raw;
    try { await window.flipVoiceDevices(raw); } catch (_) {}
  }

  // routed remembers the endpoint already applied to an element, which is what
  // makes it cheap enough to re-check on every DOM change.
  const routed = new WeakMap();
  function applySink(el) {
    if (!el || typeof el.setSinkId !== 'function' || !sinkId) return false;
    if (routed.get(el) === sinkId) return true;
    try {
      routed.set(el, sinkId);
      el.setSinkId(sinkId).catch(() => routed.delete(el));
      return true;
    } catch (_) { routed.delete(el); return false; }
  }
  function routeAll() { document.querySelectorAll('audio,video').forEach(applySink); }

  // Google Voice plays the caller through a media element. The sink is applied
  // from the cached endpoint and play() is still called in the same turn:
  // deferring play() past its user-gesture turn can trip autoplay policy and
  // lose the call audio altogether.
  const nativePlay = HTMLMediaElement.prototype.play;
  HTMLMediaElement.prototype.play = function(...args) {
    applySink(this);
    return nativePlay.apply(this, args);
  };

  // Force Google Voice's microphone onto the virtual endpoint chosen in FlipAi.
  // A video request is refused outright: this window exists for phone calls.
  if (navigator.mediaDevices && navigator.mediaDevices.getUserMedia) {
    const gum = navigator.mediaDevices.getUserMedia.bind(navigator.mediaDevices);
    navigator.mediaDevices.getUserMedia = async function(constraints) {
      if (constraints && constraints.video) return denied('The camera is disabled in FlipAi');
      let next = constraints;
      try {
        const s = await audioSettings();
        if (constraints && constraints.audio && s.input) {
          const id = await deviceIdFor('audioinput', s.input);
          if (id) {
            const a = constraints.audio === true ? {} : Object.assign({}, constraints.audio);
            a.deviceId = {exact: id};
            next = Object.assign({}, constraints, {audio: a});
          }
        }
      } catch (_) {}
      const stream = await gum(next);
      // Endpoint names are only readable once a stream exists, so this is the
      // first moment the device list is worth anything to the settings page.
      forgetDevices();
      reportDevices(true);
      return stream;
    };
    try {
      navigator.mediaDevices.addEventListener('devicechange', () => { forgetDevices(); reportDevices(true); });
    } catch (_) {}
  }

  // WebView2 runs this script at document-created time, when <html> may not
  // exist yet. Observing a null root throws, and the throw aborts the rest of
  // this script -- which is the entire call bridge. The observer therefore
  // waits for a root instead of assuming one.
  let routeQueued = false;
  const observeDocument = () => {
    const root = document.documentElement || document.body;
    if (!root) return false;
    new MutationObserver(() => {
      if (routeQueued) return;
      routeQueued = true;
      setTimeout(() => { routeQueued = false; routeAll(); }, 250);
    }).observe(root, {childList: true, subtree: true});
    return true;
  };
  if (!observeDocument()) {
    document.addEventListener('DOMContentLoaded', observeDocument, {once: true});
    document.addEventListener('readystatechange', observeDocument, {once: true});
  }

  /* ---------- who is calling ---------- */

  const normPhone = (v) => {
    const d = String(v || '').replace(/\D/g, '');
    if (d.length === 11 && d[0] === '1') return d.slice(1);
    return d.length === 10 ? d : '';
  };
  const PHONE_RE = /(?:\+?1[\s.\-]?)?(?:\([0-9]{3}\)|[0-9]{3})[\s.\-]?[0-9]{3}[\s.\-]?[0-9]{4}/;
  const phoneFrom = (text) => {
    const m = String(text || '').match(PHONE_RE);
    return m ? normPhone(m[0]) : '';
  };
  // Lines the ringing UI shows that describe the call rather than the caller.
  const CHROME_LINE = /^(answer|accept|decline|reject|ignore|dismiss|hang\s*up|end\s+call|leave\s+call|incoming\s+call|mute|unmute|keypad|hold|more|options|calling|google\s+voice|block|report\s+spam|send\s+to\s+voicemail|voicemail|\d{1,2}:\d{2}(:\d{2})?)$/i;
  const FROM_RE = /(?:incoming\s+call\s+from|call\s+from|calling\s+from)\s+(.+?)\s*$/i;

  const visible = (el) => !!el && !!(el.offsetWidth || el.offsetHeight || el.getClientRects().length);
  const buttonName = (b) => ((b.getAttribute('aria-label') || '') + ' ' + (b.innerText || b.textContent || '')).trim();
  const buttons = () => Array.from(document.querySelectorAll('button,[role="button"]')).filter(visible);
  const findAnswer = () => buttons().find(b => /^(answer|accept)(\s+call)?$/i.test(buttonName(b)) || /^answer\b/i.test(buttonName(b)));
  const findHangup = () => buttons().find(b => /(hang\s*up|end\s+call|leave\s+call)/i.test(buttonName(b)));

  // scopeText reads a container the way a person reads the ringing card: the
  // words describing the caller, without the text on the buttons. It walks text
  // nodes rather than using innerText so the result does not depend on layout.
  function scopeText(scope) {
    const parts = [];
    const walker = document.createTreeWalker(scope, NodeFilter.SHOW_TEXT);
    for (let n = walker.nextNode(); n; n = walker.nextNode()) {
      const parent = n.parentElement;
      if (!parent || parent.closest('button,[role="button"]')) continue;
      const t = String(n.nodeValue || '').replace(/\u00a0/g, ' ').trim();
      if (t) parts.push(t);
    }
    return parts.join('\n');
  }

  function labelFrom(text) {
    for (const raw of String(text || '').split('\n')) {
      const line = raw.replace(/\u00a0/g, ' ').trim();
      if (!line || line.length > 120) continue;
      if (CHROME_LINE.test(line)) continue;
      if (normPhone(line)) continue;
      return line;
    }
    return '';
  }

  // The search is deliberately confined to the ringing UI. Scanning the whole
  // page for a phone number would let any number sitting in the message list
  // decide that a call is authorized.
  function callerScopes() {
    const scopes = [];
    const push = (el) => { if (el && el.nodeType === 1 && scopes.indexOf(el) < 0) scopes.push(el); };
    const answer = findAnswer();
    if (answer) {
      // Innermost first. The nearest container that names anybody is the one
      // describing this call; anything further out starts including the rest of
      // the Google Voice UI.
      let node = answer.parentElement;
      for (let i = 0; i < 5 && node && node !== document.body; i++) {
        push(node);
        node = node.parentElement;
      }
      push(answer.closest('[role="dialog"]'));
      push(answer.closest('[role="alertdialog"]'));
    }
    document.querySelectorAll('[role="dialog"],[role="alertdialog"]').forEach(push);
    return scopes;
  }

  function callerIdentity() {
    for (const scope of callerScopes()) {
      const aria = scope.getAttribute && scope.getAttribute('aria-label');
      const fromAria = aria && aria.match(FROM_RE);
      if (fromAria) {
        const said = fromAria[1].trim();
        const n = phoneFrom(said);
        if (n) return {number: n, label: ''};
        if (said) return {number: '', label: said.slice(0, 120)};
      }
      const text = scopeText(scope);
      const number = phoneFrom(text);
      const label = labelFrom(text);
      // The first scope that identifies anybody wins outright. Carrying a label
      // outward and letting a wider scope supply the number let a number from
      // the thread list attach itself to an unrelated ringing call.
      if (number || label) return {number: number, label: label};
    }
    return {number: '', label: ''};
  }

  /* ---------- the call itself ---------- */

  let caller = {number: '', label: ''};
  let inCall = false;
  let answering = false;
  let ticking = false;

  async function tick() {
    // setInterval used to overlap these runs, because a tick awaits several
    // host calls. Two overlapping ticks answered the same call twice.
    if (ticking) return;
    ticking = true;
    try {
      const href = location.href;
      if (!allowedTopLevel(href)) {
        location.replace('https://voice.google.com/');
        return;
      }
      const bodyText = (document.body && document.body.innerText || '').slice(0, 2500);
      const signedIn = location.hostname === 'voice.google.com' && !/sign\s*in/i.test(bodyText);
      try { await window.flipVoicePage(href, signedIn); } catch (_) {}

      await refreshEndpoints();
      routeAll();
      reportDevices(false);

      const answer = findAnswer();
      if (answer && !inCall && !answering) {
        const seen = callerIdentity();
        if (seen.number || seen.label) caller = seen;
        answering = true;
        try {
          const auto = await window.flipVoiceIncoming(caller.number, caller.label);
          if (auto && answer.isConnected) answer.click();
        } catch (_) {
        } finally {
          setTimeout(() => { answering = false; }, 1200);
        }
      }

      const hang = findHangup();
      if (hang && !inCall) {
        inCall = true;
        try { await window.flipVoiceAnswered(caller.number, caller.label); } catch (_) {}
      } else if (!hang && inCall) {
        inCall = false;
        caller = {number: '', label: ''};
        try { await window.flipVoiceEnded(); } catch (_) {}
      }
    } catch (_) {
    } finally {
      ticking = false;
    }
  }

  // Endpoint names are hidden from a page until it holds a microphone grant, so
  // FlipAi opens the microphone once at startup and immediately closes it. That
  // is what makes the endpoint pickers in Settings show real device names
  // before a call has ever arrived, and it settles the permission ahead of the
  // first ring instead of during it.
  async function primeDevices() {
    try {
      const stream = await navigator.mediaDevices.getUserMedia({audio: true});
      stream.getTracks().forEach(t => t.stop());
    } catch (_) {}
    forgetDevices();
    await reportDevices(true);
  }

  const loop = () => { tick().then(() => setTimeout(loop, TICK_MS)); };
  setTimeout(() => { primeDevices().then(loop); }, 250);
  // The harness drives ticks directly instead of waiting on the timer.
  window.__flipVoiceTick = tick;
})();
`
