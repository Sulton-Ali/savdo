#!/usr/bin/env bash
# Repo-wide guard rails run as the last step of `make verify`.
# Each check prints PASS/FAIL; any FAIL fails the whole script.
set -euo pipefail

cd "$(git rev-parse --show-toplevel)"

failed=0

check_pass() {
  echo "PASS: $1"
}

check_fail() {
  echo "FAIL: $1"
  failed=1
}

# 1. No tracked .env files (only .env.example may be tracked).
tracked_env="$(git ls-files | grep -E '(^|/)\.env(\..*)?$' | grep -v '\.env\.example$' || true)"
if [ -z "$tracked_env" ]; then
  check_pass "no tracked .env files"
else
  check_fail "tracked .env file(s) found:
$tracked_env"
fi

# 2. Last commit message carries no attribution trailer (D-19).
trailer_re='^(Co-authored-by|Claude-Session|Generated-with|Made-with|Reviewed-by|Signed-off-by):'
if git log -1 --pretty=%B 2>/dev/null | grep -qiE "$trailer_re"; then
  check_fail "last commit message contains an attribution trailer (D-19)"
else
  check_pass "last commit message has no attribution trailer"
fi

# 3. float64 must not appear near money fields in Go code.
go_files="$(find api -type f -name '*.go' 2>/dev/null || true)"
if [ -z "$go_files" ]; then
  check_pass "float64/money check (no Go files yet)"
else
  money_hits="$(printf '%s\n' "$go_files" | xargs -r grep -inE -B3 -A3 'float64' \
    | grep -inE '\b(price|cost|total|amount)\b' || true)"
  if [ -z "$money_hits" ]; then
    check_pass "no float64 within 3 lines of a money field"
  else
    check_fail "float64 found near a money field (price|cost|total|amount):
$money_hits"
  fi
fi

# 4. UPDATE stock_levels only allowed under internal/stock or db/queries/stock*.sql.
stock_files="$(find api -type f \( -name '*.go' -o -name '*.sql' \) 2>/dev/null || true)"
if [ -z "$stock_files" ]; then
  check_pass "no UPDATE stock_levels outside stock module (no Go/SQL files yet)"
else
  stock_hits="$(printf '%s\n' "$stock_files" | xargs -r grep -lIE 'UPDATE[[:space:]]+stock_levels' || true)"
  if [ -z "$stock_hits" ]; then
    check_pass "no UPDATE stock_levels outside stock module (none found)"
  else
    bad_hits="$(printf '%s\n' "$stock_hits" | grep -vE '^api/internal/stock/|^api/db/queries/stock[^/]*\.sql$' || true)"
    if [ -z "$bad_hits" ]; then
      check_pass "UPDATE stock_levels confined to internal/stock and db/queries/stock*.sql"
    else
      check_fail "UPDATE stock_levels found outside the stock module:
$bad_hits"
    fi
  fi
fi

# 5. No hand-rolled fetch() in app source outside the generated api client.
fetch_hits=""
for dir in admin/src web/src mobile/src; do
  if [ -d "$dir" ]; then
    hits="$(grep -rnE 'fetch\(' "$dir" 2>/dev/null | grep -v 'packages/api-client' || true)"
    if [ -n "$hits" ]; then
      fetch_hits="$fetch_hits
$hits"
    fi
  fi
done
if [ -z "$fetch_hits" ]; then
  check_pass "no fetch() calls outside packages/api-client"
else
  check_fail "fetch() found outside packages/api-client:$fetch_hits"
fi

exit $failed
