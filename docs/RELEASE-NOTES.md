# FlipAi v0.42.0

Two things reported from a real call: it answers, the desktop app opens, and
then it sits on "Answered — starting voice" with no clue why; and the desktop
app audio routing fails with "Windows refused it". This release makes the first
one legible and pins down the second.

## "Answered — starting voice", and nothing else

When voice mode would not start, there was nothing on screen to say whether
FlipAi could even read the app, what controls it found, or whether it pressed
one. The "Last call" trace showed only "Connected". Guessing at a fix from that
is exactly what the last few releases were doing.

FlipAi now writes down what its accessibility scan of the desktop app actually
saw, on every attempt, and shows it on Connections as **"Desktop app voice:"**:

- whether the app window was **readable through Windows accessibility** at all;
- the **list of controls** the app exposed — the single most useful line, because
  it shows whether the voice control is simply named something FlipAi did not
  expect;
- which control FlipAi **matched** as the one that starts voice, if any;
- and what **pressing it reported**.

This is written straight to the status the page reads, so it updates during the
attempt rather than only at the end — a call is no longer a dead end with no
clue. It is also what makes the next fix precise instead of another guess: the
line names exactly what the app offered.

## "Windows refused it" — HRESULT 0x80070057

The routing failure is E_INVALIDARG. FlipAi's interop for the per-app audio API
matches EarTrumpet's known-good definition exactly (same 19-method layout, same
device-id form), and Windows' own Settings app can still set the same thing — so
the likely cause is that this API grew more methods on a newer Windows build,
moving the call FlipAi makes to a different vtable slot.

FlipAi now proves which it is: on a routing failure it does a **read-back at the
same slot** (a call that changes nothing) and puts the result in the message. If
the read-back also fails, the slot moved; if it succeeds, the device id is what
was rejected. That single fact decides the real fix safely, without FlipAi
poking at vtable slots blindly on your PC.

Meanwhile the routing message now names the exact one-time manual step that
always works, because Windows' own Settings does not rely on that undocumented
call: **Settings → System → Sound → Volume mixer**, find the app, set its Output
and Input to the two named cables. It sticks.

## Verified

The desktop-voice observation is tested to survive into the status the page
reads, with the session token still stripped. The router is tested to try both
device-id forms and to read back at the same slot on failure so the reason is
diagnosable.

Plus the usual: the full call lifecycle, the injected page script and control
channel in real headless Chromium, the cable plan, the audio-bridge setup, the
desktop-session hand-off, the Windows build and race test, the receiver check on
a real Windows runner, and the installer.

Honest about the limit: the accessibility scan and the audio interface both
behave in ways only your specific Windows build and desktop-app version reveal.
This release is built to report exactly what they do, so the next change is aimed
rather than guessed.
