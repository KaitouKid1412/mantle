#!/usr/bin/env python3
"""S8: how local slash-command output arrives headlessly.

Sends /context, /cost, /usage, /model, /compact (empty session), /help (local-jsx),
/clear and an unknown /nonexistent one at a time, waiting for each result.
Only /nonexistent can reach the model (haiku, a few tokens).
"""
import json
import os
import sys

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from spike import Claude, short  # noqa: E402

c = Claude("s08")
cmds = ["/context", "/cost", "/usage", "/model", "/compact", "/help", "/clear", "/nonexistent-cmd say ok"]
for cmd in cmds:
    start = len(c.msgs)
    u = c.user(cmd)
    got = c.wait_results(1, timeout=90, start=start)
    end = len(c.msgs)
    print("\n=====", cmd, "uuid", u[:8])
    for t, m in c.msgs[start:end]:
        typ = m.get("type")
        if typ == "stream_event":
            continue
        if typ == "assistant":
            extra = {k: v for k, v in m.items() if k not in ("message", "uuid", "session_id", "type")}
            print("assistant extra fields:", short(extra, 700))
            print("  model:", m["message"].get("model"), "content:", short(m["message"].get("content"), 300))
        elif typ == "result":
            keep = {k: m.get(k) for k in ("subtype", "is_error", "num_turns", "local_command", "result",
                                          "terminal_reason", "stop_reason", "total_cost_usd", "user_message_uuid",
                                          "errors") if k in m}
            print("result:", short(keep, 600))
            print("  result keys:", list(m.keys()))
        elif typ == "user":
            print("user:", short({k: m[k] for k in m if k not in ("session_id",)}, 400))
        elif typ == "conversation_reset":
            print("conversation_reset:", json.dumps(m))
        elif typ == "system" and m.get("subtype") == "init":
            print("system init session_id", m.get("session_id")[:8])
        else:
            print(typ, m.get("subtype") or "", short({k: v for k, v in m.items() if k not in ("type", "subtype", "uuid")}, 400))

print("\ncost:", c.cost())
print("exit:", c.close())
print("\nTIMELINE\n" + c.timeline())
