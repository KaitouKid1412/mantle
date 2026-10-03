#!/usr/bin/env python3
"""S1: interrupt mid-text, mid-tool, with a queued message, and with cancel_queued.

One haiku process, phases chosen by argv[1] (default ABCD):
  A  long answer, interrupt ~1.5 s after the first text delta
  B  Bash `sleep 20` (read-only, so no permission prompt), interrupt ~3 s after the tool_use
  C  long answer + a queued second message, interrupt (no cancel_queued)
  D  long answer + a queued second message, interrupt with cancel_queued:true
"""
import os
import sys
import time

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from spike import Claude, short  # noqa: E402

LONG = "Count from 1 to 300, one number per line, no other text."
PHASES = sys.argv[1] if len(sys.argv) > 1 else "ABCD"
c = Claude("s01" + ("" if PHASES == "ABCD" else "-" + PHASES))


def first_text(start):
    return c.wait(lambda m: m.get("type") == "stream_event" and m["event"]["type"] == "content_block_delta"
                  and m["event"]["delta"]["type"] == "text_delta", timeout=60, start=start)


def show_results(start):
    for i in range(start, len(c.msgs)):
        m = c.msgs[i][1]
        if m.get("type") == "result":
            print("result:", short({k: m.get(k) for k in ("subtype", "is_error", "terminal_reason", "stop_reason",
                                                           "num_turns", "result", "errors", "user_message_uuid",
                                                           "result_index", "queued_turn_count", "total_cost_usd")
                                    if k in m}, 700))
        if m.get("type") == "assistant" and m.get("aborted"):
            print("aborted assistant:", short({k: v for k, v in m.items() if k not in ("message",)}, 400))
        if m.get("type") == "user" and not m.get("isReplay"):
            print("user frame:", short({k: v for k, v in m.items() if k not in ("session_id",)}, 600))


def interrupt(**fields):
    r = c.ctl("interrupt", **fields)
    print("interrupt response:", r)


def phase_a():
    s = len(c.msgs)
    c.user(LONG)
    first_text(s)
    time.sleep(1.5)
    interrupt()
    c.wait_results(1, timeout=60, start=s)
    time.sleep(1)
    show_results(s)


def phase_b():
    s = len(c.msgs)
    c.user("Use the Bash tool to run exactly this command: sleep 20 && echo finished. Then reply done.")
    i = c.wait(lambda m: m.get("type") == "assistant" and any(
        b.get("type") == "tool_use" for b in m["message"].get("content", [])), timeout=60, start=s)
    print("tool_use seen:", i is not None)
    time.sleep(3)
    interrupt()
    c.wait_results(1, timeout=60, start=s)
    time.sleep(1)
    show_results(s)


def queued(cancel):
    s = len(c.msgs)
    c.user(LONG)
    first_text(s)
    u = c.user("Reply with just the word ok.")
    print("queued uuid", u)
    time.sleep(1.0)
    if cancel:
        interrupt(cancel_queued=True)
    else:
        interrupt()
    c.wait_results(1 if cancel else 2, timeout=60, start=s)
    time.sleep(5 if cancel else 1)
    show_results(s)


for p, fn in (("A", phase_a), ("B", phase_b), ("C", lambda: queued(False)), ("D", lambda: queued(True))):
    if p in PHASES:
        print("\n=== phase", p)
        fn()

print("\ncost:", c.cost())
print("exit:", c.close())
