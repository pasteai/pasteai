#!/usr/bin/env bash
# style-check.sh — enforces style rules from docs/STYLE_GUIDE.md that static
# analysis cannot catch. Exits non-zero if any violations are found.

set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

FAIL=0

red()   { printf '\033[31m%s\033[0m\n' "$*"; }
green() { printf '\033[32m%s\033[0m\n' "$*"; }
warn()  { printf '\033[33mWARN\033[0m  %s\n' "$*"; FAIL=1; }

echo "Running PasteAI style checks..."

# ── Panic outside main.go and *_test.go ────────────────────────────────────────
echo "  [1/7] panic() calls outside main.go and test files..."
while IFS= read -r match; do
  warn "$match"
done < <(grep -rn --include='*.go' '\bpanic(' . \
  | grep -v '_test\.go' \
  | grep -v '/main\.go' \
  | grep -v '//.*panic' \
  || true)

# ── Error string comparison (.Error() == or strings.Contains(err.Error())) ─────
echo "  [2/7] Error string comparisons (use errors.Is/As instead)..."
while IFS= read -r match; do
  warn "$match"
done < <(grep -rn --include='*.go' '\.Error() ==' . \
  | grep -v '_test\.go' \
  | grep -v '//.*Error' \
  || true)
while IFS= read -r match; do
  warn "$match"
done < <(grep -rn --include='*.go' 'strings\.Contains.*\.Error()' . \
  | grep -v '_test\.go' \
  || true)

# ── Rule 14: Exported functions must have doc comments ─────────────────────────
# Finds exported functions/types/vars with no preceding comment line.
echo "  [3/7] Exported symbols without doc comments..."
missing_docs=0
while IFS= read -r file; do
  # Skip test files and generated files
  [[ "$file" == *_test.go ]] && continue
  [[ "$file" == */vendor/* ]] && continue

  # Find exported func/type/var declarations with no preceding comment
  awk '
    /^\/\// { last_comment = NR; next }
    /^func [A-Z]|^type [A-Z]|^var [A-Z]|^const [A-Z]/ {
      if (NR - last_comment > 1) {
        print FILENAME ":" NR ": exported symbol has no doc comment: " $0
        found++
      }
    }
    { last_comment_reset = 1 }
  ' "$file"
done < <(find . -name '*.go' -not -path './vendor/*' -not -name '*_test.go') | while read -r line; do
  warn "$line"
  missing_docs=1
done

# ── Rule 64: Nil map initialisation ────────────────────────────────────────────
echo "  [4/7] Nil map declarations (var m map[...]=nil)..."
while IFS= read -r match; do
  warn "$match"
done < <(grep -rn --include='*.go' 'var [a-zA-Z]* map\[' . \
  | grep -v '_test.go' \
  | grep -v '= make\|= map\[' \
  | grep -v '//.*var' \
  || true)

# ── Rule 42: >3 mocks in one test file (heuristic) ─────────────────────────────
echo "  [5/7] Test files with >3 mock/stub/fake/spy types..."
while IFS= read -r file; do
  count=$(grep -cE '^type [A-Za-z]*(Mock|Stub|Fake|Spy|mock|stub|fake|spy)[A-Za-z]* (struct|interface)' "$file" 2>/dev/null || true)
  if [ "$count" -gt 3 ]; then
    warn "$file: $count mock/stub/fake/spy type definitions (>3 is a red flag per STYLE_GUIDE §8)"
  fi
done < <(find . -name '*_test.go' -not -path './vendor/*')

# ── Rule 58: context.Context must be first parameter ──────────────────────────
echo "  [6/7] Functions accepting context.Context not as first parameter..."
while IFS= read -r match; do
  warn "$match"
done < <(grep -rn --include='*.go' 'func .*([^)]*context\.Context' . \
  | grep -v '^.*func.*ctx context\.Context' \
  | grep -v '^.*func.*(ctx context\.Context' \
  | grep -v '^.*func.*(_ context\.Context' \
  | grep -v '^.*func.*(  *_ context\.Context' \
  | grep -v '_test.go' \
  | grep -v '//.*func' \
  | grep 'context\.Context' \
  || true)

# ── Rule 62: context.Value for non-metadata use (heuristic) ───────────────────
echo "  [7/7] context.WithValue calls (verify these are for metadata only)..."
matches=$(grep -rn --include='*.go' 'context\.WithValue' . | grep -v '_test.go' | grep -v '//.*metadata' || true)
if [ -n "$matches" ]; then
  while IFS= read -r match; do
    warn "$match  ← verify this is metadata (trace ID etc), not a functional input"
  done <<< "$matches"
fi

echo ""
if [ "$FAIL" -eq 0 ]; then
  green "All style checks passed."
  exit 0
else
  red "Style violations found. See docs/STYLE_GUIDE.md for rules."
  exit 1
fi
