# FlipAi v0.46.28

Update discovery is now much faster without hammering GitHub's release API.

## Updates

- FlipAi checks for a newer version every 30 seconds instead of every five minutes.
- The frequent check reads only the tiny `VERSION` file from GitHub's raw CDN.
- The GitHub release API is queried only when that marker reports a version newer than the running app.
- Once release metadata is already known, download retries reuse it instead of repeatedly calling the release API.
- Automatic background download, checksum verification, sidebar-only install control, and user-triggered installation remain unchanged.

## Reliability

- Added regression coverage proving a current VERSION marker does not call the release API and a newer marker does.
