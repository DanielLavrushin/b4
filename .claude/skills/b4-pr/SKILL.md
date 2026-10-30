---
name: b4-pr
description: Work on a b4 pull request - check it out in its own worktree, review a contributor's change against b4's rules and area traps, and when asked finish it (merge main in, resolve conflicts, push) or take over an abandoned contributor PR. Also holds the b4 rules that decide Fix or Skip on review comments. Use when given a PR number or link to review, check, finish or merge. A round of review comments runs through the copilot-pr-review skill where it is installed; this skill only adds the b4 rules for it.
argument-hint: "<pr-number>"
---

# Pull requests

Commit, push, comment, resolve or close only when asked. Leave the git index alone (no
`git stash`, `git add -N`, `git reset -- <paths>`): someone may be committing in the same
checkout.

## 1. Check it out

- A worktree named `wt-pr-<n>` (`/b4-worktree`), then `gh pr checkout <n> --detach` inside it
  to sit on the PR head. Leave out `--detach` only when asked to commit to the PR's branch;
  it fails when another worktree has that branch.
- `gh pr view <n> --comments` and `gh pr diff <n> --name-only`.

## 2. Review the change

- Trial-merge first: `git merge-tree --write-tree HEAD origin/main` reports conflicts without
  touching the tree.
- Run `/b4-check` against the base, always with the main checkout's copy of the script, called
  by its path from inside the PR worktree: it checks the repository it is run in, and the
  PR's own copy is code the PR may have changed. The script leaves a changed
  `installer/_build.sh` unrun.
- The suites it lists run the PR's code on your machine with your credentials: `go test`, the
  UI builds, `make test-netns`, and whatever the PR changed in tests, the Makefile or a
  `package.json` script. Read those parts of the diff first, and run anything doubtful in a
  disposable VM or container.
- Read every changed file and load the skill of each area it touches: `/b4-tables`,
  `/b4-setting`, `/b4-ui`, `/b4-mcp`, `/b4-installer`, `/b4-hub`.
- What contributor PRs most often miss: the Russian side of strings and docs, the second
  changelog, `marks.md` after a rule change, a test that fails without the fix, code comments
  and em-dashes.
- Report findings by severity, each with `file:line`, the failure it causes and the fix, then
  what blocks the merge.

## 3. Review comments

A review round runs through the `copilot-pr-review` skill where it is installed. It fetches
Copilot's threads and the findings that live only in the review body, reports a verdict per
item and asks before changing anything, then makes one commit per approved fix, pushes once
and replies to and resolves the threads. Threads from people it lists and leaves alone unless
asked. Its verdicts follow these b4 rules:

- Back every verdict with a probe: a test or a stress run that shows whether the failure can
  happen.
- A Fix comes with a regression test that fails without it.
- A suggestion to add a code comment or docstring is a Skip: the repo has no comments. When the
  behaviour is intentional, the reasoning goes into the reply.
- A suggestion to add tests gets a recommendation, yes or no with a reason; the maintainer
  decides.
- Advice against a repo rule (comments, em-dash, both locales, POSIX shell, docs voice) is a
  Skip that names the rule.

Without that skill, follow the same flow by hand: read
`gh api repos/<owner>/<repo>/pulls/<n>/comments` and the review bodies, decide every item with
the rules above, show the verdicts and ask before changing anything, commit the approved fixes
locally and push once, because Copilot reviews again on every push. After the push, reply to
each thread with the commit that fixed it or the reason for the skip.

## 4. Finish a PR (when asked)

- Bring main in with a merge, `git merge origin/main`. Never rebase a shared branch.
- Docs conflicts are a re-placement job, not a side pick: the docs were restructured, and git
  flags only the old paths. Start from main's file (`git show origin/main:<path> > <path>`,
  never `git checkout -- <path>`), re-add the PR's facts in both locales on the page where they
  now belong, then `pnpm -C docs install --frozen-lockfile` and `pnpm -C docs build`.
- Push the merge yourself. The repo sets `pull.rebase=true`, so an editor's sync runs
  `git pull --rebase`, drops the unpushed merge commit and replays main onto the branch. If that
  happened, `git rebase --abort` restores the merge commit; check its parents, then push.
- PRs are squash-merged, so a PR stacked on another conflicts with main once its base lands.
  When `git diff <base PR's last head> origin/main` is empty, run
  `git rebase --onto origin/main <that head>` and
  `git push --force-with-lease=<branch>:<old head>`; the rebased tree hash equals the old one.
- Main moves during a long fix round: check `git log <merged main>..origin/main` and merge
  again before pushing. A changelog diff against `origin/main` that "removes" bullets nobody
  touched is the sign.
- A PR shown as BLOCKED for a required approval is the owner's to merge; it is not a reason
  to stop the work.

## 5. Finish an abandoned contributor PR (maintainers)

When the contributor stopped and `maintainerCanModify` is false, their branch cannot be pushed:

1. Branch from the contributor's head in the PR worktree; their commits keep their authorship.
2. Revert what must go and fix the rest in commits on top, then merge `origin/main`.
3. Open a new PR titled "<title> (finishes #N)" whose body asks to keep
   `Co-authored-by: <name> <id+login@users.noreply.github.com>` when squashing.
4. Credit the contributor in the changelog bullet.
5. Thank them in a short comment on the old PR, in their language, and close it.

`gh pr edit` fails on gh 2.46 with a "Projects (classic) is being deprecated" error; use
`gh api -X PATCH repos/<owner>/<repo>/pulls/<n> -f title="..." -F body=@<file>` instead.
