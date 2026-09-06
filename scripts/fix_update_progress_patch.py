from pathlib import Path
p=Path('zzzzzzzz_quiet_updater_ui.go')
s=p.read_text(encoding='utf-8')
s=s.replace('title="Downloading FlipAi {{.Shell.UpdateVersion}} — {{$updateProgress}}%"','title="Downloading FlipAi {{.Shell.UpdateVersion}}"')
p.write_text(s,encoding='utf-8')
