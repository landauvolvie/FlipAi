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
can see the page — the injected script, and FlipAi reading the page itself —
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

**The second view of the page opens no port.** FlipAi reads its own Google
Voice page, and delivers a real pointer press to a ringing call, through
WebView2's in-process DevTools call. An earlier attempt asked WebView2 for a
loopback debugging port, which the runtime ignores -- so that channel silently
did not exist, and with it the second way of pressing Answer and the ability to
send an image over Google Voice. Nothing listens for it now. The Google Voice
process serves exactly one loopback endpoint, holding a token it generates
itself, so the FlipAi host can ask it to send an image.

Also in this release: the view is created already parked, as a tool window,
without focus, so not even an empty frame flashes on the desktop while WebView2
starts; the Notifications API is supplied to the view when the runtime lacks it,
and the two permissions FlipAi really grants are reported as granted, because
Google Voice may otherwise decline to ring in a browser at all; a live call
cannot be ended by a moment of blindness, since a call in progress always offers
mute and a keypad whatever the control that ends it is called; a single
supervisor with backoff so a failing view cannot spawn windows in a loop; and
the retired "open in its own window" path removed.

## Verified

In CI, on a real Windows runner: Google Voice comes up inside FlipAi's own
WebView2 view with no Microsoft Edge application started; exactly one window
however many times it is asked for; no taskbar button and no Alt-Tab entry;
FlipAi's own endpoint answering with its token and refusing without it; and a
real DevTools call reaching the page in-process, which is what the second way of
pressing Answer and sending an image both depend on.

Also in CI: the call state machine end to end -- an allowed caller answered at
once and kept being answered for the whole ring, an unauthorized caller never
touched, a call answered by hand, one dropped frame not ending a live call, a
renamed hang-up control not ending one either, exactly one voice session per
call and one teardown, a failed voice session never described as a working
conversation, and the next call starting fresh. Plus the injected page script
driving a stand-in Google Voice page in real Chromium with real device
selection, the audio-path invariants, the voice-mode report parsing and its
failure messages, the Windows build, and the installer's install and uninstall.

Not verified by CI, and only verifiable on your own PC:

- that a call to your number rings in this browser at all — Google Voice's own
  **Settings → Calls → Receive calls on this device** has to be on, and Google
  decides which browsers it will offer that to;
- that the ringing card Google renders today is the card FlipAi recognizes;
- that the Codex desktop app on your machine exposes a Voice control FlipAi can
  press, and that pressing it starts a conversation;
- that real audio flows over real virtual cables in both directions;
- that the whole cycle repeats on your line.

**A green CI run is not a working phone call.** What CI can hold is the
mechanism; the Connections status rows are written to say which step failed when
one does.
