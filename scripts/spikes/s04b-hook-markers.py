#!/usr/bin/env python3
"""S4b: do SessionEnd / Notification / InstructionsLoaded hooks run headlessly at all?

Each hook touches <Event>.marker in the temp project dir, so a hook that runs without
emitting a frame still shows up.
Run 1 (haiku): CLAUDE.md present, a prompt needing a Bash permission (answered after
  a 7 s delay), then 65 s idle, then end_session.
Run 2 (zero-token): initialize, then close stdin (no end_session).
"""
import json
import os
import sys
import time

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from spike import Claude, new_cwd  # noqa: E402

EVENTS = ["SessionStart", "SessionEnd", "Notification", "InstructionsLoaded", "PermissionRequest", "Stop",
          "UserPromptSubmit"]


def settings():
    hooks = {}
    for ev in EVENTS:
        e = {"hooks": [{"type": "command", "command": "echo spike-%s; touch %s.marker" % (ev, ev)}]}
        if ev == "PermissionRequest":
            e["matcher"] = "*"
        hooks[ev] = [e]
    return json.dumps({"hooks": hooks})


def markers(cwd):
    return sorted(f[:-7] for f in os.listdir(cwd) if f.endswith(".marker"))


def frames(c):
    return sorted({m.get("hook_event") for _, m in c.msgs
                   if m.get("type") == "system" and m.get("subtype") == "hook_response"
                   and "spike-" in (m.get("stdout") or "")})


def slow_allow(c, msg):
    req = msg.get("request", {})
    if req.get("subtype") == "can_use_tool":
        time.sleep(7)
        return {"behavior": "allow", "toolUseID": req.get("tool_use_id"), "updatedInput": req.get("input")}
    return None


cwd = new_cwd()
with open(os.path.join(cwd, "CLAUDE.md"), "w") as f:
    f.write("# spike\nKeep replies short.\n")
c = Claude("s04b-run1", cwd=cwd, args=["--settings", settings()], on_request=slow_allow)
s = len(c.msgs)
c.user("Use the Bash tool to run exactly this command: touch hello.txt . Then reply with just: done.")
c.wait_results(1, timeout=120, start=s)
print("run1 after turn: markers", markers(cwd), "frames", frames(c))
time.sleep(65)
print("run1 after 65s idle: markers", markers(cwd), "frames", frames(c))
c.ctl("end_session")
print("run1 exit", c.close(), "cost", c.cost())
time.sleep(1)
print("run1 after exit: markers", markers(cwd), "frames", frames(c))
others = [(t, m.get("type"), m.get("subtype")) for t, m in c.msgs
          if m.get("type") not in ("stream_event", "assistant", "user", "result", "control_response", "control_request")
          and m.get("subtype") not in ("hook_started", "hook_response", "hook_progress", "init", "status",
                                       "thinking_tokens", "session_state_changed")]
print("run1 other frames:", others)

cwd2 = new_cwd()
c2 = Claude("s04b-run2", cwd=cwd2, args=["--settings", settings()])
time.sleep(1)
print("\nrun2 exit on stdin close", c2.close(), "cost", c2.cost())
time.sleep(1)
print("run2 after exit: markers", markers(cwd2), "frames", frames(c2))
