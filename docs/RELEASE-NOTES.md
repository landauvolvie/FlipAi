# FlipAi v0.40.0

The call is answered, the account is signed in, the cables are installed and
wired — and then the caller could not talk to the agent, because Codex/ChatGPT's
voice mode never started. This release goes after that, and after the audio
routing Windows was refusing alongside it.

## "Could not find a Voice control in ChatGPT"

The Codex and ChatGPT desktop apps are Chromium/Electron apps, and Chromium does
not build its accessibility tree — the thing FlipAi reads to find and press the
Voice control — until a client attaches, and **not instantly**. FlipAi drove it
by launching a fresh PowerShell for each check: a brand-new accessibility client
that asked once and exited. The first query after attaching sees only the
top-level window, so every check came back with the window and nothing inside
it. That is exactly the *"it offered: ChatGPT"* in the last-call record.

Two changes fix the mechanism:

- **FlipAi now keeps one accessibility client alive and re-scans** until the
  app's web content actually appears, instead of asking once and giving up. A
  scan that returns only the bare window is treated as a tree that has not built
  yet, and it waits — up to a bounded few seconds — for the real controls.
- **The app is brought to the front first.** A minimized or fully hidden
  Chromium window throttles its renderer and may not build an accessibility tree
  for its web content at all, so the Voice control would not exist to be found.
  The caller is not typing, so focusing the app they are calling is right.

If your app still exposes no Voice control this way, the keyboard fallback is
unchanged: **Agents → the agent → Voice shortcut**, set to the desktop app's own
start-voice shortcut. FlipAi presses it when it cannot find the on-screen
control.

## "Desktop app audio: Windows refused it"

Pointing the desktop app's microphone and speaker at the cables uses a
Windows interface whose accepted device-id format **differs across Windows
builds** — some take the raw endpoint id, others a device-path wrapper around
it. FlipAi sent one form and, when Windows rejected it, reported "Windows
refused it". It now tries both forms and takes whichever Windows accepts,
failing only when neither works — and still printing the exact HRESULT and the
one-time manual fallback when that happens.

## Verified

The driver script is tested to keep one client and re-scan rather than query
once, to treat a bare window as a not-yet-built tree, to bound the wait, and to
still emit every report field the rest of FlipAi parses. The router is tested to
try the SWD wrapper and the raw id, in that order, for both the playback and the
recording endpoint.

Plus the usual: the full call lifecycle, the injected page script and the
control channel in real headless Chromium, the cable plan, the audio-bridge
setup, the desktop-session hand-off, the Windows build and race test, the
receiver check on a real Windows runner, and the installer.

Honest about the limit: whether a specific build of the ChatGPT or Codex desktop
app exposes a Voice control to Windows accessibility, and whether that build's
audio endpoints take one id form or the other, can only be confirmed on your PC.
The last-call record now says which step it reached, so if voice still does not
start it will say whether the control was found, pressed, or never appeared.
