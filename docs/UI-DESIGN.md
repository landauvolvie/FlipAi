# FlipAi desktop UI

FlipAi is a Windows desktop app. The window is a WebView2 frame over a
loopback-only HTTP server inside the background host, so the UI ships with the
binary and never depends on a network, a CDN, or an external font.

Before v0.9 the window showed one long setup form, with a script injected at
load time that hid sections to imitate an app. v0.9 replaced that: the host
serves seven real pages, each with its own route, and the injected script no
longer touches the layout.

## Navigation

A fixed sidebar holds the whole app:

| Page | Route | What it is for |
| --- | --- | --- |
| Home | `/` | Live state, remaining setup steps, recent activity, pause/resume |
| Connections | `/connections` | Gmail method, credentials, sender filtering, message-flow test |
| Agents | `/agents` | Codex and Claude paths, working folders, tests, routing behaviour |
| Phone | `/phone` | Allowed numbers, reply behaviour, SMS security code |
| Activity | `/activity` | Filterable, paged event log with durations |
| Settings | `/settings` | Startup, appearance, in-window alerts, diagnostics |
| Advanced | `/advanced` | Executable paths, local service, logs, restart and quit |

The sidebar footer shows whether the bridge is running, paused, or idle, plus
the installed version. Settings and Advanced sit below a divider because they
are configuration rather than operation.

## Visual language

- Neutral surface, dark ink, violet accent, green healthy, amber attention, red
  failure. Light and dark palettes are both defined; the theme follows the
  Settings choice, or Windows when set to "Match Windows".
- One stylesheet and one script served from `/assets/`, both versioned by the
  build so a new release cannot be served a stale cache.
- Status tiles across the top of each page, then cards: either state rows
  (label on the left, value on the right) or a form.
- Icons are inline SVG on a shared 20×20 grid, drawn with `currentColor`.
- Compact mode tightens spacing for smaller windows.

## Rules the UI follows

- **Only report verified state.** A tile says an agent is "Ready" because a test
  actually succeeded, and says "Not tested yet" otherwise. Dependency checks are
  stored in `state.json` with their timestamp.
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
