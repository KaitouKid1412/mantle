#!/usr/bin/env python3
"""S3: does a headless run write ~/.claude/history.jsonl or ~/.claude/paste-cache?

Two runs in fresh temp dirs (one with --no-session-persistence, one without). Each sends
/context, then a tiny haiku prompt carrying a pasted_content entry and an inline paste.
Afterwards reports history.jsonl records whose "project" is that temp dir (keys only,
text redacted), new paste-cache files, and every file under the config dir that changed.
Session files created by the persistent run are deleted at the end.
"""
import json
import os
import sys
import time

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from spike import Claude, changed_since, claude_home, remove_session_files  # noqa: E402

HOME = claude_home()
HIST = os.path.join(HOME, "history.jsonl")
PASTE = os.path.join(HOME, "paste-cache")
PASTE_TEXT = "\n".join("pasted line %d: the quick brown fox" % i for i in range(60))


def hist_records(project):
    out = []
    try:
        with open(HIST) as f:
            for line in f:
                try:
                    r = json.loads(line)
                except ValueError:
                    continue
                if r.get("project") == project:
                    out.append(r)
    except FileNotFoundError:
        pass
    return out


def redact(v):
    if isinstance(v, dict):
        return {k: redact(x) for k, x in v.items()}
    if isinstance(v, list):
        return [redact(x) for x in v]
    if isinstance(v, str):
        return "<str %d>" % len(v)
    return v


def run(name, persist):
    before_paste = set(os.listdir(PASTE)) if os.path.isdir(PASTE) else set()
    st = os.stat(HIST) if os.path.exists(HIST) else None
    t0 = time.time() - 1
    c = Claude(name, persist=persist)
    s = len(c.msgs)
    c.user("/context")
    c.wait_results(1, timeout=60, start=s)
    s = len(c.msgs)
    c.user("Reply with just: ok", pasted_content=[PASTE_TEXT])
    c.wait_results(1, timeout=90, start=s)
    s = len(c.msgs)
    c.user("Reply with just: ok2. " + PASTE_TEXT, inline_pastes=[PASTE_TEXT])
    c.wait_results(1, timeout=90, start=s)
    sid = next(m["session_id"] for _, m in c.msgs if m.get("type") == "system" and m.get("subtype") == "init")
    cost = c.cost()
    code = c.close()
    time.sleep(1)
    st2 = os.stat(HIST) if os.path.exists(HIST) else None
    recs = hist_records(c.cwd)
    after_paste = set(os.listdir(PASTE)) if os.path.isdir(PASTE) else set()
    print("\n===", name, "persist=%s exit=%s cost=%.4f" % (persist, code, cost))
    print("history.jsonl mtime changed:", bool(st and st2 and st2.st_mtime != st.st_mtime),
          "size delta:", (st2.st_size - st.st_size) if st and st2 else None)
    print("history records for this cwd:", len(recs))
    for r in recs:
        print("  record shape:", json.dumps(redact(r)))
    print("new paste-cache files:", len(after_paste - before_paste))
    changed = [p.replace(sid, "<session-id>") for p in changed_since(t0)]
    print("files changed under config dir during run (%d):" % len(changed))
    for p in changed:
        print("  ", p)
    if persist:
        removed = remove_session_files(sid)
        print("removed session files:", [p.replace(HOME, "<config>").replace(sid, "<session-id>") for p in removed])


run("s03-nopersist", False)
run("s03-persist", True)
