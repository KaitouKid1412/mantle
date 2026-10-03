#!/usr/bin/env python3
"""Shared driver for mantle protocol spikes against the real `claude` binary.

Spawns claude the way mantle will (stream-json both ways), records every frame in
both directions with a millisecond timestamp, and offers small helpers to send
user messages and control requests and to wait for frames.

Captures go to $SPIKE_OUT (default: a fresh mktemp dir). They contain account data,
paths and session ids, so never point SPIKE_OUT into the repository.

Every spawn strips CLAUDE*/NODE_OPTIONS/DEBUG from the inherited environment so a
run inside a Claude Code session behaves like one started from a plain terminal.
"""
import json
import os
import subprocess
import sys
import tempfile
import threading
import time
import uuid as uuidlib

BASE_FLAGS = [
    "--output-format", "stream-json", "--input-format", "stream-json", "--verbose",
    "--include-partial-messages", "--permission-prompt-tool", "stdio",
    "--include-hook-events", "--forward-subagent-text", "--replay-user-messages",
]


def out_dir():
    d = os.environ.get("SPIKE_OUT")
    if not d:
        d = tempfile.mkdtemp(prefix="mantle-spike-out-")
        os.environ["SPIKE_OUT"] = d
    os.makedirs(d, exist_ok=True)
    return d


def new_cwd(prefix="mantle-spike-"):
    return os.path.realpath(tempfile.mkdtemp(prefix=prefix))


def clean_env(extra=None, unset=()):
    env = {k: v for k, v in os.environ.items()
           if not k.startswith("CLAUDE") and k not in ("NODE_OPTIONS", "DEBUG", "SPIKE_OUT")}
    env["CLAUDE_CODE_EMIT_SESSION_STATE_EVENTS"] = "1"
    env["CLAUDE_CODE_ENABLE_SDK_FILE_CHECKPOINTING"] = "true"
    for k in unset:
        env.pop(k, None)
    if extra:
        env.update(extra)
    return env


def new_uuid():
    return str(uuidlib.uuid4())


class Claude:
    def __init__(self, name, args=(), env=None, unset=(), cwd=None, persist=False,
                 model="haiku", budget="0.25", base=True, on_request=None, initialize=True,
                 init_fields=None):
        self.name = name
        self.cwd = cwd or new_cwd()
        flags = list(BASE_FLAGS) if base else []
        if model:
            flags += ["--model", model]
        if budget:
            flags += ["--max-budget-usd", budget]
        if not persist:
            flags.append("--no-session-persistence")
        flags += list(args)
        self.flags = flags
        d = out_dir()
        self.cap = open(os.path.join(d, name + ".jsonl"), "w")
        self.err = open(os.path.join(d, name + ".stderr"), "w")
        self.lock = threading.Lock()
        self.cv = threading.Condition()
        self.msgs = []  # (t_ms, msg)
        self.on_request = on_request or allow_all
        self.req_n = 0
        self.t0 = time.time()
        self.p = subprocess.Popen(["claude"] + flags, cwd=self.cwd, env=clean_env(env, unset),
                                  stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=self.err)
        self.reader = threading.Thread(target=self._read, daemon=True)
        self.reader.start()
        self.init_resp = None
        if initialize:
            fields = {"promptSuggestions": True}
            fields.update(init_fields or {})
            self.init_resp = self.ctl("initialize", timeout=60, **fields)

    def t(self):
        return int((time.time() - self.t0) * 1000)

    def _log(self, direction, msg):
        with self.lock:
            self.cap.write(json.dumps({"t": self.t(), "dir": direction, "msg": msg}) + "\n")
            self.cap.flush()

    def _read(self):
        for raw in self.p.stdout:
            line = raw.decode("utf-8", "replace").strip()
            if not line.startswith("{"):
                if line:
                    self._log("<raw", line)
                continue
            msg = json.loads(line)
            self._log("<", msg)
            with self.cv:
                self.msgs.append((self.t(), msg))
                self.cv.notify_all()
            if msg.get("type") == "control_request":
                threading.Thread(target=self._answer, args=(msg,), daemon=True).start()
        with self.cv:
            self.msgs.append((self.t(), {"type": "__eof__"}))
            self.cv.notify_all()

    def _answer(self, msg):
        resp = self.on_request(self, msg)
        if resp is None:
            return
        self.send({"type": "control_response", "response": {
            "subtype": "success", "request_id": msg["request_id"], "response": resp}})

    def send(self, obj):
        self._log(">", obj)
        try:
            self.p.stdin.write((json.dumps(obj) + "\n").encode())
            self.p.stdin.flush()
        except BrokenPipeError:
            pass

    def user(self, content, uuid=None, **extra):
        uuid = uuid or new_uuid()
        m = {"type": "user", "session_id": "", "parent_tool_use_id": None, "uuid": uuid,
             "message": {"role": "user", "content": content}, "origin": {"kind": "human"}}
        m.update(extra)
        self.send(m)
        return uuid

    def ctl(self, subtype, wait=True, timeout=30, **fields):
        self.req_n += 1
        rid = "r%d" % self.req_n
        req = {"subtype": subtype}
        req.update(fields)
        start = len(self.msgs)
        self.send({"type": "control_request", "request_id": rid, "request": req})
        if not wait:
            return rid
        i = self.wait(lambda m: m.get("type") == "control_response"
                      and m["response"].get("request_id") == rid, timeout=timeout, start=start)
        return self.msgs[i][1]["response"] if i is not None else None

    def wait(self, pred, timeout=60, start=0):
        deadline = time.time() + timeout
        with self.cv:
            while True:
                for i in range(start, len(self.msgs)):
                    if pred(self.msgs[i][1]):
                        return i
                    if self.msgs[i][1].get("type") == "__eof__":
                        return None
                left = deadline - time.time()
                if left <= 0:
                    return None
                self.cv.wait(left)

    def wait_type(self, typ, subtype=None, timeout=60, start=0):
        return self.wait(lambda m: m.get("type") == typ and (subtype is None or m.get("subtype") == subtype),
                         timeout=timeout, start=start)

    def wait_results(self, n, timeout=120, start=0):
        deadline = time.time() + timeout
        while True:
            got = [i for i in range(start, len(self.msgs)) if self.msgs[i][1].get("type") == "result"]
            if len(got) >= n or time.time() > deadline or self.exited():
                return got
            time.sleep(0.1)

    def exited(self):
        return self.p.poll() is not None

    def close(self, timeout=20):
        try:
            self.p.stdin.close()
        except Exception:
            pass
        try:
            code = self.p.wait(timeout=timeout)
        except subprocess.TimeoutExpired:
            self.p.terminate()
            code = self.p.wait(timeout=5)
        self.reader.join(timeout=5)
        self._log("exit", code)
        self.cap.close()
        self.err.close()
        return code

    def cost(self):
        c = 0.0
        for _, m in self.msgs:
            if m.get("type") == "result" and m.get("parent_tool_use_id") is None:
                c = max(c, m.get("total_cost_usd") or 0.0)
        return c

    def timeline(self, skip_deltas=True, width=220):
        """Compact one-line-per-frame summary (no content text)."""
        lines = []
        for t, m in self.msgs:
            lines.append("%6d %s" % (t, brief(m)[:width]))
        if skip_deltas:
            out, last = [], None
            for l in lines:
                key = l[7:]
                if "stream_event content_block_delta" in key and last and "stream_event content_block_delta" in last:
                    continue
                out.append(l)
                last = key
            lines = out
        return "\n".join(lines)


def allow_all(c, msg):
    req = msg.get("request", {})
    if req.get("subtype") == "can_use_tool":
        return {"behavior": "allow", "toolUseID": req.get("tool_use_id"), "updatedInput": req.get("input")}
    if req.get("subtype") == "hook_callback":
        return {}
    return None


def brief(m):
    t = m.get("type")
    st = m.get("subtype")
    s = t + (" " + st if st else "")
    if t == "stream_event":
        e = m["event"]
        s += " " + e["type"]
        if e["type"] == "content_block_delta":
            s += ":" + e["delta"]["type"]
        if e["type"] == "content_block_start":
            s += ":" + e["content_block"]["type"]
        if m.get("parent_tool_use_id"):
            s += " ptu=" + m["parent_tool_use_id"][-8:]
    elif t == "assistant":
        msg = m["message"]
        s += " id=%s blocks=%s stop=%s" % (str(msg.get("id"))[-8:], [b.get("type") for b in msg.get("content", [])],
                                           msg.get("stop_reason"))
        for k in ("aborted", "parent_tool_use_id", "error", "user_message_uuid", "user_message_uuids", "supersedes",
                  "local_command"):
            if m.get(k) is not None:
                s += " %s=%s" % (k, short(m[k]))
    elif t == "user":
        c = m["message"].get("content")
        kinds = c if isinstance(c, str) else [b.get("type") for b in c]
        s += " content=%s" % short(kinds)
        for k in ("isReplay", "isSynthetic", "uuid", "parent_tool_use_id", "priority"):
            if m.get(k) is not None:
                s += " %s=%s" % (k, short(m[k]))
    elif t == "result":
        for k in ("is_error", "num_turns", "stop_reason", "terminal_reason", "result_index", "queued_turn_count",
                  "user_message_uuid", "user_message_uuids", "local_command", "total_cost_usd", "errors"):
            if k in m:
                s += " %s=%s" % (k, short(m[k]))
    elif t == "command_lifecycle":
        s += " %s %s" % (m.get("command_uuid", "")[:8], m.get("state"))
    elif t == "control_request":
        r = m.get("request", {})
        s += " %s %s" % (m.get("request_id"), r.get("subtype"))
        if r.get("tool_name"):
            s += " " + r["tool_name"]
    elif t == "control_response":
        r = m.get("response", {})
        s += " %s %s %s" % (r.get("request_id"), r.get("subtype"), short(r.get("response", r.get("error"))))
    elif t == "system":
        for k in ("state", "hook_event", "hook_name", "status", "task_id", "description", "trigger"):
            if m.get(k) is not None:
                s += " %s=%s" % (k, short(m[k]))
    else:
        keys = [k for k in m.keys() if k not in ("type", "uuid", "session_id")]
        s += " keys=%s" % keys
    return s


def claude_home():
    return os.environ.get("CLAUDE_CONFIG_DIR") or os.path.expanduser("~/.claude")


def project_slug(path):
    return "".join(ch if ch.isalnum() else "-" for ch in path)


def session_files(sid, home=None):
    """Every path under the config dir named after a session id."""
    import glob
    home = home or claude_home()
    pats = ["projects/*/%s.jsonl", "projects/*/%s", "file-history/%s", "session-env/%s", "tasks/%s",
            "todos/*%s*", "debug/%s*", "plans/*%s*", "sessions/*%s*"]
    out = []
    for p in pats:
        out += glob.glob(os.path.join(home, p % sid))
    return sorted(set(out))


def remove_session_files(sid, home=None):
    import shutil
    removed = []
    for p in session_files(sid, home):
        if os.path.isdir(p):
            shutil.rmtree(p)
        else:
            os.remove(p)
        removed.append(p)
        parent = os.path.dirname(p)
        if os.path.basename(os.path.dirname(parent)) == "projects" and not os.listdir(parent):
            os.rmdir(parent)
    return removed


def changed_since(marker_time, home=None, skip=("jobs",)):
    """Files under the config dir modified after marker_time (seconds since epoch)."""
    home = home or claude_home()
    out = []
    for root, dirs, files in os.walk(home):
        rel = os.path.relpath(root, home)
        if rel.split(os.sep)[0] in skip:
            dirs[:] = []
            continue
        for f in files:
            p = os.path.join(root, f)
            try:
                if os.path.getmtime(p) > marker_time:
                    out.append(os.path.relpath(p, home))
            except OSError:
                pass
    return sorted(out)


def short(v, n=120):
    s = json.dumps(v) if not isinstance(v, str) else v
    return s if len(s) <= n else s[:n] + "..."


def dump(m, n=1500):
    print(json.dumps(m)[:n])
