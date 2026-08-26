package main

// codexVoiceInitScript is injected into the dedicated Codex voice window --
// the hidden second browser FlipAi keeps on chatgpt.com -- before every
// document. It is the other half of the phone call: the Google Voice page
// carries the caller, this page carries the agent, and the two exchange sound
// over a WebRTC connection that never leaves this machine.
//
// Its jobs mirror googleVoiceInitScript's:
//
//   - keep the window on ChatGPT and strip the capabilities FlipAi does not
//     want the page to have;
//   - answer the Google Voice page's WebRTC offer, hand ChatGPT the caller's
//     voice as its microphone, and capture everything ChatGPT says -- however
//     it plays it -- into the return leg, keeping the PC's own speakers and
//     microphone out of it entirely;
//   - enter ChatGPT's voice mode when FlipAi says a call has been answered,
//     and leave it when the call ends;
//   - report once a second whether it is running, signed in, and in voice
//     mode, so Settings can say whether a call would reach anybody.
//
// It lives in a platform-independent file for the same reason the Google
// Voice script does: the harness runs this exact string in headless Chromium
// against a stand-in ChatGPT page, with the real Go relay in the middle, which
// is the only way the bridge is testable without a phone line.
const codexVoiceInitScript = `
(() => {
  if (window.__flipCodexInstalled) return;
  window.__flipCodexInstalled = true;

  const TICK_MS = 900;
  const RELAY_MS = 300;

  /* ---------- keep this window a ChatGPT window ---------- */

  const allowedTopLevel = (href) => {
    try {
      const h = new URL(href, location.href).hostname.toLowerCase();
      return h === 'chatgpt.com' || h.endsWith('.chatgpt.com') ||
        h === 'openai.com' || h.endsWith('.openai.com') ||
        h === 'accounts.google.com' || h === 'appleid.apple.com' ||
        h === 'login.live.com' || h === 'login.microsoftonline.com';
    } catch (_) { return false; }
  };
  document.addEventListener('click', (e) => {
    const a = e.target && e.target.closest ? e.target.closest('a[href]') : null;
    if (a && !allowedTopLevel(a.href)) e.preventDefault();
  }, true);

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

  /* ---------- the virtual audio bridge, agent side ----------

     Junction points, mirroring the Google Voice page:

       toCall   -- everything the agent says, captured from every way ChatGPT
                   can make sound, sent to the Google Voice page and on to the
                   caller.
       fromCall -- the caller's voice, arriving over the WebRTC link. It is
                   what ChatGPT receives when it asks for a microphone.

     Everything is silenced locally: whatever ChatGPT plays lands in toCall
     instead of the PC's speakers. */

  const RealAudioContext = window.AudioContext || window.webkitAudioContext;
  let actx = null, toCall = null, fromCall = null;
  function audioGraph() {
    if (!actx) {
      actx = new RealAudioContext();
      toCall = actx.createMediaStreamDestination();
      fromCall = actx.createMediaStreamDestination();
    }
    if (actx.state === 'suspended') { try { actx.resume(); } catch (_) {} }
    return actx;
  }

  const monitors = [];
  function monitorStream(ms) {
    // Chromium only decodes a remote WebRTC track that is attached to a media
    // element; the monitor is that attachment, muted and never in the DOM.
    const a = document.createElement('audio');
    a.__flipInternal = true;
    a.muted = true;
    a.srcObject = ms;
    const p = a.play();
    if (p && p.catch) p.catch(() => {});
    monitors.push(a);
    if (monitors.length > 8) monitors.shift();
  }

  const forwarded = new WeakSet();
  function forwardToCall(ms) {
    if (!ms || typeof ms.getAudioTracks !== 'function' || !ms.getAudioTracks().length) return;
    if (forwarded.has(ms)) return;
    forwarded.add(ms);
    try { audioGraph().createMediaStreamSource(ms).connect(toCall); } catch (_) {}
  }

  /* ---------- capture every way ChatGPT can make sound ---------- */

  const mediaProto = HTMLMediaElement.prototype;
  function silence(el) {
    el.__flipSilenced = true;
    try { el.muted = true; } catch (_) {}
    try { el.volume = 0; } catch (_) {}
  }

  // 1. Streams attached to media elements (ChatGPT's realtime voice arrives
  //    this way). The element is muted and the stream forwarded.
  const srcObjectDesc = Object.getOwnPropertyDescriptor(mediaProto, 'srcObject');
  if (srcObjectDesc && srcObjectDesc.set) {
    Object.defineProperty(mediaProto, 'srcObject', {
      configurable: true,
      get() { return srcObjectDesc.get.call(this); },
      set(v) {
        srcObjectDesc.set.call(this, v);
        if (this.__flipInternal) return;
        if (v && typeof v.getAudioTracks === 'function') {
          const tracks = v.getAudioTracks();
          if (tracks.length) {
            silence(this);
            // Never loop the caller's own voice back at them: a stream built
            // from the bridge microphone is the caller talking.
            if (!tracks.every((t) => t.__flipBridge)) forwardToCall(v);
          }
        }
      }
    });
  }
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

  // 2. Ordinary file/URL playback (spoken replies delivered as audio files).
  //    Routing the element through the audio graph both captures it and takes
  //    it off the speakers, which is exactly what a MediaElementSourceNode
  //    does by construction.
  const tapped = new WeakSet();
  const nativePlay = mediaProto.play;
  mediaProto.play = function(...args) {
    if (!this.__flipInternal && !this.srcObject && !tapped.has(this)) {
      tapped.add(this);
      try { audioGraph().createMediaElementSource(this).connect(toCall); } catch (_) {}
    }
    if (this.__flipSilenced) { try { this.muted = true; } catch (_) {} }
    return nativePlay.apply(this, args);
  };

  // 3. Web Audio playback (streamed speech decoded into buffers). Every
  //    AudioContext the page creates gets a capture node, and any connection
  //    the page makes to that context's speakers is quietly redirected into
  //    the capture node instead. FlipAi's own graph is built from the saved
  //    constructor above, so it is not affected.
  function hookContext(c) {
    try {
      c.__flipCap = c.createMediaStreamDestination();
      forwardToCall(c.__flipCap.stream);
    } catch (_) {}
  }
  const PatchedAC = function(...a) {
    const c = new RealAudioContext(...a);
    hookContext(c);
    return c;
  };
  PatchedAC.prototype = RealAudioContext.prototype;
  try { window.AudioContext = PatchedAC; } catch (_) {}
  try { if (window.webkitAudioContext) window.webkitAudioContext = PatchedAC; } catch (_) {}
  const realConnect = AudioNode.prototype.connect;
  AudioNode.prototype.connect = function(dest, ...rest) {
    try {
      if (dest && this.context && this.context.__flipCap && dest === this.context.destination) {
        return realConnect.call(this, this.context.__flipCap, ...rest);
      }
    } catch (_) {}
    return realConnect.call(this, dest, ...rest);
  };

  /* ---------- the microphone ChatGPT believes it has ---------- */

  if (navigator.mediaDevices && navigator.mediaDevices.getUserMedia) {
    navigator.mediaDevices.getUserMedia = async function(constraints) {
      if (constraints && constraints.video) return denied('The camera is disabled in FlipAi');
      audioGraph();
      const ms = fromCall.stream.clone();
      ms.getAudioTracks().forEach((t) => { t.__flipBridge = true; });
      return ms;
    };
  }

  /* ---------- the peer connection: this side answers ---------- */

  let pc = null;
  // The Google Voice page numbers its negotiations; the newest offer always
  // wins, and anything about an older one is dropped.
  let currentOffer = 0;
  function sendToCall(msg) {
    try { window.flipCodexRelaySend(JSON.stringify(msg)); } catch (_) {}
  }
  async function onCallMessage(msg) {
    if (!msg || typeof msg !== 'object') return;
    if (msg.type === 'offer' && msg.sdp) {
      audioGraph();
      if (pc) { try { pc.close(); } catch (_) {} pc = null; }
      const id = msg.id || 0;
      currentOffer = id;
      pc = new RTCPeerConnection();
      pc.addTrack(toCall.stream.getAudioTracks()[0], toCall.stream);
      pc.onicecandidate = (e) => { if (e.candidate) sendToCall({type: 'ice', id: id, candidate: e.candidate.toJSON()}); };
      pc.ontrack = (e) => {
        const ms = (e.streams && e.streams[0]) ? e.streams[0] : new MediaStream([e.track]);
        monitorStream(ms);
        try { audioGraph().createMediaStreamSource(ms).connect(fromCall); } catch (_) {}
      };
      try {
        await pc.setRemoteDescription({type: 'offer', sdp: msg.sdp});
        const answer = await pc.createAnswer();
        if (id !== currentOffer) return; // a newer offer replaced this one mid-await
        await pc.setLocalDescription(answer);
        sendToCall({type: 'answer', id: id, sdp: pc.localDescription.sdp});
      } catch (_) {}
      return;
    }
    if (!pc) return;
    if (msg.type === 'ice' && msg.candidate) {
      if (msg.id !== currentOffer) return;
      try { await pc.addIceCandidate(msg.candidate); } catch (_) {}
    } else if (msg.type === 'voice-start') {
      wantVoice = true;
      voiceDeadline = Date.now() + 30000;
      lastError = '';
    } else if (msg.type === 'voice-stop') {
      wantVoice = false;
    }
  }

  let pumping = false;
  async function pumpRelay() {
    if (pumping) return;
    pumping = true;
    try {
      for (let i = 0; i < 24; i++) {
        let raw = '';
        try { raw = await window.flipCodexRelayRecv(); } catch (_) { break; }
        if (!raw) break;
        let msg = null;
        try { msg = JSON.parse(raw); } catch (_) { continue; }
        await onCallMessage(msg);
      }
    } finally { pumping = false; }
  }

  /* ---------- driving ChatGPT's voice mode ---------- */

  const visible = (el) => !!el && !!(el.offsetWidth || el.offsetHeight || el.getClientRects().length);
  const buttonName = (b) => ((b.getAttribute('aria-label') || '') + ' ' + (b.innerText || b.textContent || '')).trim();
  const buttons = () => Array.from(document.querySelectorAll('button,[role="button"]')).filter(visible);
  function controlsSnapshot() {
    const names = [];
    for (const b of buttons()) {
      const name = buttonName(b).replace(/\s+/g, ' ').trim();
      if (name && name.length <= 60 && names.indexOf(name) < 0) names.push(name);
      if (names.length >= 40) break;
    }
    return names.join(' | ');
  }

  // Start controls: ChatGPT has labelled its voice entry "Use voice mode",
  // "Start voice mode", "Voice mode", and plain "Voice" across redesigns, so
  // the match is a family, with everything that ends or merely transcribes
  // voice excluded. Dictation is the near-miss to avoid: it types instead of
  // talking.
  const START_RE = /(use\s+voice|start\s+voice|voice\s+mode|voice\s+conversation|voice\s+chat|advanced\s+voice|^voice$)/i;
  const START_EXCLUDE_RE = /(end|stop|close|leave|exit|dictat|read\s+aloud|settings|search)/i;
  const STOP_RE = /(end\s+voice|stop\s+voice|close\s+voice|exit\s+voice|leave\s+voice|end\s+conversation|end\s+call|turn\s+off\s+voice)/i;
  const findStartVoice = () => buttons().find((b) => {
    const name = buttonName(b);
    return START_RE.test(name) && !START_EXCLUDE_RE.test(name);
  });
  const findStopVoice = () => buttons().find((b) => STOP_RE.test(buttonName(b)));
  const voiceActive = () => !!findStopVoice();

  let wantVoice = false;
  let voiceDeadline = 0;
  let lastClick = 0;
  let lastError = '';
  function driveVoiceMode() {
    const active = voiceActive();
    if (wantVoice && !active) {
      const b = findStartVoice();
      if (b && Date.now() - lastClick > 1500) {
        lastClick = Date.now();
        try { b.click(); } catch (_) {}
      } else if (!b && Date.now() > voiceDeadline && !lastError) {
        lastError = 'FlipAi could not find the voice mode control on ChatGPT. The call stays connected, but the agent is not listening. Open the Codex voice window from Settings to see what the page is showing.';
      }
    } else if (!wantVoice && active) {
      const b = findStopVoice();
      if (b && Date.now() - lastClick > 1500) {
        lastClick = Date.now();
        try { b.click(); } catch (_) {}
      }
    }
    return active;
  }

  /* ---------- status ---------- */

  const SIGNIN_RE = /^(log\s*in|sign\s*up|sign\s*in|get\s+started|continue\s+with)/i;
  function looksSignedIn() {
    if (!/(^|\.)chatgpt\.com$/.test(location.hostname)) return false;
    for (const b of buttons()) {
      if (SIGNIN_RE.test(buttonName(b))) return false;
    }
    return true;
  }

  let ticking = false;
  async function tick() {
    if (ticking) return;
    ticking = true;
    try {
      const href = location.href;
      if (!allowedTopLevel(href)) {
        location.replace('https://chatgpt.com/');
        return;
      }
      await pumpRelay();
      const active = driveVoiceMode();
      try {
        await window.flipCodexStatus(href, looksSignedIn(), active, controlsSnapshot(), lastError);
      } catch (_) {}
    } catch (_) {
    } finally {
      ticking = false;
    }
  }

  // The voice-mode button and the ringing bridge both have to work in a
  // window nobody is looking at, so DOM changes drive ticks exactly as they
  // do in the Google Voice window.
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

  const loop = () => { tick().then(() => setTimeout(loop, TICK_MS)); };
  setTimeout(() => {
    // Announce this page to the Google Voice side; it responds with a fresh
    // WebRTC offer whether it loaded before or after this window.
    sendToCall({type: 'hello'});
    loop();
  }, 250);
  setInterval(() => { pumpRelay(); }, RELAY_MS);
  // The harness drives ticks directly instead of waiting on the timer.
  window.__flipCodexTick = tick;
})();
`
