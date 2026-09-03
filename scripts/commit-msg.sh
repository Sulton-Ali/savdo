#!/usr/bin/env bash
# Enforces Conventional Commits and bans AI attribution trailers (D-19).
#
# Usage: commit-msg.sh <path-to-commit-message-file>
set -euo pipefail

msg_file="${1:?usage: commit-msg.sh <commit-message-file>}"
msg="$(cat "$msg_file")"
subject="$(printf '%s\n' "$msg" | head -n1)"

# Skip merge commits and fixup/squash commits — git and lefthook create these
# with their own conventions, not ours.
if printf '%s' "$subject" | grep -qiE '^(Merge |fixup!|squash!)'; then
  exit 0
fi

conventional_re='^(feat|fix|docs|chore|refactor|test|ci|build|perf|style|revert|merge)(\([a-z0-9._-]+\))?!?: .+'
if ! printf '%s' "$subject" | grep -qE "$conventional_re"; then
  echo "commit-msg: subject does not match Conventional Commits format." >&2
  echo "  got:      $subject" >&2
  echo "  expected: <type>(<scope>)?: <description>, type one of" >&2
  echo "            feat|fix|docs|chore|refactor|test|ci|build|perf|style|revert|merge" >&2
  exit 1
fi

trailer_re='^(Co-authored-by|Claude-Session|Generated-with|Made-with|Reviewed-by|Signed-off-by):'
if printf '%s\n' "$msg" | grep -qiE "$trailer_re"; then
  echo "commit-msg: attribution trailer found in commit message." >&2
  echo "  Savdo bans AI/attribution trailers on commits (D-19: docs/00-DECISIONS.md)." >&2
  echo "  Remove any Co-authored-by / Claude-Session / Generated-with / Made-with /" >&2
  echo "  Reviewed-by / Signed-off-by line and commit again." >&2
  exit 1
fi

exit 0
