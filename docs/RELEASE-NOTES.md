# FlipAi v0.43.0

The diagnostic in v0.42.0 did its job. The "Desktop app voice:" line from a real
call read: **"Claude offered [Claude, Minimize, Maximize, Close]. FlipAi found no
control it recognized as voice."** That is the whole answer, and this release
acts on it.

## Why voice mode would not start

Two faults, both now fixed.

**The scan gave up before the app's content appeared.** These desktop apps are
Chromium/Electron, and Chromium does not expose its web content to Windows
accessibility until a client keeps asking — with it off, a scan sees only the
window frame: the title and the Minimize/Maximize/Close buttons. FlipAi's wait
loop was meant to hold on until the content built, but it stopped the moment the
tree had "more than a few" elements — and the window frame alone has more than a
few. So it bailed on the first scan, every time, and never gave Chromium the
second or two it needs. The loop now waits for an actual voice control (or a
running session) to appear, or a bounded twelve seconds, and nothing less.

**It could press the wrong thing.** An earlier trace showed FlipAi pressing
*"Voice Chat Topic Summary"* — a conversation title in the sidebar, not a
button. The matcher now only ever takes a **clickable** control: list items,
text, document and tree nodes are excluded, so a conversation whose name happens
to contain "voice chat" can never be mistaken for the control that starts one.

**And it now asks the app to expose itself.** When FlipAi launches the desktop
app, it starts it with Chromium's accessibility forced on, so the voice control
is there to be found. An app you already had open is untouched — which is why,
if the scan still sees only the window frame, FlipAi now says so in plain words
and tells you to **quit the app completely (system tray included) and let FlipAi
reopen it**, so it starts with accessibility on.

## Audio routing

Unchanged in mechanism, still diagnosed: the read-back probe from v0.42.0 tells
apart a moved interface slot from a rejected device id. The one-time manual step
in the message works today. Set the app's Output and Input to the two cables in
**Settings → System → Sound → Volume mixer** and it sticks.

## Also

The browser test harness now allows more time for a scenario to answer on a slow
CI runner, so releases stop stalling on a timing flake rather than a real
failure.

## Verified

The driver script is tested to wait for a real voice control rather than bail on
window chrome, and to take only clickable controls. The accessibility-off
signature — a scan returning only the window frame — is tested to be diagnosed
specifically, apart from every other reason a voice control is missing, so the
"reopen the app" fix is the one surfaced.

Plus the usual: the full call lifecycle, the injected page script and control
channel in real headless Chromium, the cable plan, the audio-bridge setup, the
desktop-session hand-off, the Windows build and race test, the receiver check on
a real Windows runner, and the installer.
