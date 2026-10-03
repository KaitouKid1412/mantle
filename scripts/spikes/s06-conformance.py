#!/usr/bin/env python3
"""S6: zero-API conformance probe.

initialize, then get_hooks_listing, get_context_usage, mcp_status, list_models,
get_usage, end_session. Reports whether anything costs money and how the process exits.
The temp cwd gets a CLAUDE.md so memory files show up in get_context_usage.
"""
import json
import os
import sys
import time

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from spike import Claude, new_cwd, short  # noqa: E402

cwd = new_cwd()
with open(os.path.join(cwd, "CLAUDE.md"), "w") as f:
    f.write("# spike project\nSay hi.\n")

c = Claude("s06", cwd=cwd)
init = c.init_resp
print("initialize envelope keys:", list(init.keys()))
r = init.get("response", {})
print("initialize response keys:", list(r.keys()))
print("commands:", len(r.get("commands", [])), "first:", short(r.get("commands", [{}])[0]))
print("agents:", short(r.get("agents")))
print("capabilities:", r.get("capabilities"), "session_state:", r.get("session_state"))

for sub, fields in [("get_hooks_listing", {}), ("get_context_usage", {}), ("mcp_status", {}),
                    ("list_models", {}), ("get_usage", {}), ("get_session_cost", {}),
                    ("get_binary_version", {})]:
    t0 = time.time()
    resp = c.ctl(sub, **fields)
    dt = int((time.time() - t0) * 1000)
    body = resp.get("response") if resp else None
    print("\n==", sub, "%dms" % dt, "subtype:", resp and resp.get("subtype"))
    if isinstance(body, dict):
        print("keys:", list(body.keys()))
    if sub == "get_context_usage" and body:
        print("memoryFiles:", short(body.get("memoryFiles"), 400))
        print("categories[0]:", short((body.get("categories") or [None])[0]))
        for k in ("totalTokens", "maxTokens", "rawMaxTokens", "percentage", "model", "isAutoCompactEnabled"):
            print(" ", k, short(body.get(k)))
    elif sub == "get_hooks_listing" and body:
        print(short(body, 800))
    elif sub == "mcp_status" and body:
        for s in body.get("mcpServers", []):
            print("  server:", short({k: s.get(k) for k in s if k not in ("tools",)}, 300),
                  "tools:", len(s.get("tools") or []))
    elif sub == "list_models" and body:
        print("model[0]:", short((body.get("models") or [None])[0], 500))
    else:
        print(short(body, 600))

t_end = time.time()
resp = c.ctl("end_session", reason="spike")
print("\n== end_session response:", resp)
code = c.close(timeout=15)
print("exit code:", code, "after %dms" % int((time.time() - t_end) * 1000))
print("results seen:", sum(1 for _, m in c.msgs if m.get("type") == "result"))
print("\nTIMELINE\n" + c.timeline())
