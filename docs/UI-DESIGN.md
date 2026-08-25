# FlipAi desktop UI

FlipAi is a Windows desktop app. The window is a WebView2 frame over a
loopback-only HTTP server inside the background host, so the UI ships with the
binary and never depends on a network, a CDN, or an external font.

Before v0.9 the window showed one long setup form, with a script injected at
load time that hid sections to imitate an app. v0.9 replaced that: the host
serves seven real pages, each with its own route, and the injected script no
longer touches the layout.

## Navigation

A fixed sidebar holds the whole app, in three labelled groups so seven pages
read as three short lists rather than one column:

| Group | Page | Route | What it is for |
| --- | --- | --- | --- |
| — | Home | `/` | Live state, remaining setup steps, recent activity, pause/resume |
| Bridge | Connections | `/connections` | Gmail method, credentials, subject filter, end-to-end test |
| Bridge | Agents | `/agents` | Everything Codex and Claude each own, plus the few shared defaults |
| Bridge | Phone | `/phone` | Allowed numbers, reply behaviour, SMS security code |
| Bridge | Activity | `/activity` | Filterable, paged event log; export and clear |
| App | Settings | `/settings` | Updates, startup, appearance, alerts, this install |
| App | Advanced | `/advanced` | Loopback service, log files, restart and quit |

## One home for every setting

A setting lives on exactly one page, and that page is the one it belongs to.

- **Anything an agent owns lives with that agent.** Executable path, working
  folder, SMS shortcut, permission mode, Chrome, session mode, conversation
  reset, progress cadence, and the SMS instruction are all inside the Codex or
  Claude pane. Advanced carries none of them, and links there instead.
- **The shared pane holds only what is genuinely shared**: the default agent,
  the new-conversation keyword, the turn timeout, the fallback working folder,
  and the shared SMS instruction. The keyword used to be repeated in all three
  panes; it now appears once.
- **Logs are read where logs are read.** Export and clear live on Activity;
  Advanced keeps the folder link and the last error. Settings no longer
  duplicates either.
- **Senders are configured on Phone.** Connections links to Phone rather than
  offering a second copy of the allowlist and the security code.
- **Windows startup is on Settings**, including the startup-entry repair that
  used to sit under Advanced tools.

A test asserts these boundaries: each agent field renders exactly once on the
Agents page, and not at all on any other page.

## Agents is a workbench

The Agents screen is a master/detail: a rail listing Codex, Claude, and Shared
defaults, and one pane each. The pane switch is three hidden radio inputs and
CSS sibling selectors, so choosing an agent needs no script and no round trip.
Each pane is a stack of cards — Routing & workspace, SMS instruction,
Conversation, Access, Connection — and each pane posts to the same validated
`/agents/save` handler, so the screen cannot advertise a control the bridge does
not support.

## The SMS instruction

FlipAi is a transport between a phone and the agent the user already runs, so
the prompt it builds is the user's own text inside an `<sms_command>` fence,
followed by exactly one instruction explaining that the answer travels as a text
message. That instruction is editable:

- Codex and Claude each have their own, because they answer a text differently.
- An empty box means "follow the shared instruction" rather than "send no
  framing", and the editor shows the shared wording as its placeholder.
- Clearing the shared instruction restores the wording FlipAi ships with, since
  every turn needs some framing.
- The editor shows a live character count and a preview of the exact prompt the
  agent receives, and each instruction is capped so a pasted document cannot
  ride along on every text.

## Product marks and event vocabulary

Rows and tiles that refer to a real service carry that service's mark — Google
for Gmail, Google Voice for the reply path, Codex, Claude — rather than a
generic glyph. The activity table then reads as plain English: *Incoming SMS*
(Received), *To Codex* (Delivered), *Codex command* (Completed), *Codex reply*
(Sent), and *Failed* when a step did not work. Stage and message text are ink;
only timestamps are muted.

Every `<select>` is rendered as a matching listbox rather than the native
Windows dropdown, which cannot be styled. The real `<select>` stays in the form,
so a page still submits correctly if the script never runs.

## Visual language

- One token scale for colour, space, radius, and elevation, defined once at the
  top of the stylesheet. Every control is built from it, so an input, a select
  button and a push button are never subtly different shapes or greys.
- Neutral surface, dark ink, indigo-violet accent, green healthy, amber
  attention, red failure. Light and dark palettes are both complete; the theme
  follows the Settings choice, or Windows when set to "Match Windows".
- Page rhythm: page head, a row of stat tiles, then cards — either state rows
  (label left, value right) or a form. Cards use a hairline border and a soft
  shadow that lifts on hover, not a heavy box.
- Icons are inline SVG on a shared 20×20 grid, drawn with `currentColor`.
- Motion is short and functional (menus, dropdowns, toasts) and disabled under
  `prefers-reduced-motion`.
- Compact mode tightens the whole scale — spacing, radii, control height, and
  the sidebar — rather than only the padding.
- One stylesheet and one script served from `/assets/`, both versioned by the
  build so a new release cannot be served a stale cache.

## Rules the UI follows

- **Only report verified state.** A tile says an agent is "Ready" because a test
  actually succeeded, and says "Not tested yet" otherwise. `Check.Ready()`
  requires a timestamp as well as a pass, so an untested agent can never render
  green. Dependency checks are stored in `state.json` with their timestamp.
- **Partial forms never clear settings they do not show.** Every action applies
  a mutation to the current config, so a card can save just its own fields.
- **Actions are POSTs.** Pages are GET; anything that changes state is a form
  post behind the loopback session cookie, so a link or a prefetch cannot
  trigger it.
- **The bridge keeps running.** Closing the window leaves the background host
  alive unless "Close to tray" is turned off; only Quit stops everything.
- **Nothing sensitive is rendered back.** Security codes, App Passwords, and
  tokens are write-only fields; the activity log holds statuses, not message
  text.

## Live updates

`/status.json` and `/activity.json` are polled by the page. The activity table
filters and pages on the client, so the log stays responsive without reloading.
When the host restarts after a settings change, the window shows a
"Reconnecting" notice and reloads itself once `/status.json` answers again.

## Update prompt

When the release check finds a newer version, every page shows one banner with
an Install button, and Settings carries the full picture: installed version,
latest release, when it was last checked, and what installing does. The banner
says plainly that installing keeps existing settings, because the complaint it
answers is an update that looked like a fresh install.

## Previewing the UI while working on it

`FLIPAI_PREVIEW_DIR=/some/dir go test -run TestDumpPreview .` writes every page
plus the stylesheet and script to that directory, so the redesign can be opened
in a browser on any platform. The test is skipped when the variable is unset, so
a normal `go test ./...` produces no files.
