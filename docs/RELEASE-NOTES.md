# FlipAi v0.39.0

After a restart, Google Voice would not start ("no interactive desktop
session"), Retry did nothing, and Set up opened no browser. One cause sat under
all three, and this release fixes it.

## Google Voice, Retry, and Set up all did nothing

They share a root: **"Start before sign-in."**

That option runs FlipAi from a power-on scheduled task, before anyone logs in,
so FlipAi is already handling texts after a reboot. But a power-on task runs in
**Windows Session 0 — a session with no desktop.** And everything about calling
needs a desktop: the Google Voice window is a real browser view, the desktop AI
app is a real window, and Set up opens a real browser.

FlipAi's background host — the part that owned Google Voice and the audio-bridge
setup — was the process that power-on task started, so it sat in Session 0. From
there:

- **Google Voice reported "no interactive desktop session" and never started.**
  It was right: it cannot draw a window where there is no desktop.
- **Retry did nothing**, because Retry asked that same Session 0 host.
- **Set up opened a browser into Session 0**, where no signed-in user could see
  it — so, from the desktop, nothing happened.

Signing in did not rescue it. When you signed in and opened FlipAi, your own
session found the Session 0 host already answering and never started one of its
own, so the only host stayed where it could not help.

## The fix: the desktop work runs in your session, not Session 0

A tray icon cannot exist without a desktop, so the FlipAi **tray** is always in
your signed-in session. Google Voice supervision and the audio-bridge setup
endpoint now live there. The background host, wherever it is running, hands any
desktop action it cannot perform itself to that interactive tray.

So with **Start before sign-in on**, texts are still handled from the moment the
PC powers on, exactly as before — and the calling window, Retry, and Set up all
work as soon as you have signed in and opened FlipAi. With the option off,
nothing changes: everything was already in your session.

**One thing to know:** the calling side is inherently a signed-in-desktop
feature. It answers calls whenever you are logged in with FlipAi open; it cannot
answer them at the Windows lock screen, because Google Voice and the desktop AI
app have nowhere to run there.

## What a real call now reports

The call in the last report **was answered** — it did not go to voicemail. The
caller-ID fix from v0.36.0 is holding. The "Last call" record shows the call
connecting and then stopping at the next step: *"could not find a Voice control
in ChatGPT."* That is Codex/ChatGPT's own voice control not being found
automatically. FlipAi already tells you the fix and has the setting for it:
**Agents → the agent → Voice shortcut**, set to the desktop app's own
push-to-talk / start-voice keyboard shortcut (for ChatGPT desktop, the Voice
Mode shortcut). FlipAi presses that to start voice mode when it cannot find the
on-screen control.

## Verified

The hand-off is tested: an interactive host opens Google Voice directly; a
non-interactive one hands the work to the worker and reports back the worker's
own outcome — success, or the real reason it failed — rather than a generic
error; and a request left pending while nobody was signed in is dropped, never
replayed on the next sign-in.

Plus the usual: the full call lifecycle, the injected page script and the
control channel in real headless Chromium, the cable plan, the audio-bridge
setup, the Windows build and race test, the receiver check on a real Windows
runner, and the installer's install and uninstall.

Still only verifiable on your own PC: whether a real call is answered end to end
and whether the desktop app enters voice mode.
