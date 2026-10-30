---
name: b4-worktree
description: Set up a separate git worktree for a b4 task, pull request or issue - the naming (wt-<slug>, wt-pr-<n>, wt-issue-<n>), creating it or re-entering an existing one, renaming its branch, what a fresh worktree lacks (node_modules, generated UI defaults) and how to fill it, the limits of a worktree session, and leaving it in place when done. Use when asked to work "in a worktree" or "in a separate worktree", and before starting on a PR or an issue.
argument-hint: "<slug> | pr <n> | issue <n>"
---

# Worktrees

## Name

| For | Directory and branch |
| --- | --- |
| A task or an investigation | `wt-<short-kebab-slug>` of the request: `wt-dns-leak`, `wt-tun-mss` |
| A pull request | `wt-pr-<n>` |
| An issue | `wt-issue-<n>` |

The directory is `.claude/worktrees/<name>` in the repository. A path the user gives wins.

## Create or re-enter

1. `git worktree list`. If the worktree exists, enter it (Claude Code: `EnterWorktree` with
   `path`) and run `git status` first: it may hold someone's uncommitted work. For a PR, run
   the checkout of step 4 again to get its current head.
2. Otherwise create it with Claude Code's `EnterWorktree` (`name: "wt-<name>"`), not
   `git worktree add`. Only Claude Code's own creation copies the gitignored files listed in
   `.worktreeinclude`: both `.env` files (the root one and `src/http/ui/.env`), `.mcp.json`,
   `.claude/settings*.json` and the built UI in `src/http/ui/dist/`, which `go build` embeds.
   The new worktree starts from `origin/main`, on a branch named `worktree-wt-<name>`.
3. Rename that branch before anything else: `git branch -m wt-<name>`. The `wt-` prefix already
   says worktree.
4. For a PR, `gh pr checkout <n> --detach` inside the worktree. Leave out `--detach` only when
   asked to commit to the PR's own branch; that fails while another worktree has the branch
   checked out. For an issue, stay on `origin/main` and read it with
   `gh issue view <n> --comments`.

## Fill it

- `pnpm install --frozen-lockfile --prefer-offline` at the worktree root: `node_modules` is
  never copied, and pnpm links from its store in seconds. `docs/` is its own workspace and
  needs its own `pnpm -C docs install --frozen-lockfile` before a docs build.
- `make gen-defaults` before a UI type check or a vite run: its output is gitignored.
- The shared `b4-*` skills come with git. A maintainer's private skills appear as symlinks
  inside `.claude/skills/`, which git ignores.

## Inside a worktree session

- Claude Code refuses commands it cannot prove stay inside the worktree. Spell paths out, keep
  shell variables and command substitutions out of arguments, and run one plain command per
  call; a multi-step script goes into a file that is then run by its path. If a hook rewrites
  `git` into a wrapper, call `/usr/bin/git`.
- A code index built for the main checkout answers with the main checkout's code, which may be
  another branch. Read a file before editing it.
- `go version -m` stamps a worktree build with the main checkout's commit, because Go takes
  version-control data only from a `.git` directory. Tell builds apart by
  `-X main.Version=...`, which the Makefile sets from `VERSION`.
- Never copy the repository with `cp -r`: `node_modules` can exhaust a tmpfs's inodes. A
  worktree, a gitignore-filtered `rsync` or `git archive` does the job.

## When done

Creating the worktree and its branch is part of the job; committing, pushing and removing it
are not, unless asked. Leave it in place and give its path. When asked to remove it:
`git worktree remove .claude/worktrees/<name>`, then `git branch -D <name>` if nothing else
uses the branch.
