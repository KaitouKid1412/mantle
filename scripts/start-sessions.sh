#!/usr/bin/env bash
# Start mantle's plan sessions in parallel.
#
#   scripts/start-sessions.sh                 # plans 01-11 as Claude Code background sessions
#   scripts/start-sessions.sh 01 02 04        # only these plans
#   scripts/start-sessions.sh --print [NN..]  # print one command per plan to paste into terminal tabs
#
# Extra claude flags go in CLAUDE_ARGS, e.g. CLAUDE_ARGS="--permission-mode acceptEdits".
# Background sessions are watched and answered with `claude agents`.
set -euo pipefail
cd "$(git rev-parse --show-toplevel)"

mode=bg
if [[ "${1:-}" == "--print" ]]; then mode=print; shift; fi

plans=("$@")
if [[ ${#plans[@]} -eq 0 ]]; then
  plans=(01 02 04 03 05 10 06 07 11 08 09) # plan 12 starts after M1
fi

for nn in "${plans[@]}"; do
  file=$(ls docs/plans/"$nn"-*.md 2>/dev/null | head -1 || true)
  if [[ -z "$file" ]]; then
    echo "no plan file docs/plans/$nn-*.md" >&2
    exit 1
  fi
  prompt="You are session $nn of mantle. Read CLAUDE.md, docs/plans/00-overview.md and $file, then execute that plan. Do Part A now. Start Part B once \`git tag -l\` shows the tags it needs. Commit with prefix [$nn]."
  if [[ $mode == print ]]; then
    printf 'cd %q && claude -n mantle-%s %s %q\n' "$PWD" "$nn" "${CLAUDE_ARGS:-}" "$prompt"
  else
    printf 'mantle-%s: ' "$nn"
    # shellcheck disable=SC2086
    claude --bg -n "mantle-$nn" ${CLAUDE_ARGS:-} "$prompt"
  fi
done

if [[ $mode == bg ]]; then
  echo "Started ${#plans[@]} sessions. Watch and answer them with: claude agents"
fi
