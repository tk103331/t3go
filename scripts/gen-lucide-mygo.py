#!/usr/bin/env python3
# Generate internal/app/icons/lucide from the NSGo lucide icon set
# (github.com/oneo-me/nsgo, Apache-2.0; Lucide icons, ISC).
import re, sys, os, subprocess

nsgo = os.popen("go env GOMODCACHE").read().strip()
nsgo += "/github.com/oneo-me/nsgo@v0.0.0-20261109153452-87e1fba091b9/icons/lucide/icons_gen.go"
src = open(nsgo).read()

used = subprocess.run(
    ["grep", "-rho", "lucide\\.[A-Z][A-Za-z]*", "cmd/t3go-app", "--include=*.go"],
    capture_output=True, text=True, cwd=".").stdout.split()
names = sorted(set(used))

# Icons renamed or removed in newer Lucide sets: map the client's name to
# the NSGo package's.
aliases = {"Columns": "Columns2"}

block = re.compile(
    r"func ([A-Za-z0-9]+)\(\) \*core\.SVG \{\n\treturn cache[A-Za-z0-9]+\.get\(\"(.*?)\"\)\n\}\n\nvar cache[A-Za-z0-9]+ lazyIcon\n",
    re.S)

funcs = {}
for m in block.finditer(src):
    funcs[m.group(1)] = m.group(2)

lines = []
lines.append("// Package lucide carries the Lucide icons the client draws, as MyGo")
lines.append("// SVGs. Generated from the NSGo lucide package (Apache-2.0); the")
lines.append('// icons are Lucide (ISC), https://lucide.dev.')
lines.append("package lucide")
lines.append("")
lines.append('import (')
lines.append('\t"sync"')
lines.append("")
lines.append('\t"github.com/egoist/mygo/ui"')
lines.append(')')
lines.append("")

count = 0
for ref in names:
    name = ref.removeprefix("lucide.")
    source = funcs.get(aliases.get(name, name))
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
    # The extracted body is a Go string literal (escapes intact); emit it
    # verbatim as one.
    lines.append(f'\tonce{name}.Do(func() {{ svg{name} = ui.MustParseSVG([]byte("' + source + '")) })')
    lines.append(f"\treturn svg{name}")
    lines.append("}")

os.makedirs("internal/app/icons/lucide", exist_ok=True)
open("internal/app/icons/lucide/lucide.go", "w").write("\n".join(lines) + "\n")
print(f"wrote {count} icons")
