# FlipAi v0.36.0

Two things reported from a real PC: the free audio bridge would not install, and
a call that reached FlipAi still went to voicemail with nothing on screen to say
why.

## The audio bridge could not install

The installer drove **nefcon** with **devcon**'s command line
(`nefconc install <inf> <hardware-id>`). nefcon does not accept that, so the
very first step failed and nothing was ever installed.

It now uses the documented commands: Windows' own `pnputil /add-driver <inf>
/install` puts the verified package in the driver store, and nefcon is used only
for the one thing Windows has no built-in command for — creating a root device
node for a driver with no hardware behind it. Each step logs its exit code.

**And the error message showed the wrong thing.** The installer runs under
`Start-Transcript`, whose header — the machine name, the account, the PowerShell
build numbers — is longer than the message the user is shown, so the failure was
truncated away before the sentence that said what went wrong. The message now
carries the error the installer actually raised, and the path to the full log.

## A call reached FlipAi and still went to voicemail

Every field describing a call was cleared the moment it returned to idle. So a
call that was refused, one FlipAi could not manage to answer, and one that never
rang at all **all left the same screen**: "Current call: Idle", and nothing else.
There was no way to tell which had happened, on a page whose whole job is to say.

There is now a **Last call** row that outlives the call, carrying:

- what happened to it, in a sentence — refused and why, answered and bridged, or
  answered with the desktop voice session failing to start;
- **what FlipAi actually tried, in order** — each rung of the answer ladder it
  used and what that rung reported back, then starting the desktop voice
  session, then ending it.

So a call that is not answered now says whether the caller was not on any
agent's list, whether Google Voice drew nothing FlipAi could press, or whether
it pressed and the call did not connect. Each call starts its own record.

Also in this release: "Virtual audio cables: Not found" was still being repeated
by the desktop-app routing row as if it were a second, unrelated problem.

## Verified

The installer-log summariser is tested against the exact transcript from the
failing machine and must report the raised error rather than any of the header.
The call record is tested three ways: a refused call leaves a reason that
survives the call ending, an allowed call records each answer attempt and what
it reported, and a second call never inherits the first call's record.

Plus the usual: the full call lifecycle, the injected page script in real
Chromium, the cable plan, the Windows build, the receiver check on a real
Windows runner, and the installer's install and uninstall.

Still only verifiable on your own PC: whether the driver installs on that
machine, and whether a real call is answered end to end.
