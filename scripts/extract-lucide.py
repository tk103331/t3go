#!/usr/bin/env python3
"""Extract the icon bodies of internal/app/icons/lucide/lucide.go into
internal/app/icons/lucide/icons.json.

The Lucide icons (ISC, https://lucide.dev) came with the NSGo package
(Apache-2.0). They are kept in this repository as icons.json, so the
client does not depend on that package to draw them.
"""
import json
import os
import re
import sys

LUCIDE = os.path.join("internal", "app", "icons", "lucide")

PATTERN = re.compile(r'svg[A-Za-z0-9]+ = ui\.MustParseSVG\(\[\]byte\("(.*?)"\)\)', re.S)


def unescape(text: str) -> str:
    out = []
    i = 0
    while i < len(text):
        ch = text[i]
        if ch != "\\":
            out.append(ch)
            i += 1
            continue
        i += 1
        if i >= len(text):
            break
        nxt = text[i]
        if nxt == "n":
            out.append("\n")
        elif nxt == "t":
            out.append("\t")
        elif nxt == "r":
            out.append("\r")
        elif nxt == '"':
            out.append('"')
        elif nxt == "\\":
            out.append("\\")
        else:
            out.append(nxt)
        i += 1
    return "".join(out)


def main() -> int:
    source = os.path.join(LUCIDE, "lucide.go")
    with open(source, encoding="utf-8") as handle:
        text = handle.read()
    icons = {}
    target = os.path.join(LUCIDE, "icons.json")
    if os.path.exists(target):
        with open(target, encoding="utf-8") as handle:
            icons = json.load(handle)
    for match in PATTERN.finditer(text):
        body = unescape(match.group(1))
        name = match.group(0).split("svg", 1)[1]
        name = name.split(" ", 1)[0]
        icons[name] = body
    if not icons:
        print("no icons found in " + source, file=sys.stderr)
        return 1
    with open(os.path.join(LUCIDE, "icons.json"), "w", encoding="utf-8") as handle:
        json.dump(icons, handle, indent=1, sort_keys=True, ensure_ascii=False)
        handle.write("\n")
    print(f"wrote {len(icons)} icons")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
