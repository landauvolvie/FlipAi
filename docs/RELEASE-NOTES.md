# FlipAi v0.35.0

This release fixes a bug introduced in v0.33.0 that stopped incoming calls being
answered at all. If you are on v0.33.0 or v0.34.0, update.

## What was wrong

Google Voice's ordinary page offers a keypad to dial with and a mute control of
its own. v0.33.0 added a second, weaker way of recognising a call in progress —
"a call always offers mute and a keypad" — as protection against Google renaming
the control that ends a call. That signal matched the page with **no call on it
at all**.

The consequences ran in order:

- FlipAi decided it was in a call with a caller it could not identify, and
  reported **"Answered by hand — not bridged"** for a call nobody had answered.
- From then on it treated every real incoming call as call waiting and ignored
  it, so **no call was ever answered again** and allowed callers rang out to
  voicemail.
- Nothing recovered from that state on its own, because a ringing page never
  reached the code that ends a call.

## What changed

**That signal can no longer start a call.** It may only keep a call FlipAi
already knows about from being declared over — which is what it was for. The
control that ends a call is once again the only thing that may say a call has
started.

**A ring clears a stale call immediately.** However FlipAi comes to believe a
call is up when one is not, an Answer control with no hang-up control beside it
corrects it on the spot rather than after a debounce. Waiting out a debounce
during a ring is waiting out the ring. Call waiting is unaffected: a second call
ringing during a live one still shows the control that ends the live one.

**FlipAi recognises the cables it installs itself.** The built-in audio bridge
was matched by endpoint names carrying a specific vendor suffix. Where the
driver names its endpoints differently, the cables installed correctly and the
status still said **Not found** — driver present, call still silent. The match
no longer depends on that suffix.

**The install is offered where the failure is reported.** "Virtual audio cables:
Not found" now carries its own **Install** button, instead of only a callout
further down the page that has to be found first. A machine with one cable is
offered the second.

**"Desktop app audio: Waiting" no longer appears when there is no cable.** That
outcome was inferred from the wording of a status note, so a PC with nothing to
route to was told the desktop app was being waited for — sending the user to
look at the app, which was not the problem. Each outcome now says which one it
is: applied, no cable to route to, waiting for the desktop app, or Windows
refused it. The missing cable is reported once, by the row that can fix it.

**A ring with nothing to press says so.** If Google Voice announces an incoming
call and never draws an Answer control in the page, that is now stated plainly
rather than looking like FlipAi doing nothing.

## Verified

The exact field failure is now a test: an idle Google Voice page never starts a
call, and a ring after a stale call state is answered. Against the shipped
v0.34.0 logic that test fails — the ring is never answered. The browser harness
also renames the control that ends a call *during* a live conversation and
requires the call to survive, which is the protection the bad signal was added
for in the first place.

Plus the usual: the full call lifecycle, the injected page script in real
Chromium with real device selection, the cable plan under three different
endpoint namings, the Windows build, the receiver check on a real Windows
runner, and the installer's install and uninstall.

Still only verifiable on your own PC: that a call to your number rings in this
browser, that your Codex desktop app exposes a Voice control FlipAi can press,
and that audio flows over the cables in both directions.
