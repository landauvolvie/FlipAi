# FlipAi v0.33.0

Google Voice is now part of FlipAi rather than a browser FlipAi drives, and one
state machine owns a call from the first ring to the teardown after the caller
hangs up.

## What was wrong

Incoming calls reached FlipAi and still did not become conversations.

- The receiver launched **Microsoft Edge as a separate application** and then
  moved its windows around. Edge appeared on the desktop, and a relaunch that
  found the profile already held handed off to the running browser and opened a
  **second Edge window** instead of a new one.
- The Connections panel refused to show Google Voice at all unless the whole
  panel fitted in the viewport, so on a smaller FlipAi window, at a higher
  display scale, or with the page scrolled by a few pixels, **Google Voice was
  simply not there** — and the panel looked exactly the same as while it was
  starting up.
- Between the window process, a second watchdog loop and the page's own "make
  sure it is running", **three supervisors** could each decide nothing was
  running and start another one.
- Three separate guards each pressed **Answer** and each started the desktop
  app's **voice mode** on their own timer, and any of them could mark a call
  connected on its own. A call could read "connected" with nothing audible
  behind it.
- Google Voice was kept **minimized**, which Chromium is entitled to treat as
  hidden: backgrounded renderer, timers slowed to once a minute, far longer
  than a call rings for.

## What changed

**Google Voice lives inside FlipAi.** It runs in FlipAi's own Edge WebView2
view — the same component the FlipAi window is drawn with — created and owned by
FlipAi. No external browser is started anywhere in the product. The window has
exactly two states, standing inside the FlipAi panel or parked past every
display, and no path that gives it a title bar, a taskbar button or an Alt-Tab
entry. Parking replaces minimizing so the page stays live and a ring is still
noticed.

**The panel is clipped rather than refused.** Google Voice is docked to the part
of the reserved panel that is really on screen, so scrolling or a smaller window
moves it instead of making it vanish.

**One state machine owns the call.** Ring, authorize, answer, wire the audio,
start the desktop voice session, hang up, tear down, next call. Everything that
can see the page — the injected script and FlipAi's loopback control channel —
reports to it, and neither decides anything.

**An allowed caller is not lost to voicemail.** Answer is pressed for the whole
~25-second ring, escalating through the page's own click, a real pointer press
delivered through the browser's input pipeline, and the Windows accessibility
Invoke a screen reader would use.

**The Codex desktop voice session is checked.** FlipAi finds or launches the
desktop app, points its microphone and speaker at the virtual cables *before*
anything opens a stream, ends any leftover session so every call is a fresh
conversation, starts voice mode, and confirms through the app's accessibility
tree that it really started. A call is reported as a working conversation only
once that is true; otherwise the status says which controls the app offered.

**The status tells the truth.** Ringing, answered-but-not-yet-talking, and a
live conversation are now three different things instead of one "connected".

Also in this release: a single supervisor with backoff so a failing view cannot
spawn windows in a loop; the Google Voice MMS sender driven from the recorded
control port instead of guessing at listeners; and the retired "open in its own
window" path removed.

## Verified

In CI, on Windows: Google Voice comes up inside FlipAi's own WebView2 view with
no Microsoft Edge application started, exactly one window however many times it
is asked for, no taskbar button, and its loopback control channel answering.
Plus the call state machine end to end, the injected page script driving a
stand-in Google Voice page in real Chromium with real device selection, the
audio-path invariants, the voice-mode report parsing, the Windows build, and the
installer's install and uninstall.

Not verified by CI, and only verifiable on your own PC: that a call to your
number rings in this browser (Google Voice's own **Settings → Calls → Receive
calls on this device** has to be on), that the Codex desktop app on your machine
exposes a Voice control FlipAi can press, and that real audio flows over real
virtual cables in both directions. A green CI run is not a working phone call.
