#!/bin/sh
set -u

skill_dir=$(cd "$(dirname "$0")" && pwd) || exit 2
root=$(git rev-parse --show-toplevel 2>/dev/null) || {
  echo "b4-check: run it inside a b4 checkout" >&2
  exit 2
}
cd "$root" || exit 2

ref=${1:-origin/main}
base=$(git merge-base HEAD "$ref" 2>/dev/null) || {
  echo "b4-check: no merge base with $ref; run git fetch origin or pass a base ref" >&2
  exit 2
}

tmp=$(mktemp -d "${TMPDIR:-/tmp}/b4-check.XXXXXX") || exit 2
trap 'rm -rf "$tmp"' EXIT
trap 'exit 130' INT TERM

fails=0
notes=0
fail() {
  fails=$((fails + 1))
  printf 'FAIL  %s\n' "$*"
}
note() {
  notes=$((notes + 1))
  printf 'NOTE  %s\n' "$*"
}
detail() {
  head -n 20 "$1" | sed 's/^/        /'
  lines=$(wc -l < "$1" | tr -d ' ')
  if [ "$lines" -gt 20 ]; then
    printf '        ... and %s more\n' $((lines - 20))
  fi
}
has() {
  grep -Eq -- "$1" "$tmp/changed"
}

git diff --name-only --no-renames "$base" > "$tmp/tracked"
git ls-files --others --exclude-standard > "$tmp/untracked"
sort -u "$tmp/tracked" "$tmp/untracked" > "$tmp/changed"
if [ ! -s "$tmp/changed" ]; then
  echo "b4-check: nothing changed against $ref"
  exit 0
fi
printf 'b4-check: %s changed files against %s (%s)\n\n' "$(wc -l < "$tmp/changed" | tr -d ' ')" "$ref" "$(git rev-parse --short "$base")"

git diff -U0 --no-color --no-ext-diff --no-renames "$base" > "$tmp/diff"
awk -f "$skill_dir/added-lines.awk" "$tmp/diff" > "$tmp/added"
while IFS= read -r f; do
  if [ -f "$f" ] && grep -Iq . "$f" 2>/dev/null; then
    awk -v file="$f" '{ print file ":" FNR ":" $0 }' "$f" >> "$tmp/added"
  fi
done < "$tmp/untracked"
grep -Ev '^(install\.sh|pnpm-lock\.yaml|docs/static/swagger\.json|docs/static/swagger-versions/[^:]*|hub/ui/src/models/api\.gen\.ts|src/http/ui/src/models/defaults\.json):' "$tmp/added" > "$tmp/scan"

emdash=$(printf '\342\200\224')
grep -F -- "$emdash" "$tmp/scan" > "$tmp/emdash"
if [ -s "$tmp/emdash" ]; then
  fail "em-dash in added lines; use a plain hyphen:"
  detail "$tmp/emdash"
fi

awk -f "$skill_dir/comments.awk" "$tmp/scan" > "$tmp/comments"
if [ -s "$tmp/comments" ]; then
  fail "comments in added code; the repo has none, explanations go in the commit message:"
  detail "$tmp/comments"
fi

if command -v node >/dev/null 2>&1; then
  for strings in src/http/ui/src/i18n hub/ui/src/i18n; do
    if [ -f "$strings/en.json" ] && [ -f "$strings/ru.json" ]; then
      if ! node "$skill_dir/i18n-keys.mjs" "$strings/en.json" "$strings/ru.json" > "$tmp/i18n" 2>&1; then
        fail "$strings: en.json and ru.json do not carry the same keys:"
        detail "$tmp/i18n"
      fi
    fi
  done
else
  note "node not found: skipped the en/ru key comparison"
fi

ru_docs=docs/i18n/ru/docusaurus-plugin-content-docs/current
grep -E '^docs/docs/.+\.mdx?$' "$tmp/changed" | sed 's|^docs/docs/||' > "$tmp/docs_en"
grep -E "^$ru_docs/.+\.mdx?\$" "$tmp/changed" | sed "s|^$ru_docs/||" > "$tmp/docs_ru"
sort -u "$tmp/docs_en" "$tmp/docs_ru" > "$tmp/docs_all"
: > "$tmp/docs_missing"
: > "$tmp/docs_one_side"
while IFS= read -r rel; do
  if [ -e "docs/docs/$rel" ] && [ ! -e "$ru_docs/$rel" ]; then
    echo "$ru_docs/$rel is missing" >> "$tmp/docs_missing"
  elif [ ! -e "docs/docs/$rel" ] && [ -e "$ru_docs/$rel" ]; then
    echo "docs/docs/$rel is missing" >> "$tmp/docs_missing"
  elif ! grep -qxF -- "$rel" "$tmp/docs_en" || ! grep -qxF -- "$rel" "$tmp/docs_ru"; then
    echo "$rel" >> "$tmp/docs_one_side"
  fi
done < "$tmp/docs_all"
if [ -s "$tmp/docs_missing" ]; then
  fail "a docs page exists in one locale only:"
  detail "$tmp/docs_missing"
fi
if [ -s "$tmp/docs_one_side" ]; then
  note "docs page changed in one locale only; a correction in one locale applies to the other:"
  detail "$tmp/docs_one_side"
fi

changelog_en=0
changelog_ru=0
has '^changelog\.md$' && changelog_en=1
has '^changelog_ru\.md$' && changelog_ru=1
if [ "$changelog_en" != "$changelog_ru" ]; then
  fail "changelog.md and changelog_ru.md change together"
fi
if [ "$changelog_en" = 1 ] || [ "$changelog_ru" = 1 ]; then
  top_bullets() {
    awk '/^## \[/ { n++; next } n == 1 && /^- / { c++ } n > 1 { exit } END { print c + 0 }' "$1"
  }
  heading_en=$(grep -m 1 '^## \[' changelog.md)
  heading_ru=$(grep -m 1 '^## \[' changelog_ru.md)
  if [ "$heading_en" != "$heading_ru" ]; then
    fail "top changelog headings differ: \"$heading_en\" and \"$heading_ru\""
  fi
  bullets_en=$(top_bullets changelog.md)
  bullets_ru=$(top_bullets changelog_ru.md)
  if [ "$bullets_en" != "$bullets_ru" ]; then
    fail "top changelog block has $bullets_en bullets in changelog.md and $bullets_ru in changelog_ru.md"
  fi
fi
if grep -E '^src/' "$tmp/changed" | grep -Evq '_test\.go$' && [ "$changelog_en" = 0 ]; then
  note "router code changed with no changelog entry; a change users can see gets one (/b4-changelog)"
fi
if grep -E '^hub/' "$tmp/changed" | grep -Evq '^hub/CHANGELOG\.md$|_test\.go$' && ! has '^hub/CHANGELOG\.md$'; then
  note "hub/ changed with no hub/CHANGELOG.md bullet (English only, its own version)"
fi
if has '^src/hubwire/'; then
  note "src/hubwire changed: it crosses router and hub, so changelog.md and hub/CHANGELOG.md each get a bullet"
fi

if has '^installer/|^install\.sh$'; then
  if git diff --quiet "$base" -- installer/_build.sh; then
    mkdir -p "$tmp/build"
    cp -R installer "$tmp/build/installer"
    if sh "$tmp/build/installer/_build.sh" > "$tmp/build/log" 2>&1; then
      if ! cmp -s "$tmp/build/install.sh" install.sh; then
        fail "install.sh is stale: run make build-installer and commit install.sh"
      fi
    else
      fail "installer/_build.sh failed:"
      detail "$tmp/build/log"
    fi
  else
    note "installer/_build.sh differs from $ref, so it was not run: read it first, then run make build-installer and compare install.sh"
  fi
  if ! sh -n install.sh 2> "$tmp/syntax"; then
    fail "install.sh does not parse:"
    detail "$tmp/syntax"
  fi
  if command -v busybox >/dev/null 2>&1 && ! busybox sh -n install.sh 2> "$tmp/syntax"; then
    fail "busybox sh does not parse install.sh:"
    detail "$tmp/syntax"
  fi
  awk -f "$skill_dir/posix-sh.awk" "$tmp/scan" > "$tmp/posix"
  awk -F '\t' '$1 == "FAIL" { print $2 ": " $3 }' "$tmp/posix" > "$tmp/posix_fail"
  awk -F '\t' '$1 == "NOTE" { print $2 ": " $3 }' "$tmp/posix" > "$tmp/posix_note"
  if [ -s "$tmp/posix_fail" ]; then
    fail "installer code that breaks on busybox or plain POSIX sh:"
    detail "$tmp/posix_fail"
  fi
  if [ -s "$tmp/posix_note" ]; then
    note "installer probes:"
    detail "$tmp/posix_note"
  fi
  if ! grep -Eq '^(src|hub)/' "$tmp/changed" && [ "$changelog_en" = 1 ]; then
    note "installer-only change: no changelog entry and no version bump, since install.sh is served from main"
  fi
  note "install.sh reaches every user the moment this merges: run it on a busybox box or in a busybox container first (/b4-installer)"
fi

if grep -q '\.go$' "$tmp/changed"; then
  if command -v go >/dev/null 2>&1; then
    if go -C src list -deps . 2>/dev/null | grep -q goleak; then
      fail "goleak is linked into the b4 binary; it belongs in _test.go files only (src/leaktest)"
    fi
    grep '\.go$' "$tmp/changed" | while IFS= read -r f; do
      if [ -f "$f" ]; then
        gofmt -l "$f"
      fi
    done > "$tmp/gofmt"
    if [ -s "$tmp/gofmt" ]; then
      fail "gofmt would rewrite:"
      detail "$tmp/gofmt"
    fi
  else
    note "go not found: skipped the goleak and gofmt checks"
  fi
fi

if has '^pnpm-lock\.yaml$'; then
  contexts=$(grep -cE "^  '@mui/material@[^']*\(" pnpm-lock.yaml)
  if [ "$contexts" -gt 1 ]; then
    fail "pnpm-lock.yaml resolves @mui/material in $contexts peer contexts: run pnpm dedupe (two MUI copies fail the hub's tsc and ship twice in the router UI)"
  fi
  note "lockfile changed: run pnpm dedupe after any lockfile change, Dependabot's included"
fi
if has '^src/(tables|tproxy|tun|ctnl)/'; then
  if ! has '^docs/docs/guides/marks\.md$'; then
    note "routing code changed: if a mark bit, ip rule priority or chain position changed, docs/docs/guides/marks.md and its RU mirror change in the same PR"
  fi
fi
if has '^src/config/'; then
  note "config changed: run make gen-defaults before the UI type check; a new or changed setting follows /b4-setting"
fi
if has '^src/http/handler/mcp'; then
  note "MCP tools changed: see /b4-mcp for the byte budget and client limits"
fi
if grep -E '^src/http/handler/.+\.go$' "$tmp/changed" | grep -Evq '_test\.go$'; then
  note "handler code changed: if API annotations changed, make swagger VERSION=<next> regenerates docs/static/swagger.json"
fi

echo
echo "Run next:"
suites=0
suite() {
  suites=$((suites + 1))
  printf '  %s\n' "$*"
}
if grep -Eq '^src/.+\.go$|^src/go\.(mod|sum)$' "$tmp/changed"; then
  suite "go -C src test ./..."
fi
if has '^src/(tables|tproxy|tun|ctnl)/'; then
  suite "make test-netns"
fi
if grep -Eq '^hub/.+\.go$|^hub/go\.(mod|sum)$|^src/hubwire/' "$tmp/changed"; then
  suite "go -C hub test ./..."
fi
if has '^(src/http/ui|shared/design)/|^pnpm-lock\.yaml$|^src/config/'; then
  suite "make gen-defaults && pnpm -C src/http/ui exec tsc -p tsconfig.json"
  suite "pnpm -C src/http/ui exec oxlint --type-aware --report-unused-disable-directives-severity=warn"
  suite "pnpm -C src/http/ui build"
fi
if has '^(hub/ui|shared/design)/|^pnpm-lock\.yaml$'; then
  suite "pnpm -C hub/ui typecheck"
  suite "pnpm -C hub/ui exec oxlint --type-aware --report-unused-disable-directives-severity=warn"
fi
if has '^docs/'; then
  suite "pnpm -C docs build"
fi
if [ "$suites" -eq 0 ]; then
  echo "  nothing beyond the checks above"
fi

printf '\nb4-check: %s failed, %s notes\n' "$fails" "$notes"
[ "$fails" -eq 0 ]
