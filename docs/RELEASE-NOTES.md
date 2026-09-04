# FlipAi v0.46.26

Updates now behave like a quiet desktop-app updater instead of a Settings workflow.

## Updates

- Update controls are completely removed from Settings.
- FlipAi checks GitHub every five minutes and automatically downloads and verifies a newer installer in the background.
- While the installer is downloading, only a small download indicator appears beside the version in the left sidebar.
- Once the verified installer is ready, that indicator becomes the install button. Clicking it starts the update without navigating to an update-success or update-failure page.
- Page-wide update banners and download-complete popups are removed.
- Installation remains a deliberate user click; only the download is automatic.

## Restart reliability

The updater now restores the watchdog/background host before reopening the normal FlipAi window, and gives stale FlipAi process trees more time to release after forced shutdown. This reduces the post-update race that could leave FlipAi installed but not running until its processes were ended manually in Task Manager.
