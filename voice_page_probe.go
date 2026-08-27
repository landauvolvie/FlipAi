package main

// These three scripts are what FlipAi's control channel runs inside the Google
// Voice page. They are deliberately platform-independent so the browser
// harness in voice_page_test.go can run the exact strings the product runs,
// against a stand-in Google Voice page, in headless Chromium on any OS.
//
// They share one idea of what a call looks like: a visible control whose
// accessible name answers the call, a visible control whose accessible name
// ends it, and the words around them describing who is calling. Nothing here
// depends on a Google class name, a private API, or a DOM shape, because all
// three change without notice.

// voiceCallDOMHelpers is the common preamble. It is duplicated into each
// script rather than injected once, because each script is evaluated on its own
// in a page FlipAi does not control the lifetime of.
const voiceCallDOMHelpers = `
  const docs = () => {
    const out = [document];
    for (let i = 0; i < out.length; i++) {
      let frames = [];
      try { frames = out[i].querySelectorAll('iframe,frame'); } catch (_) {}
      for (const f of frames) {
        try {
          const d = f.contentDocument;
          if (d && !out.includes(d)) out.push(d);
        } catch (_) {}
      }
    }
    return out;
  };
  const visible = (el) => {
    if (!el) return false;
    if (!(el.offsetWidth || el.offsetHeight || el.getClientRects().length)) return false;
    try {
      const style = (el.ownerDocument.defaultView || window).getComputedStyle(el);
      if (style && (style.visibility === 'hidden' || style.display === 'none')) return false;
    } catch (_) {}
    return true;
  };
  const buttonName = (b) => ((b.getAttribute('aria-label') || '') + ' ' + (b.innerText || b.textContent || '')).replace(/\s+/g, ' ').trim();
  const allButtons = () => {
    const out = [];
    for (const d of docs()) {
      try {
        for (const b of d.querySelectorAll('button,[role="button"],[role="menuitem"]')) {
          if (visible(b)) out.push(b);
        }
      } catch (_) {}
    }
    return out;
  };
  const ANSWER_RE = /(^|\b)(answer|accept|pick\s*up|take\s+call)(\b|$)/i;
  const DECLINE_RE = /(decline|reject|ignore|dismiss|voicemail|block|spam|mark\s+as)/i;
  const HANG_RE = /(hang\s*up|end\s+call|leave\s+call|end\s+the\s+call|disconnect)/i;
  const answerButton = () => allButtons().find(b => {
    const n = buttonName(b);
    return ANSWER_RE.test(n) && !DECLINE_RE.test(n) && !b.disabled;
  }) || null;
  const hangupButton = () => allButtons().find(b => HANG_RE.test(buttonName(b))) || null;

  // callControlsPresent is a weaker second opinion: a call in progress offers
  // mute and a keypad whatever Google calls the control that ends it.
  //
  // It is reported separately from hangup, and deliberately so. Google Voice's
  // ordinary page offers a keypad for dialling and a mute control of its own,
  // so this matches a page with no call on it at all -- and when it was allowed
  // to mean "a call is up", FlipAi believed it was permanently in a call,
  // reported it as answered by hand, and then ignored every real ring as call
  // waiting. It may only keep a call FlipAi already knows about from being
  // declared over; it may never start one.
  const callControlsPresent = () => {
    const names = allButtons().map(buttonName).join(' | ');
    return /\bmute\b/i.test(names) && /\b(keypad|dialpad)\b/i.test(names);
  };
`

// voicePageSnapshotJS reads the call state, the caller, the visible controls
// and the audio endpoints in one round trip.
const voicePageSnapshotJS = `(async () => {` + voiceCallDOMHelpers + `
  const answerEl = answerButton();
  const hangEl = hangupButton();

  let scopeText = '';
  let node = answerEl || hangEl;
  for (let i = 0; node && i < 8; i++, node = node.parentElement) {
    const t = String(node.innerText || node.textContent || '').replace(/\u00a0/g, ' ').trim();
    if (t.length >= 5 && t.length <= 1800) {
      scopeText = t;
      const lineCount = t.split(/\r?\n/).filter(Boolean).length;
      if (lineCount >= 3) break;
    }
  }

  const normPhone = (v) => {
    const d = String(v || '').replace(/\D/g, '');
    if (d.length === 11 && d[0] === '1') return d.slice(1);
    return d.length === 10 ? d : '';
  };
  const PHONE_RE = /(?:\+?1[\s.\-]?)?(?:\([0-9]{3}\)|[0-9]{3})[\s.\-]?[0-9]{3}[\s.\-]?[0-9]{4}/;
  const FROM_RE = /(?:incoming\s+call\s+from|call\s+from|calling\s+from)\s+(.+?)\s*$/i;

  let caller = '';
  let label = '';
  const said = answerEl && (answerEl.getAttribute('aria-label') || '').match(FROM_RE);
  if (said) {
    const spoken = said[1].trim();
    const n = normPhone((spoken.match(PHONE_RE) || [''])[0]);
    if (n) caller = n; else label = spoken.slice(0, 120);
  }
  if (!caller) {
    const phoneMatch = scopeText.match(PHONE_RE);
    if (phoneMatch) caller = normPhone(phoneMatch[0]);
  }
  const UI_LINE = /^(answer|accept|decline|reject|ignore|dismiss|hang\s*up|end\s+call|leave\s+call|incoming\s+call|mute|unmute|keypad|hold|more|options|calling|google\s+voice|block|report\s+spam|send\s+to\s+voicemail|voicemail|mobile|work|home|cell|main|iphone|android|\d{1,2}:\d{2}(:\d{2})?)$/i;
  if (!label) {
    for (const raw of scopeText.split(/\r?\n/)) {
      const line = raw.replace(/\s+/g, ' ').trim();
      if (!line || line.length > 120 || UI_LINE.test(line) || PHONE_RE.test(line)) continue;
      if (/^incoming\s+call\s+from\s+/i.test(line)) {
        label = line.replace(/^incoming\s+call\s+from\s+/i, '').trim();
        break;
      }
      if (!label) label = line;
    }
  }

  let devices = [];
  try {
    if (navigator.mediaDevices && navigator.mediaDevices.enumerateDevices) {
      devices = (await navigator.mediaDevices.enumerateDevices()).map(d => ({
        kind: d.kind,
        deviceId: d.deviceId || '',
        label: d.label || ''
      })).filter(d => d.kind === 'audioinput' || d.kind === 'audiooutput');
    }
  } catch (_) {}

  const names = [];
  for (const b of allButtons()) {
    const n = buttonName(b);
    if (n && n.length <= 80 && !names.includes(n)) names.push(n);
    if (names.length >= 40) break;
  }
  const body = (() => { try { return (document.body && document.body.innerText) || ''; } catch (_) { return ''; } })();
  const signedIn = location.hostname.toLowerCase() === 'voice.google.com' && !/^\s*sign\s+in\s*$/im.test(body.slice(0, 1500));
  return {
    href: location.href,
    signedIn,
    answer: !!answerEl,
    hangup: !!hangEl,
    callControls: callControlsPresent(),
    caller,
    label,
    controls: names,
    devices
  };
})()`

// voiceClickAnswerJS presses Answer the cheap way, with the page's own click
// handler. It reports whether it found something to press, which is the
// difference between "answered" and "the card is not there yet".
const voiceClickAnswerJS = `(() => {` + voiceCallDOMHelpers + `
  const b = answerButton();
  if (!b) return false;
  try { b.focus(); } catch (_) {}
  // Google Voice's ringing card listens for pointer events on some builds and
  // for click on others. Sending the whole sequence costs nothing and covers
  // both; the element's own click() is last so a handler that stops
  // propagation on the pointer sequence still gets it.
  const opts = {bubbles: true, cancelable: true, composed: true, button: 0, buttons: 1};
  for (const type of ['pointerdown', 'mousedown', 'pointerup', 'mouseup']) {
    try {
      const Ctor = type.startsWith('pointer') && window.PointerEvent ? PointerEvent : MouseEvent;
      b.dispatchEvent(new Ctor(type, type.endsWith('up') ? Object.assign({}, opts, {buttons: 0}) : opts));
    } catch (_) {}
  }
  try { b.click(); } catch (_) { return false; }
  return true;
})()`

// voiceAnswerPointJS reports where to aim a real mouse press. Coordinates are
// in the page's own CSS pixels, which is what the browser's input pipeline
// expects, so a moved, resized or rescaled window does not change the answer.
const voiceAnswerPointJS = `(() => {` + voiceCallDOMHelpers + `
  const b = answerButton();
  if (!b) return {found: false, x: 0, y: 0};
  let rect;
  try { rect = b.getBoundingClientRect(); } catch (_) { return {found: false, x: 0, y: 0}; }
  if (!rect || rect.width <= 0 || rect.height <= 0) return {found: false, x: 0, y: 0};
  // A control inside a frame is positioned relative to that frame, so the
  // frame's own offset has to be added or the press lands somewhere else
  // entirely.
  let offsetX = 0, offsetY = 0;
  try {
    let win = (b.ownerDocument && b.ownerDocument.defaultView) || window;
    while (win && win !== window && win.frameElement) {
      const fr = win.frameElement.getBoundingClientRect();
      offsetX += fr.left;
      offsetY += fr.top;
      win = win.parent;
    }
  } catch (_) {}
  return {
    found: true,
    x: offsetX + rect.left + rect.width / 2,
    y: offsetY + rect.top + rect.height / 2
  };
})()`
