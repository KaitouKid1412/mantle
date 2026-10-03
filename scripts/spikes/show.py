#!/usr/bin/env python3
"""Print a trimmed timeline of a spike capture (written by spike.py).

usage: show.py <capture.jsonl> [--width N] [--deltas] [--full TYPE[:SUBTYPE]]

Lines: "<ms> <dir> <summary>". dir is ">>" for stdin, "  " for stdout.
--deltas keeps every stream_event delta; --full prints whole frames of that type.
Home paths and email addresses are masked.
"""
import json
import os
import re
import sys

args = sys.argv[1:]
path = args[0]
width = 600
deltas = False
full = []
i = 1
while i < len(args):
    if args[i] == "--width":
        width = int(args[i + 1]); i += 2
    elif args[i] == "--deltas":
        deltas = True; i += 1
    elif args[i] == "--full":
        full.append(args[i + 1]); i += 2
    else:
        i += 1

HOME = os.path.expanduser("~")
EMAIL = re.compile(r"[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[a-z]{2,}")


def mask(s):
    return EMAIL.sub("<email>", s.replace(HOME, "/home/user"))


def j(v, n=200):
    s = json.dumps(v)
    return s if len(s) <= n else s[:n] + "..."


def summary(m):
    t = m.get("type")
    if t == "stream_event":
        e = m["event"]
        s = "stream_event " + e["type"]
        if e["type"] == "content_block_delta":
            s += ":" + e["delta"]["type"] + " " + j(e["delta"], 80)
        elif e["type"] == "content_block_start":
            s += ":" + e["content_block"]["type"] + " idx=%s" % e.get("index")
        elif e["type"] == "message_start":
            s += " id=%s model=%s" % (e["message"].get("id"), e["message"].get("model"))
        elif e["type"] == "message_delta":
            s += " " + j(e.get("delta"), 120)
        for k in ("parent_tool_use_id", "ttft_ms", "user_message_uuid"):
            if m.get(k) is not None:
                s += " %s=%s" % (k, m[k])
        return s
    if t == "assistant":
        msg = m["message"]
        ex = {k: v for k, v in m.items() if k not in ("message", "uuid", "session_id", "type")}
        for k in ("local_command_source", "context_usage", "usage_report"):
            if k in ex:
                ex[k] = "<%d chars>" % len(json.dumps(ex[k]))
        return "assistant id=%s model=%s stop=%s content=%s extra=%s" % (
            msg.get("id"), msg.get("model"), msg.get("stop_reason"), j(msg.get("content"), 220), j(ex, 400))
    if t == "user":
        ex = {k: v for k, v in m.items() if k not in ("session_id", "type", "message")}
        if "tool_use_result" in ex:
            ex["tool_use_result"] = j(ex["tool_use_result"], 150)
        return "user content=%s extra=%s" % (j(m["message"].get("content"), 220), j(ex, 300))
    if t == "result":
        keep = {k: m.get(k) for k in m if k not in ("usage", "modelUsage", "subagent_stats", "session_id", "uuid", "type")}
        if isinstance(keep.get("result"), str):
            keep["result"] = keep["result"][:160]
        return "result " + j(keep, 900)
    if t == "control_request":
        r = dict(m.get("request", {}))
        return "control_request %s %s" % (m.get("request_id"), j(r, 500))
    if t == "control_response":
        return "control_response " + j(m.get("response"), 500)
    return t + " " + j({k: v for k, v in m.items() if k not in ("type", "uuid", "session_id")}, 500)


last = None
for line in open(path):
    x = json.loads(line)
    d, m = x["dir"], x["msg"]
    if d == ">":
        s = ">> " + j(m, 400)
    elif d == "<":
        typ = m.get("type")
        key = typ + (":" + m["subtype"] if m.get("subtype") else "")
        if key in full or typ in full:
            s = "   " + json.dumps(m)
        else:
            s = "   " + summary(m)
        if not deltas and typ == "stream_event" and m["event"]["type"] == "content_block_delta":
            if last == "delta":
                continue
            last = "delta"
        else:
            last = None
    else:
        s = "-- %s %s" % (d, j(m, 300))
    print(mask("%6d %s" % (x["t"], s))[:width if not s.startswith("   {") else 100000])
