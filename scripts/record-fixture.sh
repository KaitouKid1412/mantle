#!/usr/bin/env bash
# Record engine fixtures into testdata/fixtures/02/.
#
#   scripts/record-fixture.sh                  re-record every built-in flow (real claude
#                                              against fakeapi: offline, free)
#   scripts/record-fixture.sh <flow>           re-record one built-in flow (see flows in
#                                              internal/engine/enginetest/record_test.go)
#   scripts/record-fixture.sh -drive <client.jsonl> <name> [claude args...]
#                                              record a custom session: runs the real
#                                              claude with your claude args (it may call
#                                              the real API and cost money), drives it with
#                                              a client-mode enginefake script, writes
#                                              testdata/fixtures/02/<name>.jsonl (+ .ndjson)
#
# Recordings are sanitised (home paths, usernames, emails, keys, session ids, engine
# prose). Check the diff before committing anyway.
set -euo pipefail

root="$(cd "$(dirname "$0")/.." && pwd)"
out="$root/testdata/fixtures/02"
cd "$root"

if [[ "${1:-}" == "-drive" ]]; then
  [[ $# -ge 3 ]] || { echo "usage: $0 -drive <client.jsonl> <name> [claude args...]" >&2; exit 2; }
  client="$2" name="$3"
  shift 3
  bin="$(mktemp -d)/fakeclaude"
  go build -o "$bin" ./cmd/fakeclaude
  "$bin" record -o "$out/$name.jsonl" -drive "$client" -- claude \
    --output-format stream-json --input-format stream-json --verbose \
    --include-partial-messages --permission-prompt-tool stdio \
    --include-hook-events --forward-subagent-text --replay-user-messages "$@"
  # Engine stdout only, one message per line (plan 03's replay goldens).
  python3 -c 'import json,sys
for l in open(sys.argv[1]):
    e=json.loads(l)
    if e["dir"]=="out": print(json.dumps(e["msg"],separators=(",",":"),ensure_ascii=False))' \
    "$out/$name.jsonl" > "$out/$name.ndjson"
  echo "wrote $out/$name.jsonl and .ndjson"
  exit 0
fi

MANTLE_RECORD_FIXTURES=1 MANTLE_RECORD_ONLY="${1:-}" \
  go test -count=1 -run TestRecordFixtures -v ./internal/engine/enginetest | grep -E 'record_test|^(---|ok|FAIL)'
