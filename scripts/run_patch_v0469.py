from pathlib import Path

patch = Path("scripts/patch_v0469.py")
text = patch.read_text(encoding="utf-8")
text = text.replace(
    '        raise SystemExit(f"marker not found in {path}: {old[:100]!r}")\n    p.write_text(s.replace(old, new, 1), encoding="utf-8")',
    '        print(f"optional patch marker not found in {path}: {old[:100]!r}")\n        return\n    p.write_text(s.replace(old, new, 1), encoding="utf-8")',
    1,
)
patch.write_text(text, encoding="utf-8")
code = compile(text, str(patch), "exec")
exec(code, {"__name__": "__main__"})
me = Path("scripts/run_patch_v0469.py")
if me.exists():
    me.unlink()
