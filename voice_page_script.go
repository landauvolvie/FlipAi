package main

// googleVoiceInitScript is injected into the dedicated Google Voice window
// before every document. It is the only part of the call bridge that runs
// inside Google's page, and it does four jobs:
//
//   - keep the window on Google Voice and strip the capabilities FlipAi does
//     not want the page to have (see the permission note in
//     voice_call_windows.go for why that removal happens here);
//   - notice a ringing call -- wherever Google renders it, the main document,
//     a same-origin frame, or only a notification -- work out who is calling,
//     and answer it exactly as a person would, by clicking Answer, whenever
//     FlipAi says the caller is authorized;
//   - pin Google Voice's microphone and speaker to the virtual cable
//     endpoints FlipAi chose, so the call is wired to the desktop AI app
//     rather than the PC's own headset, silently and without any picker;
//   - tell FlipAi when the call starts and ends, so the desktop app's voice
//     mode is switched on and off around it.
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
  // While the phone is ringing there are only about 25 seconds before Google
  // Voice gives up and takes the call to voicemail, so the page looks much
  // more often for as long as a call is on screen.
  const RING_TICK_MS = 250;
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
    if (!a) return;
    const raw = (a.getAttribute('href') || '').trim().toLowerCase();
    // An outgoing Google Voice dial can be dispatched through a phone URI.
    // The old navigation guard cancelled it before Google's handler saw it.
    if (raw.startsWith('tel:') || raw.startsWith('callto:')) return;
    if (!allowedTopLevel(a.href)) e.preventDefault();
  }, true);

  // FlipAi grants only the microphone and notification permissions a phone
  // call needs at the WebView2 host. Keep defense-in-depth denials here for
  // browser capabilities Google Voice does not need.
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

  /* ---------- every document Google Voice renders in ----------

     Google has rendered the calling UI both in the main document and inside
     same-origin frames, and a ring that appears in a frame is exactly as real
     as one in the page. Everything that looks for controls therefore looks in
     every document it can reach; a cross-origin frame simply throws and is
     skipped. New frames are put under the same mutation observer as the page,
     because a ring must be noticed from the DOM change that carries it. */

  const observedDocs = new WeakSet();
  function docs() {
    const out = [document];
    const walk = (doc) => {
      let frames = [];
      try { frames = doc.querySelectorAll('iframe,frame'); } catch (_) { return; }
      for (const f of frames) {
        try {
          const inner = f.contentDocument;
          if (inner && out.indexOf(inner) < 0) {
            out.push(inner);
            observeDoc(inner);
            walk(inner);
          }
        } catch (_) {}
      }
    };
    walk(document);
    return out;
  }

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

  // Every lookup below is cached. Resolving the endpoint from scratch for
  // every audio element on every DOM mutation once issued thousands of host
  // round-trips a second and left the page too busy to answer a call.
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
  function routeAll() {
    for (const doc of docs()) {
      try { doc.querySelectorAll('audio,video').forEach(applySink); } catch (_) {}
    }
  }

  // Google Voice plays the caller through a media element. The sink is applied
  // from the cached endpoint and play() is still called in the same turn:
  // deferring play() past its user-gesture turn can trip autoplay policy and
  // lose the call audio altogether.
  const nativePlay = HTMLMediaElement.prototype.play;
  HTMLMediaElement.prototype.play = function(...args) {
    applySink(this);
    return nativePlay.apply(this, args);
  };

  // Force Google Voice's microphone onto the virtual endpoint FlipAi chose.
  // A video request is refused outright: this window exists for phone calls.
  if (navigator.mediaDevices && navigator.mediaDevices.getUserMedia) {
    const gum = navigator.mediaDevices.getUserMedia.bind(navigator.mediaDevices);
    navigator.mediaDevices.getUserMedia = async function(constraints) {
      if (constraints && constraints.video) return denied('The camera is disabled in FlipAi');
      let next = constraints;
      try {
        let s = await audioSettings();
        if (constraints && constraints.audio && !s.input) {
          // The cached answer can predate the first device report; a call's
          // microphone matters enough to ask again rather than open the
          // default device on a stale blank.
          settingsAt = 0;
          s = await audioSettings();
        }
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
      // first moment the device list is worth anything to the status page.
      forgetDevices();
      reportDevices(true);
      return stream;
    };
    try {
      navigator.mediaDevices.addEventListener('devicechange', () => { forgetDevices(); reportDevices(true); });
    } catch (_) {}
  }

  // FlipAi grants Google Voice's microphone and notification permissions at the
  // WebView2 host, before any prompt can appear. A page that asks
  // navigator.permissions instead of asking for the device can be told
  // "prompt" all the same -- and Google Voice partly decides whether it may
  // ring in a browser from answers like that one. This reports what FlipAi has
  // in fact already granted, and nothing else: every other permission is
  // answered by the real implementation.
  try {
    if (navigator.permissions && navigator.permissions.query) {
      const realQuery = navigator.permissions.query.bind(navigator.permissions);
      const granted = {microphone: true, notifications: true};
      navigator.permissions.query = function(descriptor) {
        const name = descriptor && descriptor.name;
        if (granted[name]) {
          return Promise.resolve({
            name: name,
            state: 'granted',
            status: 'granted',
            onchange: null,
            addEventListener: () => {},
            removeEventListener: () => {},
            dispatchEvent: () => false
          });
        }
        return realQuery(descriptor);
      };
    }
  } catch (_) {}

  /* ---------- notifications as a ring signal ----------

     Google Voice sometimes announces an incoming call through the
     Notifications API before -- or instead of -- drawing anything FlipAi can
     see. The notification itself cannot be clicked from here, but it is a
     reliable "look now" signal: a burst of immediate checks catches the
     in-page Answer control the moment it exists, without waiting out the
     poll interval. */

  let noteHint = '';
  let noteHintAt = 0;
  let notificationsPolyfilled = false;

  // Google Voice decides whether a browser can take calls partly from what the
  // browser can do, and one of the things it looks at is whether it can raise a
  // notification. WebView2 does not always expose the Notifications API, and a
  // page that finds it missing can simply not offer to ring here at all --
  // which looks, from the outside, exactly like FlipAi failing to notice calls.
  //
  // So when it is missing it is supplied: a notification object that behaves
  // correctly to the page and shows nothing. FlipAi does not want a Windows
  // toast for an incoming call; it wants the ring, and it takes the ring from
  // the page. A real Notification implementation is never replaced.
  try {
    if (typeof window.Notification === 'undefined') {
      notificationsPolyfilled = true;
      const listeners = 'onclick onclose onerror onshow'.split(' ');
      const Shim = function(title, options) {
        this.title = String(title == null ? '' : title);
        const opts = options || {};
        this.body = String(opts.body || '');
        this.tag = String(opts.tag || '');
        this.data = opts.data;
        for (const name of listeners) this[name] = null;
        // The wrapper below wraps this shim and reports the ring too. Doing it
        // twice costs nothing -- a hint is a hint and the tick it schedules is
        // single-flighted -- and doing it here means a ring is still noticed if
        // that wrapper ever fails to install.
        ringHint(this.title, this.body);
      };
      Shim.prototype.close = function() {};
      Shim.prototype.addEventListener = function() {};
      Shim.prototype.removeEventListener = function() {};
      Shim.prototype.dispatchEvent = function() { return false; };
      Shim.requestPermission = (cb) => {
        if (typeof cb === 'function') { try { cb('granted'); } catch (_) {} }
        return Promise.resolve('granted');
      };
      Object.defineProperty(Shim, 'permission', {get: () => 'granted'});
      Object.defineProperty(Shim, 'maxActions', {get: () => 0});
      window.Notification = Shim;
    }
  } catch (_) {}
  function ringHint(title, body) {
    const text = (String(title || '') + ' ' + String(body || '')).trim();
    if (!/incoming|call/i.test(text)) return;
    noteHint = text.slice(0, 200);
    noteHintAt = Date.now();
    for (const wait of [0, 300, 800, 1500]) setTimeout(() => { tick(); }, wait);
  }
  try {
    const RealNotification = window.Notification;
    if (RealNotification) {
      const Wrapped = function(title, options) {
        ringHint(title, options && options.body);
        return new RealNotification(title, options);
      };
      Wrapped.requestPermission = RealNotification.requestPermission ?
        RealNotification.requestPermission.bind(RealNotification) : (() => Promise.resolve('granted'));
      Object.defineProperty(Wrapped, 'permission', {get: () => {
        try { return RealNotification.permission; } catch (_) { return 'granted'; }
      }});
      window.Notification = Wrapped;
    }
  } catch (_) {}
  try {
    if (window.ServiceWorkerRegistration && ServiceWorkerRegistration.prototype.showNotification) {
      const show = ServiceWorkerRegistration.prototype.showNotification;
      ServiceWorkerRegistration.prototype.showNotification = function(title, options) {
        ringHint(title, options && options.body);
        return show.apply(this, arguments);
      };
    }
  } catch (_) {}

  // WebView2 runs this script at document-created time, when <html> may not
  // exist yet. Observing a null root throws, and the throw aborts the rest of
  // this script -- which is the entire call bridge. The observer therefore
  // waits for a root instead of assuming one.
  //
  // The observer also drives a tick. The poll below runs on a timer, and a
  // timer is exactly what Chromium slows down in a window nobody is looking at
  // -- and this window is deliberately minimized. A ring that arrives while the
  // timer is throttled has to be noticed from the DOM change that carries it,
  // or the call is simply never answered. Frames get the same observer for the
  // same reason.
  let tickQueued = false;
  const onMutation = () => {
    if (tickQueued) return;
    tickQueued = true;
    setTimeout(() => { tickQueued = false; routeAll(); tick(); }, 250);
  };
  function observeDoc(doc) {
    if (observedDocs.has(doc)) return true;
    const root = doc.documentElement || doc.body;
    if (!root) return false;
    observedDocs.add(doc);
    try { new MutationObserver(onMutation).observe(root, {childList: true, subtree: true}); } catch (_) {}
    return true;
  }
  if (!observeDoc(document)) {
    document.addEventListener('DOMContentLoaded', () => observeDoc(document), {once: true});
    document.addEventListener('readystatechange', () => observeDoc(document), {once: true});
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
  const CHROME_LINE = /^(answer|accept|decline|reject|ignore|dismiss|hang\s*up|end\s+call|leave\s+call|incoming\s+call|mute|unmute|keypad|hold|more|options|calling|google\s+voice|block|report\s+spam|send\s+to\s+voicemail|voicemail|mobile|work|home|cell|main|iphone|android|\d{1,2}:\d{2}(:\d{2})?)$/i;
  const FROM_RE = /(?:incoming\s+call\s+from|call\s+from|calling\s+from)\s+(.+?)\s*$/i;

  const visible = (el) => !!el && !!(el.offsetWidth || el.offsetHeight || el.getClientRects().length);
  const buttonName = (b) => ((b.getAttribute('aria-label') || '') + ' ' + (b.innerText || b.textContent || '')).trim();
  function buttons() {
    const out = [];
    for (const doc of docs()) {
      try {
        for (const b of doc.querySelectorAll('button,[role="button"]')) {
          if (visible(b)) out.push(b);
        }
      } catch (_) {}
    }
    return out;
  }
  const ANSWER_RE = /(^|\b)(answer|accept|pick\s*up|take\s+call)(\b|$)/i;
  const DECLINE_RE = /(decline|reject|ignore|dismiss|voicemail|block|spam)/i;
  const findAnswer = () => buttons().find(b => {
    const name = buttonName(b);
    if (!ANSWER_RE.test(name)) return false;
    // "Decline" and "Send to voicemail" sit beside it; never click those.
    return !DECLINE_RE.test(name);
  });
  const findHangup = () => buttons().find(b => /(hang\s*up|end\s+call|leave\s+call|end\s+the\s+call)/i.test(buttonName(b)));

  // callControlsPresent is a weaker second opinion, used only to keep a call
  // FlipAi already knows about from being declared over when Google renames the
  // control that ends it.
  //
  // It may never start a call. Google Voice's ordinary page offers a keypad to
  // dial with and a mute control of its own, so this matches a page with no
  // call on it -- and when it was allowed to mean "a call is up", FlipAi
  // believed it was permanently in one, reported it as answered by hand, and
  // ignored every real ring after that as call waiting.
  const callControlsPresent = () => {
    const names = buttons().map(buttonName).join(' | ');
    return /\bmute\b/i.test(names) && /\b(keypad|dialpad)\b/i.test(names);
  };

  // Google Voice's ringing card listens for a pointer sequence on some builds
  // and for click on others, and a bare element.click() is ignored by the ones
  // that expect the first. Sending the whole sequence costs nothing and is the
  // difference between an allowed caller being answered and being sent to
  // voicemail. FlipAi's control channel presses the same control a different
  // way a moment later if this does not take.
  function pressAnswer(b) {
    try { b.focus(); } catch (_) {}
    const base = {bubbles: true, cancelable: true, composed: true, button: 0};
    for (const type of ['pointerdown', 'mousedown', 'pointerup', 'mouseup']) {
      try {
        const Ctor = type.indexOf('pointer') === 0 && window.PointerEvent ? PointerEvent : MouseEvent;
        const opts = Object.assign({}, base, {buttons: type.slice(-2) === 'up' ? 0 : 1});
        b.dispatchEvent(new Ctor(type, opts));
      } catch (_) {}
    }
    try { b.click(); } catch (_) {}
  }

  // controlsSnapshot is what FlipAi can currently see. Whether a ring is even
  // reaching this window is otherwise invisible: Google Voice only rings in a
  // browser when "Receive calls on this device" is switched on in its own
  // settings, and until then nothing at all happens here. A recent incoming
  // notification is included, because it is proof a call reached this browser
  // even when no Answer control ever appeared.
  function controlsSnapshot() {
    const names = [];
    // Worth knowing on the status page: a browser FlipAi had to supply the
    // Notifications API to is one Google Voice might have refused to ring in.
    if (notificationsPolyfilled) names.push('[notifications supplied by FlipAi]');
    if (noteHint && Date.now() - noteHintAt < 60000) names.push('[notification: ' + noteHint + ']');
    for (const b of buttons()) {
      const name = buttonName(b).replace(/\s+/g, ' ').trim();
      if (name && name.length <= 60 && names.indexOf(name) < 0) names.push(name);
      if (names.length >= 40) break;
    }
    return names.join(' | ');
  }

  // scopeText reads a container the way a person reads the ringing card: the
  // words describing the caller, without the text on the buttons. It walks text
  // nodes rather than using innerText so the result does not depend on layout.
  function scopeText(scope) {
    const parts = [];
    const doc = scope.ownerDocument || document;
    const walker = doc.createTreeWalker(scope, NodeFilter.SHOW_TEXT);
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
      // Innermost first, and never past the ringing card itself: the walk
      // stops at the dialog holding the Answer button, because anything
      // further out is the rest of the Google Voice UI -- a ringing card that
      // names nobody must not inherit a caller from the thread list beside it.
      const dialog = answer.closest('[role="dialog"],[role="alertdialog"]');
      let node = answer.parentElement;
      const body = (answer.ownerDocument || document).body;
      for (let i = 0; i < 5 && node && node !== body; i++) {
        push(node);
        if (dialog && node === dialog) break;
        node = node.parentElement;
      }
      push(dialog);
    }
    for (const doc of docs()) {
      try { doc.querySelectorAll('[role="dialog"],[role="alertdialog"]').forEach(push); } catch (_) {}
    }
    return scopes;
  }

  function callerIdentity() {
    // The Answer button's own accessible name is the most direct statement of
    // who is calling when Google provides one.
    const answer = findAnswer();
    if (answer) {
      const said = (answer.getAttribute('aria-label') || '').match(FROM_RE);
      if (said) {
        const spoken = said[1].trim();
        const n = phoneFrom(spoken);
        if (n) return {number: n, label: ''};
        if (spoken) return {number: '', label: spoken.slice(0, 120)};
      }
    }
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
    // A notification that named the caller is the last resort, for a ring
    // that only ever announced itself that way.
    if (noteHint && Date.now() - noteHintAt < 30000) {
      const n = phoneFrom(noteHint);
      if (n) return {number: n, label: ''};
      const said = noteHint.match(FROM_RE);
      if (said) return {number: '', label: said[1].trim().slice(0, 120)};
    }
    return {number: '', label: ''};
  }

  /* ---------- the call itself ---------- */

  let caller = {number: '', label: ''};
  let inCall = false;
  let answering = false;
  let ticking = false;
  let quiet = 0;
  let ringing = false;

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
      try { await window.flipVoicePage(href, signedIn, controlsSnapshot()); } catch (_) {}

      await refreshEndpoints();
      routeAll();
      reportDevices(false);

      // Answering is not an option or a mode. FlipAi answers exactly when the
      // caller is authorized for an agent, and flipVoiceIncoming is that whole
      // decision; an unauthorized caller simply keeps ringing.
      const answer = findAnswer();
      if (answer && !inCall && !answering) {
        const seen = callerIdentity();
        if (seen.number || seen.label) caller = seen;
        answering = true;
        try {
          const authorized = await window.flipVoiceIncoming(caller.number, caller.label);
          if (authorized && answer.isConnected) pressAnswer(answer);
        } catch (_) {
        } finally {
          // Short enough that an allowed caller gets many attempts inside one
          // ring, long enough that a card Google is still animating in is not
          // pressed several times in the same frame.
          setTimeout(() => { answering = false; }, 700);
        }
      }

      // Only the control that ends a call may say a call has started.
      const hang = findHangup();
      if (hang) {
        quiet = 0;
        if (!inCall) {
          inCall = true;
          try { await window.flipVoiceAnswered(caller.number, caller.label); } catch (_) {}
        }
      } else if (inCall) {
        // An Answer control with no hang-up control beside it means the call
        // FlipAi thought was up is over and a new one is ringing. That is not
        // a frame to wait out; Google Voice is offering to answer right now.
        if (answer) {
          quiet = 0;
          inCall = false;
          caller = {number: '', label: ''};
          try { await window.flipVoiceEnded(); } catch (_) {}
        } else if (callControlsPresent()) {
          // Still showing the controls a call offers: the hang-up control has
          // been renamed, not removed.
          quiet = 0;
        } else {
          // Google Voice renders neither an Answer nor a hang-up control for a
          // moment while it swaps one card for another. Ending the call on that
          // single frame shut the desktop app's voice mode down in the middle
          // of a conversation, so the page has to see it gone more than once.
          quiet++;
          if (quiet >= 2) {
            quiet = 0;
            inCall = false;
            caller = {number: '', label: ''};
            try { await window.flipVoiceEnded(); } catch (_) {}
          }
        }
      } else {
        quiet = 0;
      }
      ringing = !!(answer || hang);
    } catch (_) {
    } finally {
      ticking = false;
    }
  }

  // Endpoint names are hidden from a page until it holds a microphone grant,
  // so FlipAi opens the microphone once at startup and immediately closes it.
  // That is what reveals real device names to the automatic cable detection
  // before a call has ever arrived, and it settles the microphone permission
  // ahead of the first ring instead of during it -- Google Voice will not
  // treat a browser without it as able to take calls.
  async function primeDevices() {
    try {
      const stream = await navigator.mediaDevices.getUserMedia({audio: true});
      stream.getTracks().forEach(t => t.stop());
    } catch (_) {}
    forgetDevices();
    await reportDevices(true);
  }

  const loop = () => { tick().then(() => setTimeout(loop, ringing ? RING_TICK_MS : TICK_MS)); };
  setTimeout(() => { primeDevices().then(loop); }, 250);
  // The harness drives ticks directly instead of waiting on the timer.
  window.__flipVoiceTick = tick;
})();
`
