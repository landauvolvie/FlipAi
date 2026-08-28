# FlipAi v0.41.0

The call is answered and the desktop app opens — but voice mode never starts, so
there is no one to talk to. This release goes at that, having found what the
voice actually is and how it has to be started.

## What the voice is, and why it would not start

Two things were wrong, and both come from the same fact: **the voice a caller
talks to is ChatGPT Voice (GPT‑Live), which OpenAI ships in the ChatGPT desktop
app — and from there it can drive Codex.** It is started by clicking **"Start new
voice chat"**, and there is no keyboard shortcut for it. The standalone Codex
app has only a separate, less reliable dictation path.

- **FlipAi was opening the wrong app.** It launched the standalone Codex app
  when both are installed, then looked for a voice control that app does not
  reliably have. FlipAi now prefers the **ChatGPT desktop app** — the one that
  carries the voice and drives Codex — for launching, the Start Menu shortcut,
  and the window it drives. A machine that only has the Codex app still falls
  back to it, and a title you set yourself still wins.

- **It pressed the control in a way Electron ignores.** These apps are
  Chromium/Electron, and their custom buttons routinely ignore the accessibility
  "invoke" FlipAi was using — it reports success while nothing happens, which is
  the "pressed but did not enter voice mode" a call got stuck on. FlipAi now
  presses the control with a **real synthesized mouse click** on the control
  itself, the way a person does, and falls back to the accessibility methods
  only when the control has no usable on‑screen rectangle.

FlipAi also now recognizes the control by what the app really calls it —
**"Start new voice chat"** and its headphone/headset icon — not only the older
"voice mode" wording.

## If it still does not start

Voice is a ChatGPT‑app feature and needs a Plus, Pro, Business, Edu or Enterprise
plan, with the ChatGPT desktop app installed and signed in. The failure message
now says so, and names the "Start new voice chat" control it looked for and what
the app offered instead — so a missing app, a signed‑out app, or a plan without
voice is legible rather than a silent "could not start voice".

## Verified

The driver is tested to press the control with a real click, and to try that
before the accessibility invoke; to recognize "Start new voice chat" and the
headphone icon; to keep one accessibility client alive and wait for the Electron
tree to build; and to prefer ChatGPT over the standalone Codex app for the
window, the shortcut and the launch path alike.

Plus the usual: the full call lifecycle, the injected page script and control
channel in real headless Chromium, the cable plan, the audio‑bridge setup, the
desktop‑session hand‑off, the Windows build and race test, the receiver check on
a real Windows runner, and the installer.

Honest about the limit: which control a given build of the ChatGPT desktop app
exposes, and whether the account has a voice‑capable plan, can only be confirmed
on your PC. The last‑call record says which step it reached — found, pressed, or
never appeared — so the next thing to change, if any, is named rather than
guessed.
