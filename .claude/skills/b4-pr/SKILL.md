---
name: b4-pr
description: Work on a b4 pull request - check it out in its own worktree, review a contributor's change against b4's rules and area traps, work through review comments (Copilot's included), and when asked finish it - merge main in, resolve conflicts, push - or take over an abandoned contributor PR. Use when given a PR number or link, or asked to review, check, finish, merge or fix a PR, or to go through its review comments.
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
- Run `/b4-check` against the base. A branch cut before the check script existed does not
  contain it: run the main checkout's copy by its path from inside the PR worktree; it checks
  the repository it is run in.
- Read every changed file and load the skill of each area it touches: `/b4-tables`,
  `/b4-setting`, `/b4-ui`, `/b4-mcp`, `/b4-installer`, `/b4-hub`.
- What contributor PRs most often miss: the Russian side of strings and docs, the second
  changelog, `marks.md` after a rule change, a test that fails without the fix, code comments
  and em-dashes.
- Report findings by severity, each with `file:line`, the failure it causes and the fix, then
  what blocks the merge.

## 3. Review comments

For a GitHub Copilot round, use the `copilot-pr-review` skill when it is installed: it fetches
the threads and the findings that live only in the review body, and keeps fixes local until a
single push, because Copilot reviews again on every push. Without it, read
`gh api repos/<owner>/<repo>/pulls/<n>/comments` and the review bodies.

Every comment, from a bot or a person:

1. Reproduce or probe it, with a test or a stress run, before deciding.
2. If it is real, fix it and add a regression test that fails without the fix.
3. If it is not, give a short reasoned skip.

b4 specifics:

- A suggestion to add a code comment or docstring is never applied: the repo has no comments.
  When the behaviour is intentional, the reasoning goes into the reply.
- A suggestion to add tests gets a recommendation, yes or no with a reason; the maintainer
  decides.
- Advice against a repo rule (comments, em-dash, both locales, POSIX shell, docs voice) is a
  skip that names the rule.

End with one table: comment, verdict, action.

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
