#!/bin/sh

set -u

ROOT=$(git rev-parse --show-toplevel 2>/dev/null) || {
    echo "not inside a git repository" >&2
    exit 1
}
MAIN=$(git -C "$ROOT" worktree list --porcelain | sed -n '1s/^worktree //p')
WT_DIR="$MAIN/.claude/worktrees"
APPLY=0
BRANCHES=1

usage() {
    echo "usage: $0 [--apply] [--keep-branches]"
    echo
    echo "  default          dry run: list what would be removed and what would be kept"
    echo "  --apply          remove the clean worktrees and delete their merged wt-* branches"
    echo "  --keep-branches  remove worktrees only, never touch branches"
    echo
    echo "a worktree with uncommitted changes is kept. A branch git does not consider"
    echo "merged is kept (git branch -d), so commits that exist only locally survive."
}

for a in "$@"; do
    case "$a" in
        --apply) APPLY=1 ;;
        --keep-branches) BRANCHES=0 ;;
        -h | --help) usage; exit 0 ;;
        *) usage >&2; exit 2 ;;
    esac
done

cd "$MAIN" || exit 1

removed=0
kept=0

if [ -d "$WT_DIR" ]; then
    for w in "$WT_DIR"/*/; do
        [ -d "$w" ] || continue
        n=$(basename "$w")
        dirty=$(git -C "$w" status --porcelain 2>/dev/null | wc -l | tr -d ' ')
        if [ "$dirty" != "0" ]; then
            echo "keep    $n: $dirty uncommitted file(s)"
            kept=$((kept + 1))
            continue
        fi
        local_only=$(git -C "$w" log --oneline HEAD --not --remotes 2>/dev/null | wc -l | tr -d ' ')
        if [ "$APPLY" = 1 ]; then
            if git worktree remove "$w" 2>/dev/null; then
                echo "removed $n (commits not on any remote: $local_only)"
                removed=$((removed + 1))
            else
                echo "keep    $n: git refused to remove it"
                kept=$((kept + 1))
            fi
        else
            echo "remove  $n (commits not on any remote: $local_only)"
            removed=$((removed + 1))
        fi
    done
fi

git worktree prune

if [ "$BRANCHES" = 1 ]; then
    current=$(git rev-parse --abbrev-ref HEAD)
    for b in $(git branch --format='%(refname:short)' | grep '^wt-'); do
        [ "$b" = "$current" ] && continue
        if git worktree list --porcelain | grep -q "^branch refs/heads/$b$"; then
            echo "keep    branch $b: checked out in a worktree"
            continue
        fi
        if [ "$APPLY" = 1 ]; then
            if git branch -d "$b" >/dev/null 2>&1; then
                echo "deleted branch $b"
            else
                echo "keep    branch $b: not merged"
            fi
        else
            if git merge-base --is-ancestor "$b" HEAD 2>/dev/null; then
                echo "delete  branch $b"
            else
                echo "keep    branch $b: not merged"
            fi
        fi
    done
fi

if [ "$APPLY" = 0 ]; then
    echo
    echo "dry run, nothing changed. Run with --apply to do it."
fi
