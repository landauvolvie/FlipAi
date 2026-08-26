from pathlib import Path

old = """          function Wait-VoiceWindow([int]$seconds) {
            $deadline = (Get-Date).AddSeconds($seconds)
            do {
              $p = @(Get-Process -Name FlipAi -ErrorAction SilentlyContinue |
                     Where-Object { $_.MainWindowTitle -like '*Google Voice*' })
              if ($p.Count -gt 0) { return $p[0] }
              Start-Sleep -Milliseconds 500
            } while ((Get-Date) -lt $deadline)
            return $null
          }
"""
new = """          function Wait-VoiceWindow([int]$seconds) {
            $deadline = (Get-Date).AddSeconds($seconds)
            do {
              # Google Voice now runs in Microsoft Edge app mode so browser
              # push can wake incoming calls. Older builds used a FlipAi-owned
              # window. Accept either process while still requiring FlipAi's
              # stable Google Voice title.
              $p = @(Get-Process -Name FlipAi,msedge -ErrorAction SilentlyContinue |
                     Where-Object { $_.MainWindowTitle -like '*Google Voice*' })
              if ($p.Count -gt 0) { return $p[0] }
              Start-Sleep -Milliseconds 500
            } while ((Get-Date) -lt $deadline)
            return $null
          }
"""

for name in ('.github/workflows/build.yml', '.github/workflows/release.yml'):
    p = Path(name)
    text = p.read_text()
    count = text.count(old)
    if count != 1:
        raise SystemExit(f'{name}: expected one stale block, got {count}')
    p.write_text(text.replace(old, new, 1))

Path('.github/workflows/patch-google-voice-edge-ci.yml').unlink(missing_ok=True)
Path('scripts/patch_edge_ci.py').unlink(missing_ok=True)
