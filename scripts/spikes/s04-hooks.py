#!/usr/bin/env python3
"""S4: which hook frames --include-hook-events emits; does Notification fire headlessly?

Passes harmless `echo` command hooks for many events through --settings (flag tier),
then runs one haiku turn that needs a Bash permission (touch a file; allowed through
can_use_tool), then end_session. The user's own (plugin) hooks also run; spike hooks are
the ones whose output starts with "spike-".
"""
import json
import os
import sys
import time

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from spike import Claude, short  # noqa: E402

EVENTS = ["SessionStart", "UserPromptSubmit", "PreToolUse", "PostToolUse", "PermissionRequest", "Notification",
          "Stop", "SessionEnd", "SubagentStart", "SubagentStop", "PreCompact", "PostCompact", "InstructionsLoaded",
          "PostToolBatch", "MessageDisplay", "CwdChanged", "FileChanged"]
hooks = {}
for ev in EVENTS:
    cmd = "echo spike-%s" % ev
    if ev == "PreToolUse":
        cmd = "echo spike-PreToolUse-start; sleep 2; echo spike-PreToolUse-end"
    entry = {"hooks": [{"type": "command", "command": cmd}]}
    if ev in ("PreToolUse", "PostToolUse", "PermissionRequest"):
        entry["matcher"] = "*"
    hooks[ev] = [entry]

c = Claude("s04", args=["--settings", json.dumps({"hooks": hooks})])
listing = c.ctl("get_hooks_listing")["response"]
print("hooks listing sources:", sorted({h.get("source") for h in listing.get("hooks", [])}))
print("flag hook entry example:", short(next((h for h in listing["hooks"] if "spike-Stop" in json.dumps(h)), None), 400))

s = len(c.msgs)
c.user("Use the Bash tool to run exactly this command: touch hello.txt . Then reply with just: done.")
c.wait_results(1, timeout=120, start=s)
time.sleep(2)
print("end_session:", c.ctl("end_session"))
code = c.close()
print("exit:", code, "cost:", c.cost())

print("\nHOOK AND RELATED FRAMES")
for t, m in c.msgs:
    typ = m.get("type")
    if typ == "system" and m.get("subtype", "").startswith("hook_"):
        out = (m.get("stdout") or m.get("output") or "").strip().replace("\n", " | ")
        print("%6d %-13s %-28s event=%-18s outcome=%s exit=%s out=%r" % (
            t, m["subtype"], m.get("hook_name"), m.get("hook_event"), m.get("outcome"), m.get("exit_code"), out[:80]))
    elif typ == "system" and m.get("subtype") not in ("init", "session_state_changed", "status", "thinking_tokens"):
        print("%6d system %s %s" % (t, m.get("subtype"), short(m, 300)))
    elif typ == "control_request":
        print("%6d control_request %s %s" % (t, m["request"].get("subtype"), m["request"].get("tool_name")))
    elif typ == "result":
        print("%6d result %s" % (t, m.get("subtype")))
    elif typ in ("assistant", "user"):
        blocks = m["message"]["content"]
        kinds = blocks if isinstance(blocks, str) else [b.get("type") for b in blocks]
        print("%6d %s %s" % (t, typ, short(kinds, 100)))
