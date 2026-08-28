# FlipAi v0.38.0

The one-click audio-bridge installer could never have worked. This release
removes it and replaces it with a path that does.

## "Windows rejected a virtual audio device (problem code 52)"

FlipAi downloaded a free virtual audio driver, verified its signature, put it in
the Windows driver store, created the device node — and then Windows refused to
start it.

The reason is not fixable by trying harder. On 64-bit Windows 10 and 11, the
Code Integrity engine loads a **kernel-mode** driver only when its catalog is
signed by the **Microsoft Windows Hardware Compatibility Publisher** — the
signature a vendor gets by submitting the driver to Microsoft. The driver FlipAi
used is signed by SignPath Foundation through GlobalSign. That is a perfectly
valid Authenticode code-signing certificate, and it is irrelevant to loading a
driver. The driver project's own README says so plainly: it *"requires test
signing to be enabled"*.

So FlipAi's signature check passed and told the truth — the signature **is**
valid — while asking Windows for something it was always going to refuse.
Problem code 52 is `CM_PROB_UNSIGNED_DRIVER`, and it was correct every time.

The only ways past it are switching on Windows test-signing or turning off
Secure Boot. FlipAi will not do either to somebody's PC to make a button look
like it works.

## What replaces it

Two free virtual audio pairs that **are** signed the way Windows requires, so
they install on a stock Windows 11 with Secure Boot left on:

- **VB-CABLE Virtual Audio Device** — carries the caller's voice to the desktop app
- **VoiceMeeter** — carries the app's reply back to the caller

The button on the cables row now opens whichever of the two the PC is still
missing, and it says *Set up*, not *Install*, because FlipAi is not the one
installing it. Install, restart, come back: **FlipAi finds the endpoints and
wires both directions itself, on every call, exactly as before.** Nothing about
the per-call behaviour changes — that part always worked and still does.

FlipAi already recognised both of these, along with CABLE A/B, VB-Audio Point
and the VoiceMeeter AUX and VAIO3 strips. Anyone who already has cables
installed is unaffected.

## Also gone

The driver download, the SHA-256 pins, the device-node tool that existed only to
install that driver, the elevated PowerShell install script, and the installer
log summariser that existed only to explain why it failed. About 350 lines,
whose only job was to fail.

## Verified

A test walks every non-test source file and fails the build if the driver
package, the device-node tool, `testsigning` or `bcdedit` are ever referenced
again outside the one file that explains why they cannot be used.

Another test installs — in the cable planner — exactly what FlipAi now
recommends, and requires the result to be a complete, working bridge with the
PC's own speakers and microphone left out of it. A recommendation FlipAi cannot
then wire is the failure this release is about, and it is now impossible to ship
one.

Plus the usual: the full call lifecycle, the injected page script and the
control channel in real headless Chromium, the cable plan, the Windows build and
race test, the receiver check on a real Windows runner, and the installer's
install and uninstall.

Still only verifiable on your own PC: whether a real call is answered end to end
and whether the desktop app enters voice mode.
