---
name: b4-check
description: Run b4's pre-merge checks on the current branch - the mechanical rules reviews keep catching (no code comments, no em-dash, en/ru pairs for UI strings, docs and changelog, a rebuilt install.sh, busybox-safe installer code, gofmt, goleak out of the binary, one MUI copy in the lockfile) plus the test, type-check, lint and build suites for the areas the diff touches. Use before a push, before opening or updating a pull request, when asked whether a change is ready or "check my changes", and after finishing any change in this repo.
argument-hint: "[base-ref]"
---

# Pre-merge check

No GitHub workflow runs on pull requests. `go test` for `src/` and `hub/` and the router UI's
vite build run only inside the release workflow, the docs build only after a push to `main`,
and nothing in this repository's CI runs `tsc`, oxlint or `make test-netns`. A type error, a
broken routing rule or a missing Russian string merges silently. This check is the gate.

## 1. Mechanical checks

```sh
sh .claude/skills/b4-check/check.sh            # against the merge base with origin/main
sh .claude/skills/b4-check/check.sh <ref>      # a stacked branch: pass its base
```

It compares the working tree with the merge base, so commits, staged, unstaged and untracked
files all count. Every `FAIL` gets fixed. Every `NOTE` gets a decision that the report states
("changelog: internal refactor, no entry"). A false positive is possible in two places: a
string literal that contains ` //`, and a shell line that only mentions a forbidden word. Say
so instead of contorting the code.

| Check | Rule |
| --- | --- |
| Em-dash | None in added lines anywhere: code, docs, changelog, strings. Plain hyphen. Older lines that have one are left alone. |
| Comments | None in added Go, TS/TSX/JS, shell, YAML, Makefile or Dockerfile lines. Names and structure carry the meaning; explanations go in the commit message or the PR. `//go:` directives are code, and so are the swag annotations in Go (`// @Param`, `// @Router` and the rest) that `make swagger` turns into `docs/static/swagger.json`. |
| UI strings | `en.json` and `ru.json` carry the same keys, in `src/http/ui/src/i18n/` and `hub/ui/src/i18n/`. Plurals: English `_one`/`_other` (a bare key stands in for `_other`), Russian `_one`/`_few`/`_many`. |
| Docs | A page under `docs/docs/` has its mirror under `docs/i18n/ru/docusaurus-plugin-content-docs/current/`. A change to one side only is a NOTE: a correction in one locale applies to the other. |
| Changelog | `changelog.md` and `changelog_ru.md` change together, with the same top heading and bullet count. `hub/` changes get a `hub/CHANGELOG.md` bullet. |
| Installer | `install.sh` matches a fresh `make build-installer`, parses under `sh -n` and `busybox sh -n`, and added installer lines use no bashisms, `grep -P`, `\s`, `\d`, `od`, `hexdump` or `xxd`. A changed `installer/_build.sh` is not run: it is code from the branch under review, so a NOTE asks to read it first. |
| Go | `gofmt -l` is empty for changed files; `goleak` is not linked into the b4 binary. |
| Lockfile | `@mui/material` resolves in one peer context. Two contexts fail the hub's `tsc` and ship two MUI copies in the router UI; `pnpm dedupe` fixes it. |

## 2. Suites

Run every command the script prints under `Run next:`, from the repo root, one per call, and
wait for each result.

- A fresh worktree has no `node_modules`: run `pnpm install --frozen-lockfile --prefer-offline`
  at the root first. `docs/` is its own workspace: `pnpm -C docs install --frozen-lockfile`
  before its build.
- `make gen-defaults` comes before the router UI's `tsc`. Its output,
  `src/http/ui/src/models/defaults.json`, is gitignored, so a fresh checkout lacks it.
- `go -C src test ./...` takes several minutes. Give it a long timeout rather than a filter.
- `make test-netns` builds the routing and conntrack tests and runs them in an unprivileged
  network namespace (`unshare -Urnm`). Where user namespaces are off, report it as not run
  and why; never as passed.
- The oxlint command printed checks only. `pnpm lint` in either UI runs oxlint with `--fix` and
  rewrites files. oxlint prints nothing when clean.
- `pnpm -C docs build` fails on a broken link or anchor. Only one Docusaurus process at a time
  in `docs/`: a dev server and a build share `docs/.docusaurus/`.

## 3. What the script cannot see

Read the diff for these, and load the matching skill when the change is in its area:

- A new, renamed or retyped config field: `/b4-setting`.
- A rule, mark, ip rule or chain change in `src/tables`, `src/tproxy` or `src/tun`: `/b4-tables`.
- Installer changes: `/b4-installer`. MCP tools: `/b4-mcp`. `hub/`: `/b4-hub`. Web UI: `/b4-ui`.
- A committed file derived from another committed file, such as a gzipped or bundled copy. It
  is generated in every build path instead (Makefile, Dockerfile), with a committed placeholder
  so plain `go build` still compiles.
- The two locales of a string or page say the same thing. The key check only compares keys.

## 4. Report

One table: check, result (pass, fail, not run), and for a fail the first lines of output or
for a not-run the reason. A suite that did not run is never reported as passing. End with the
list of what has to change before the branch can merge, or say plainly that nothing does.
