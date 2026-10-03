#!/usr/bin/env python3
"""S2: priority now/next/later while a turn runs; cancel_async_message.

Phase 1: a turn with two sequential Bash sleeps. During the first sleep send
  N  priority "next"
  D  no priority (default)
  L1 priority "later"
  L2 priority "later", then cancel_async_message(L2)
Phase 2: a long text answer, then a priority "now" message mid-stream.
Prints lifecycle, replays and results with their uuids.
"""
import os
import sys
import time

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from spike import Claude, short  # noqa: E402

c = Claude("s02")
names = {}


def tag(u, name):
    names[u] = name
    return u


def nm(u):
    return names.get(u, (u or "")[:8])


def report(start):
    for t, m in c.msgs[start:]:
        typ = m.get("type")
        if typ == "command_lifecycle":
            print("%6d lifecycle %-3s %s" % (t, nm(m["command_uuid"]), m["state"]))
        elif typ == "user" and m.get("isReplay"):
            print("%6d replay   %-3s priority=%s" % (t, nm(m.get("uuid")), m.get("priority")))
        elif typ == "user":
            c_ = m["message"]["content"]
            kinds = c_ if isinstance(c_, str) else [b.get("type") + (":" + b.get("text", "")[:60] if b.get("type") == "text" else "") for b in c_]
            print("%6d user     %s" % (t, short(kinds, 200)))
        elif typ == "result":
            print("%6d RESULT   %s idx=%s queued=%s ums=%s term=%s result=%r" % (
                t, m.get("subtype"), m.get("result_index"), m.get("queued_turn_count"),
                [nm(u) for u in m.get("user_message_uuids") or []], m.get("terminal_reason"),
                (m.get("result") or "")[:60]))
        elif typ == "assistant":
            blocks = []
            for b in m["message"].get("content", []):
                if b["type"] == "text":
                    blocks.append("text:" + b["text"][:40])
                elif b["type"] == "tool_use":
                    blocks.append("tool_use:" + short(b["input"].get("command", b["input"]), 40))
                else:
                    blocks.append(b["type"])
            print("%6d assist   %s ums=%s aborted=%s" % (t, blocks, [nm(u) for u in m.get("user_message_uuids") or []],
                                                     m.get("aborted")))
        elif typ == "control_response":
            print("%6d ctlresp  %s" % (t, short(m["response"], 200)))
        elif typ == "system" and m.get("subtype") in ("session_state_changed",):
            print("%6d state    %s" % (t, m.get("state")))
        elif typ == "system" and m.get("subtype") == "init":
            print("%6d init" % t)


# Phase 1
print("=== phase 1: next / default / later / cancel_async_message")
s = len(c.msgs)
tag(c.user("Run these two Bash commands one at a time, as two separate tool calls (not in parallel, not combined): "
           "first `sleep 4`, then `sleep 4`. Then reply with just: done."), "T1")
i = c.wait(lambda m: m.get("type") == "assistant" and any(
    b.get("type") == "tool_use" for b in m["message"].get("content", [])), timeout=60, start=s)
time.sleep(0.5)
tag(c.user("Also append the word NEXT to your final reply.", priority="next"), "N")
time.sleep(0.2)
tag(c.user("Reply with just: DEFAULT."), "D")
time.sleep(0.2)
tag(c.user("Reply with just: LATER1.", priority="later"), "L1")
time.sleep(0.2)
l2 = tag(c.user("Reply with just: LATER2.", priority="later"), "L2")
time.sleep(0.3)
r = c.ctl("cancel_async_message", message_uuid=l2)
print("cancel_async_message(L2) ->", r)
r = c.ctl("cancel_async_message", message_uuid="00000000-0000-4000-8000-000000000000")
print("cancel_async_message(unknown) ->", r)
c.wait_results(3, timeout=150, start=s)
time.sleep(2)
report(s)

# Phase 2
print("\n=== phase 2: now")
s = len(c.msgs)
tag(c.user("Count from 1 to 200, one number per line, no other text."), "T2")
c.wait(lambda m: m.get("type") == "stream_event" and m["event"]["type"] == "content_block_delta"
       and m["event"]["delta"]["type"] == "text_delta", timeout=60, start=s)
time.sleep(1.0)
tag(c.user("Reply with just: NOW.", priority="now"), "NOW")
c.wait_results(2, timeout=90, start=s)
time.sleep(2)
report(s)

print("\ncost:", c.cost())
print("exit:", c.close())
