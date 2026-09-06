from pathlib import Path

p = Path("main.go")
s = p.read_text(encoding="utf-8")
old = '''\tcase "--host":\n\t\trunHost(dataDir, cfgPath, statePath, tokenPath)'''
new = '''\tcase "--host":\n\t\tif maybeInstallStagedUpdateAtStartup(statePath) {\n\t\t\treturn\n\t\t}\n\t\trunHost(dataDir, cfgPath, statePath, tokenPath)'''
if old not in s:
    raise SystemExit("--host startup block not found")
p.write_text(s.replace(old, new, 1), encoding="utf-8")
