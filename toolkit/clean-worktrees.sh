#!/bin/sh

set -u

ROOT=$(git rev-parse --show-toplevel 2>/dev/null) || {
    echo "not inside a git repository" >&2
    exit 1
}
MAIN=$(git -C "$ROOT" worktree list --porcelain | sed -n '1s/^worktree //p')
APPLY=0
BRANCHES=1

usage() {
    echo "usage: $0 [--apply] [--keep-branches]"
    echo
    echo "  default          dry run: list what would be removed and what would be kept"
    echo "  --apply          remove the clean worktrees and delete their branches"
    echo "  --keep-branches  remove worktrees only, never touch branches"
    echo
    echo "every worktree in 'git worktree list' except the main checkout is considered,"
    echo "wherever it lives. A worktree is kept when it has uncommitted changes, is locked,"
    echo "is the one you run this from, contains another worktree, or is on a detached HEAD"
    echo "with commits that are on no remote. A branch is deleted only when every commit"
    echo "on it is on a remote, so nothing that exists only locally is lost. main and"
    echo "release/* are never deleted. It fetches all remotes first; if that fails, no"
    echo "branch is deleted."
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

remotes_ok=1
if ! git fetch --all --prune --quiet; then
    remotes_ok=0
    echo "fetch failed: cannot tell which commits are on a remote, so every branch and"
    echo "every detached worktree with commits is kept"
    echo
fi

LIST=$(mktemp) || exit 1
trap 'rm -f "$LIST"' EXIT
git worktree list --porcelain > "$LIST"
echo >> "$LIST"

removed=0
kept=0
pruned_branches=""
prune_ok=1

label() {
    case "$1" in
        "$MAIN"/*) echo "${1#"$MAIN"/}" ;;
        *) echo "$1" ;;
    esac
}

keep() {
    echo "keep    $1"
    kept=$((kept + 1))
}

protected() {
    case "$1" in
        main | release/*) return 0 ;;
        *) return 1 ;;
    esac
}

local_only() {
    [ "$remotes_ok" = 1 ] || { echo "?"; return 0; }
    git rev-list --count "$1" --not --remotes 2>/dev/null || echo "?"
}

drop_branch() {
    b=$1
    [ "$BRANCHES" = 1 ] || return 0
    if protected "$b"; then
        echo "keep    branch $b: protected"
        return 0
    fi
    n=$(local_only "refs/heads/$b")
    if [ "$n" != "0" ]; then
        echo "keep    branch $b: $n commit(s) on no remote"
        return 0
    fi
    if [ "$APPLY" = 1 ]; then
        if git branch -D "$b" >/dev/null 2>&1; then
            echo "deleted branch $b"
        else
            echo "keep    branch $b: git refused to delete it"
        fi
    else
        echo "delete  branch $b"
    fi
}

nested() {
    sed -n 's/^worktree //p' "$LIST" | while IFS= read -r other; do
        case "$other" in
            "$1"/*) echo "$other"; break ;;
        esac
    done
}

gone() {
    w=$1 head=$2 branch=$3 detached=$4 locked=$5 name=$6
    if [ "$locked" = 1 ]; then
        keep "$name: directory is gone, but the entry is locked"
        return 0
    fi
    if [ "$detached" = 1 ]; then
        n=$(local_only "$head")
        if [ "$n" != "0" ]; then
            rescue="rescued/$(basename "$w")"
            if [ "$APPLY" = 0 ]; then
                echo "prune   $name: directory is gone, $n commit(s) on no remote go to branch $rescue"
            elif git branch "$rescue" "$head" >/dev/null 2>&1; then
                echo "prune   $name: directory is gone, $n commit(s) on no remote saved in branch $rescue"
            else
                prune_ok=0
                keep "$name: directory is gone and branch $rescue could not be created, so nothing is pruned"
            fi
            return 0
        fi
    fi
    echo "prune   $name: directory is gone"
    [ -n "$branch" ] && pruned_branches="$pruned_branches $branch"
    return 0
}

handle() {
    w=$1 head=$2 branch=$3 detached=$4 locked=$5 prunable=$6
    [ -n "$w" ] || return 0
    [ "$w" = "$MAIN" ] && return 0
    name=$(label "$w")
    if [ "$prunable" = 1 ] || [ ! -d "$w" ]; then
        gone "$w" "$head" "$branch" "$detached" "$locked" "$name"
        return 0
    fi
    if [ "$w" = "$ROOT" ]; then
        keep "$name: you are running this from it"
        return 0
    fi
    if [ "$locked" = 1 ]; then
        keep "$name: locked"
        return 0
    fi
    inner=$(nested "$w")
    if [ -n "$inner" ]; then
        keep "$name: contains worktree $(label "$inner")"
        return 0
    fi
    dirty=$(git -C "$w" status --porcelain 2>/dev/null | wc -l | tr -d ' ')
    if [ "$dirty" != "0" ]; then
        keep "$name: $dirty uncommitted file(s)"
        return 0
    fi
    n=$(local_only "$(git -C "$w" rev-parse HEAD)")
    if [ "$detached" = 1 ] && [ "$n" != "0" ]; then
        keep "$name: detached HEAD with $n commit(s) on no remote"
        return 0
    fi
    what=${branch:-detached}
    if [ "$APPLY" = 1 ]; then
        if git worktree remove "$w" 2>/dev/null; then
            echo "removed $name ($what, commits on no remote: $n)"
            removed=$((removed + 1))
        else
            keep "$name: git refused to remove it"
            return 0
        fi
    else
        echo "remove  $name ($what, commits on no remote: $n)"
        removed=$((removed + 1))
    fi
    [ -n "$branch" ] && drop_branch "$branch"
    return 0
}

wt="" hd="" br="" det=0 lck=0 prn=0
while IFS= read -r line; do
    case "$line" in
        "worktree "*) wt=${line#worktree } ;;
        "HEAD "*) hd=${line#HEAD } ;;
        "branch refs/heads/"*) br=${line#branch refs/heads/} ;;
        detached) det=1 ;;
        locked | "locked "*) lck=1 ;;
        prunable | "prunable "*) prn=1 ;;
        "")
            handle "$wt" "$hd" "$br" "$det" "$lck" "$prn"
            wt="" hd="" br="" det=0 lck=0 prn=0
            ;;
    esac
done < "$LIST"

if [ "$APPLY" = 1 ] && [ "$prune_ok" = 1 ]; then
    git worktree prune
fi
if [ "$prune_ok" = 1 ]; then
    for b in $pruned_branches; do
        drop_branch "$b"
    done
fi

if [ "$BRANCHES" = 1 ]; then
    checked_out=$(git worktree list --porcelain | sed -n 's/^branch refs\/heads\///p')
    for b in $(git branch --format='%(refname:short)' | grep '^worktree-'); do
        echo "$checked_out" | grep -qx "$b" && continue
        drop_branch "$b"
    done
fi

echo
echo "worktrees: $removed to remove, $kept kept"
if [ "$APPLY" = 0 ]; then
    echo "dry run, nothing changed. Run with --apply to do it."
fi
