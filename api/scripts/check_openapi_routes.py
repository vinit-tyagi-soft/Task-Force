#!/usr/bin/env python3
"""Fail CI when the token OpenAPI operations drift from registered token routes."""
import json
import re
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
SERVER = (ROOT / "internal/httpapi/server.go").read_text()
INTEGRATIONS = (ROOT / "internal/httpapi/integrations.go").read_text()
SPEC = json.loads((ROOT / "internal/httpapi/openapi.json").read_text())

UUID = r"[0-9a-fA-F-]{36}"
block = re.search(r"for _, p := range \[\]string\{(.*?)\n\s*\}", INTEGRATIONS, re.S)
if not block:
    raise SystemExit("could not find token route allowlist in integrations.go")

token_patterns = []
for line in block.group(1).splitlines():
    parts = re.findall(r"`([^`]*)`", line)
    if not parts:
        continue
    pattern = parts[0]
    if "+ id +" in line:
        if len(parts) != 2:
            raise SystemExit(f"unrecognized token route pattern: {line.strip()}")
        pattern += UUID + parts[1]
    elif len(parts) > 1:
        raise SystemExit(f"unrecognized token route pattern: {line.strip()}")
    token_patterns.append(re.compile(pattern))

if not token_patterns:
    raise SystemExit("token route allowlist is empty")

registered = re.findall(r'mux\.Handle(?:Func)?\("([A-Z]+) ([^"]+)"', SERVER)
actual = set()
for method, template in registered:
    if method not in {"GET", "POST", "PATCH"}:
        continue  # tokenActor permits GET, and POST/PATCH only for write tokens
    concrete = re.sub(r"\{[^}]+\}", UUID, template)
    if any(pattern.fullmatch(concrete) for pattern in token_patterns):
        actual.add((template, method.lower()))

documented = {
    (path, method.lower())
    for path, operations in SPEC.get("paths", {}).items()
    for method in operations
    if method.lower() in {"get", "post", "patch"}
}

missing = sorted(actual - documented)
stale = sorted(documented - actual)
if missing or stale:
    if missing:
        print("registered token routes missing from OpenAPI:")
        for path, method in missing:
            print(f"  {method.upper()} {path}")
    if stale:
        print("OpenAPI operations not registered for API tokens:")
        for path, method in stale:
            print(f"  {method.upper()} {path}")
    raise SystemExit(1)

print(f"OpenAPI route check passed ({len(actual)} token operations).")
