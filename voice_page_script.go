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
//   - carry the call's sound to and from the built-in Codex voice page over a
//     WebRTC connection that never leaves this machine: the caller's voice is
//     lifted out of Google Voice's own audio elements, and the stream Google
//     Voice believes is its microphone is really the agent talking. No real
//     microphone is opened, nothing is played through the PC's speakers, and
//     there is no audio device anywhere in the path;
//   - tell FlipAi when the call starts and ends, so the agent's voice mode is
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
  const RELAY_MS = 300;

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
  // grants are broken in the browser binding FlipAi uses. Everything except
  // what a phone call needs is therefore taken away here, before Google Voice
  // can ask.
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

  /* ---------- the virtual audio bridge ----------

     Two Web Audio junction points carry the whole conversation:

       toAgent   -- everything the caller says. Google Voice plays the caller
                    through a media element; the stream behind that element is
                    tapped into this node, and the element itself is muted so
                    nothing near the PC hears the call.
       fromAgent -- everything the agent says. It arrives from the Codex voice
                    page over the WebRTC link and is poured into this node,
                    whose stream is what Google Voice receives when it asks
                    for a microphone.

     The link between the pages is an RTCPeerConnection whose offer/answer/ICE
     messages travel through FlipAi (window.flipVoiceRelay*), because two
     WebViews have no other way to reach each other. The media itself flows
     directly between the two browser processes on this machine. */

  let actx = null, toAgent = null, fromAgent = null;
  function audioGraph() {
    if (!actx) {
      actx = new AudioContext();
      toAgent = actx.createMediaStreamDestination();
      fromAgent = actx.createMediaStreamDestination();
    }
    if (actx.state === 'suspended') { try { actx.resume(); } catch (_) {} }
    return actx;
  }

  // Chromium only decodes a remote WebRTC track that is attached to a media
  // element. The monitor element is that attachment: muted, never in the DOM,
  // existing so the track flows into the audio graph.
  const monitors = [];
  function monitorStream(ms) {
    const a = document.createElement('audio');
    a.__flipInternal = true;
    a.muted = true;
    a.srcObject = ms;
    const p = a.play();
    if (p && p.catch) p.catch(() => {});
    monitors.push(a);
    if (monitors.length > 8) monitors.shift();
  }

  let pc = null;
  let bridgeState = 'idle';
  let lastOffer = 0;
  async function reportBridge(state) {
    if (state === bridgeState) return;
    bridgeState = state;
    try { await window.flipVoiceBridge(state); } catch (_) {}
  }
  function sendToAgent(msg) {
    try { window.flipVoiceRelaySend(JSON.stringify(msg)); } catch (_) {}
  }

  // This page is always the offerer; the Codex page always answers. Whoever
  // loads second announces itself ('hello' from the agent, a fresh offer from
  // here), so a reload on either side rebuilds the same connection.
  //
  // Both pages come up within moments of each other, so the startup offer and
  // the offer answering the agent's 'hello' can be in flight at once. Every
  // negotiation therefore carries an id, and anything about an older
  // negotiation is dropped: the newest offer always wins on both sides.
  let offerSeq = 0;
  async function newPeer() {
    audioGraph();
    lastOffer = Date.now();
    if (pc) { try { pc.close(); } catch (_) {} pc = null; }
    const id = ++offerSeq;
    pc = new RTCPeerConnection();
    pc.addTrack(toAgent.stream.getAudioTracks()[0], toAgent.stream);
    pc.onicecandidate = (e) => { if (e.candidate) sendToAgent({type: 'ice', id: id, candidate: e.candidate.toJSON()}); };
    pc.ontrack = (e) => {
      const ms = (e.streams && e.streams[0]) ? e.streams[0] : new MediaStream([e.track]);
      monitorStream(ms);
      try { audioGraph().createMediaStreamSource(ms).connect(fromAgent); } catch (_) {}
    };
    const mine = pc;
    pc.onconnectionstatechange = () => {
      if (pc !== mine) return;
      const s = mine.connectionState;
      if (s === 'connected') reportBridge('connected');
      else if (s === 'failed' || s === 'disconnected' || s === 'closed') reportBridge('failed');
    };
    reportBridge('connecting');
    const offer = await pc.createOffer();
    if (id !== offerSeq) return; // a newer negotiation replaced this one mid-await
    await pc.setLocalDescription(offer);
    sendToAgent({type: 'offer', id: id, sdp: pc.localDescription.sdp});
  }

  async function onAgentMessage(msg) {
    if (!msg || typeof msg !== 'object') return;
    if (msg.type === 'hello') { await newPeer(); return; }
    if (!pc || msg.id !== offerSeq) return;
    if (msg.type === 'answer' && msg.sdp) {
      try { await pc.setRemoteDescription({type: 'answer', sdp: msg.sdp}); } catch (_) {}
    } else if (msg.type === 'ice' && msg.candidate) {
      try { await pc.addIceCandidate(msg.candidate); } catch (_) {}
    }
  }

  let pumping = false;
  async function pumpRelay() {
    if (pumping) return;
    pumping = true;
    try {
      for (let i = 0; i < 24; i++) {
        let raw = '';
        try { raw = await window.flipVoiceRelayRecv(); } catch (_) { break; }
        if (!raw) break;
        let msg = null;
        try { msg = JSON.parse(raw); } catch (_) { continue; }
        await onAgentMessage(msg);
      }
    } finally { pumping = false; }
  }

  // A connection that failed -- the Codex window restarted, or never came up
  // -- is retried from scratch, but not so often that two pages starting at
  // once trip over each other's half-finished handshakes.
  function bridgeUpkeep() {
    if (!pc || bridgeState === 'failed') {
      if (Date.now() - lastOffer > 8000) newPeer().catch(() => {});
    }
  }

  /* ---------- keep the call silent and lift the caller's voice ----------

     Google Voice attaches the caller's stream to a media element with
     srcObject. Intercepting that property is both halves of the audio path at
     once: the stream is tapped into toAgent for the agent to hear, and the
     element is silenced so the conversation never reaches the PC's speakers.
     The ringtone plays through an ordinary src= element and is deliberately
     left alone -- a phone ringing in the room is fine, a conversation is not. */

  const captured = new WeakSet();
  function silence(el) {
    el.__flipSilenced = true;
    try { el.muted = true; } catch (_) {}
    try { el.volume = 0; } catch (_) {}
  }
  function captureCallMedia(el, ms) {
    if (!ms || typeof ms.getAudioTracks !== 'function') return;
    const tracks = ms.getAudioTracks();
    if (!tracks.length) return;
    silence(el);
    // Never route the agent's own voice back to the agent: a stream built
    // from the bridge microphone is the agent talking, not the caller.
    if (tracks.every((t) => t.__flipBridge)) return;
    if (captured.has(ms)) return;
    captured.add(ms);
    try { audioGraph().createMediaStreamSource(ms).connect(toAgent); } catch (_) {}
  }

  const mediaProto = HTMLMediaElement.prototype;
  const srcObjectDesc = Object.getOwnPropertyDescriptor(mediaProto, 'srcObject');
  if (srcObjectDesc && srcObjectDesc.set) {
    Object.defineProperty(mediaProto, 'srcObject', {
      configurable: true,
      get() { return srcObjectDesc.get.call(this); },
      set(v) {
        srcObjectDesc.set.call(this, v);
        if (!this.__flipInternal) captureCallMedia(this, v);
      }
    });
  }
  // A silenced element stays silenced even if the page unmutes it later.
  const mutedDesc = Object.getOwnPropertyDescriptor(mediaProto, 'muted');
  if (mutedDesc && mutedDesc.set) {
    Object.defineProperty(mediaProto, 'muted', {
      configurable: true,
      get() { return mutedDesc.get.call(this); },
      set(v) { mutedDesc.set.call(this, this.__flipSilenced ? true : v); }
    });
  }
  const volumeDesc = Object.getOwnPropertyDescriptor(mediaProto, 'volume');
  if (volumeDesc && volumeDesc.set) {
    Object.defineProperty(mediaProto, 'volume', {
      configurable: true,
      get() { return volumeDesc.get.call(this); },
      set(v) { volumeDesc.set.call(this, this.__flipSilenced ? 0 : v); }
    });
  }
  const nativePlay = HTMLMediaElement.prototype.play;
  HTMLMediaElement.prototype.play = function(...args) {
    if (this.__flipSilenced) { try { this.muted = true; } catch (_) {} }
    return nativePlay.apply(this, args);
  };

  /* ---------- the microphone Google Voice believes it has ---------- */

  // Google Voice's microphone is the agent's voice. No device is opened, no
  // permission prompt can appear, and locking the PC changes nothing because
  // no hardware is in use. A video request is refused outright: this window
  // exists for phone calls.
  if (navigator.mediaDevices && navigator.mediaDevices.getUserMedia) {
    navigator.mediaDevices.getUserMedia = async function(constraints) {
      if (constraints && constraints.video) return denied('The camera is disabled in FlipAi');
      audioGraph();
      const ms = fromAgent.stream.clone();
      ms.getAudioTracks().forEach((t) => { t.__flipBridge = true; });
      return ms;
    };
  }

  // WebView2 runs this script at document-created time, when <html> may not
  // exist yet. Observing a null root throws, and the throw aborts the rest of
  // this script -- which is the entire call bridge. The observer therefore
  // waits for a root instead of assuming one.
  //
  // The observer also drives a tick. The poll below runs on a timer, and a
  // timer is exactly what Chromium slows down in a window nobody is looking at
  // -- and this window is deliberately minimized. A ring that arrives while the
  // timer is throttled has to be noticed from the DOM change that carries it,
  // or the call is simply never answered.
  let tickQueued = false;
  const observeDocument = () => {
    const root = document.documentElement || document.body;
    if (!root) return false;
    new MutationObserver(() => {
      if (tickQueued) return;
      tickQueued = true;
      setTimeout(() => { tickQueued = false; tick(); }, 250);
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
  const CHROME_LINE = /^(answer|accept|decline|reject|ignore|dismiss|hang\s*up|end\s+call|leave\s+call|incoming\s+call|mute|unmute|keypad|hold|more|options|calling|google\s+voice|block|report\s+spam|send\s+to\s+voicemail|voicemail|mobile|work|home|cell|main|iphone|android|\d{1,2}:\d{2}(:\d{2})?)$/i;
  const FROM_RE = /(?:incoming\s+call\s+from|call\s+from|calling\s+from)\s+(.+?)\s*$/i;

  const visible = (el) => !!el && !!(el.offsetWidth || el.offsetHeight || el.getClientRects().length);
  const buttonName = (b) => ((b.getAttribute('aria-label') || '') + ' ' + (b.innerText || b.textContent || '')).trim();
  const buttons = () => Array.from(document.querySelectorAll('button,[role="button"]')).filter(visible);
  const ANSWER_RE = /(^|\b)(answer|accept|pick\s*up|take\s+call)(\b|$)/i;
  const DECLINE_RE = /(decline|reject|ignore|dismiss|voicemail|block|spam)/i;
  const findAnswer = () => buttons().find(b => {
    const name = buttonName(b);
    if (!ANSWER_RE.test(name)) return false;
    // "Decline" and "Send to voicemail" sit beside it; never click those.
    return !DECLINE_RE.test(name);
  });
  const findHangup = () => buttons().find(b => /(hang\s*up|end\s+call|leave\s+call|end\s+the\s+call)/i.test(buttonName(b)));

  // controlsSnapshot is what FlipAi can currently see. Whether a ring is even
  // reaching this window is otherwise invisible: Google Voice only rings in a
  // browser when "Receive calls on this device" is switched on in its own
  // settings, and until then nothing at all happens here.
  function controlsSnapshot() {
    const names = [];
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
      try { await window.flipVoicePage(href, signedIn, controlsSnapshot()); } catch (_) {}

      await pumpRelay();
      bridgeUpkeep();

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

  const loop = () => { tick().then(() => setTimeout(loop, TICK_MS)); };
  setTimeout(() => {
    // Offer the bridge immediately: the Codex page may already be waiting, and
    // a call can arrive before the first tick otherwise finds it.
    newPeer().catch(() => {});
    loop();
  }, 250);
  // The handshake with the Codex page must not wait on the slow tick: ICE is a
  // short back-and-forth, and every leg would otherwise cost 700ms.
  setInterval(() => { pumpRelay(); }, RELAY_MS);
  // The harness drives ticks directly instead of waiting on the timer.
  window.__flipVoiceTick = tick;
})();
`
