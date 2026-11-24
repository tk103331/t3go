#!/usr/bin/env python3
# Generate internal/app/icons/lucide/lucide.go from the icon bodies kept
# in internal/app/icons/lucide/icons.json.
#
# The icons are Lucide (ISC, https://lucide.dev). They came with the
# NSGo package (Apache-2.0) and are kept in this repository, so the
# client draws them without that package.
#
#   ./scripts/gen-lucide-mygo.py
import json
import os
import re
import subprocess
import sys

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
LUCIDE = os.path.join(ROOT, "internal", "app", "icons", "lucide")

# Icons renamed or removed in newer Lucide sets: map the client's name
# to the name the icon file keeps.
ALIASES = {"Columns": "Columns2"}


def escape(text: str) -> str:
    out = []
    for ch in text:
        if ch == "\\":
            out.append("\\\\")
        elif ch == '"':
            out.append('\\"')
        elif ch == "\n":
            out.append("\\n")
        elif ch == "\t":
            out.append("\\t")
        elif ch == "\r":
            out.append("\\r")
        else:
            out.append(ch)
    return "".join(out)


def main() -> int:
    with open(os.path.join(LUCIDE, "icons.json"), encoding="utf-8") as handle:
        icons = json.load(handle)

    used = subprocess.run(
        ["grep", "-rho", "lucide\\.[A-Z][A-Za-z]*", "internal/app", "cmd", "--include=*.go"],
        capture_output=True, text=True, cwd=ROOT).stdout.split()
    names = sorted({ref.removeprefix("lucide.") for ref in used})

    lines = [
        "// Package lucide carries the Lucide icons the client draws, as MyGo",
        "// SVGs. The icons are Lucide (ISC), https://lucide.dev.",
        "package lucide",
        "",
        "import (",
        '\t"sync"',
        "",
        '\t"github.com/egoist/mygo/ui"',
        ")",
        "",
    ]
    count = 0
    for name in names:
        source = icons.get(ALIASES.get(name, name))
        if source is None:
            print(f"MISSING: {name}", file=sys.stderr)
            continue
        if count > 0:
            lines.append("")
        count += 1
        words = re.sub(r'([a-z0-9])([A-Z])', r'\1 \2', name)
        lines.append(f"var once{name} sync.Once")
        lines.append("")
        lines.append(f"var svg{name} *ui.SVG")
        lines.append("")
        lines.append(f"// {name} returns the Lucide {words.lower()} icon.")
        lines.append(f"func {name}() *ui.SVG {{")
        lines.append(f'\tonce{name}.Do(func() {{ svg{name} = ui.MustParseSVG([]byte("{escape(source)}")) }})')
        lines.append(f"\treturn svg{name}")
        lines.append("}")

    with open(os.path.join(LUCIDE, "lucide.go"), "w", encoding="utf-8") as handle:
        handle.write("\n".join(lines) + "\n")
    subprocess.run(["gofmt", "-w", os.path.join(LUCIDE, "lucide.go")], check=True)
    print(f"wrote {count} icons")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
