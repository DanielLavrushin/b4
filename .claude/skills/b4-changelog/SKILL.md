---
name: b4-changelog
description: Write or edit an entry in b4's changelog.md and its Russian mirror changelog_ru.md, or in hub/CHANGELOG.md for the b4hub service, in the Keep a Changelog format. Use whenever a change needs to be recorded for a release, when asked to "add a changelog entry", when cutting a version, when editing existing changelog wording, or after changing anything under hub/.
---

# Changelog

b4's changelogs follow [Keep a Changelog 1.1.0](https://keepachangelog.com/en/1.1.0/). There
are three files:

- `changelog.md`: the router service, English.
- `changelog_ru.md`: its Russian mirror. Both files have the same versions, the same sections
  and the same number of bullets in each section. An entry that lands in one and not the
  other is a bug.
- `hub/CHANGELOG.md`: the b4hub service, English only, its own versions (see "Hub changelog").

## Who reads it

The reader is a b4 user deciding whether to update and wanting to know what they get. They
see a version's block in the web UI's update dialog, on the GitHub release page and on the
docs site. They do not read code, commits or PRs. Everything below follows from that.

## Keep a Changelog principles

- Changelogs are for humans, not machines. Never paste a commit log or a PR list.
- Every released version has a block. The newest is first.
- Each block carries its release date, `YYYY-MM-DD`.
- Changes are grouped by type under fixed section headings.
- Unreleased changes collect at the top under `## [Unreleased]`, so a reader can see what
  is coming and cutting a release is a rename.
- Versions are linkable: link references at the bottom of the file.
- Record only notable changes, but do record all of them: a changelog that hides a change
  users will notice, such as a removed option or a converted setting, is worse than none.
  "Notable" is the test, not a count: a release with many real changes has many bullets.

## File shape

```
# Changelog

All notable changes to b4 are recorded in this file. The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).

## [Unreleased]

### Fixed

- A set using **Combo** with an **Overlap Length** could break connections soon after they opened, because the overlap was added to later packets too.

## [1.90.0] - 2026-11-07

### Added

- **Discovery** finds the other blocked hosts a site's page needs: the new **Page hosts** step tests them, and **Apply as a set** adds the ticked ones.

[unreleased]: https://github.com/DanielLavrushin/b4/compare/v1.90.0...HEAD
[1.90.0]: https://github.com/DanielLavrushin/b4/compare/v1.89.1...v1.90.0
```

Hard format rules. Code reads these files, so they are not style choices:

- A version heading is exactly `## [x.y.z] - YYYY-MM-DD`: no `v`, no other text. The web UI
  on every installed b4 fetches `changelog_ru.md` from `main` and splits it on
  `## [version]`; the release workflow and the docs site read the same heading.
- `## [Unreleased]` is spelled in English in both files, never translated. The pre-merge
  check compares the top headings of the two files character for character.
- Section headings are `###`, in this order, each only when it has bullets:

  | English | Russian | What goes in |
  | --- | --- | --- |
  | `### Added` | `### Добавлено` | New features, controls, settings, platforms. |
  | `### Changed` | `### Изменено` | Deliberate changes to existing behaviour, defaults, UI locations, converted settings. |
  | `### Deprecated` | `### Устарело` | Something that still works but will be removed. |
  | `### Removed` | `### Удалено` | A feature, option, tab or platform that is gone. |
  | `### Fixed` | `### Исправлено` | Defects a user of the last stable release could hit. |
  | `### Security` | `### Безопасность` | Anything that exposed users or their data, fixed or hardened. |

- No `ADDED:` / `FIXED:` / `ИСПРАВЛЕНО:` prefixes on bullets: the section heading carries
  the type.
- One blank line around every heading and a blank line between a heading and its list.
- Link references live at the very bottom, newest first: `[unreleased]` compares the latest
  tag with `HEAD`, each version compares the previous tag with its own. A version without a
  git tag (1.76.3 was never tagged) gets no reference.

## What earns a bullet

Before writing, ask: would a user of the last stable release notice this, or need to do
something about it? If not, it gets no bullet.

In:

- New features, controls, settings and supported platforms.
- Behaviour a user will see change: a default, a UI location, a converted value, a removed
  option.
- Defects that shipped in a stable release and that a user could hit.
- Anything with a security impact.

Out:

- Refactors, tests, CI, build scripts, docs-only changes, code style.
- A fix for something that existed only in an rc or in unreleased work. Check each Fixed
  bullet against the last stable tag, not the rc tags.
- Installer-only changes (`installer/`, `install.sh`): `install.sh` is served from `main`,
  so they reach users when they land. They get no entry and no version bump. They may be
  mentioned inside a binary release's bullet they shipped with.
- Performance or memory changes nobody would notice.

**Collapse the small ones.** A narrow fix that few users meet does not get its own
bullet. Several of them in one area go into one bullet that names them briefly, keeping
every issue link and credit:

```
- AI apps connected over **MCP** could lose the connection before the tools appeared (Antigravity CLI), wait a minute behind nginx, or fail to stop a cancelled tool.
```

## Group by what the user gained

**A feature is one bullet, however many commits, tools or settings it took.** The unit is
what a user gained, not what was built. Name the capability once and list what it covers
inside that bullet.

- Merge every bullet that shares the sentence a user would say. A new permission, the tools
  it unlocks and the UI switch that turns it on are one bullet, not three.
- A defect found and fixed *while building the feature* belongs inside the feature's bullet.
  It was never shipped broken.
- Split only when a reader is genuinely different: something that changes behaviour for
  users who never touch the new feature earns its own bullet.
- Three symptoms of one root cause are one bullet.

There is no target count. A long block is fine when every bullet is a different notable
change; it is a problem when several bullets describe one feature or one root cause.

**Re-read the whole block, not the line you just added.** Entries get written one at a time,
in whichever session that piece landed, and each reads fine on its own. Bullets about the
same feature pile up unnoticed: 1.90.0 shipped six bullets for one IP Block Detection
problem and four for MCP. After any session that touches the changelog, re-read the whole
`[Unreleased]` block and re-group it. Merging is where errors
creep in: re-verify every merged bullet against its sources. The 1.83.0 regroup dropped a
condition, chained a cause onto the wrong symptom, gave a symptom to the wrong subject and
widened a scope.

## Writing a bullet

A bullet is one plain sentence, in the style of Keep a Changelog's own examples. There is
no bold summary followed by a paragraph: that form says everything twice.

```
- A set using **Combo** with an **Overlap Length** could break connections soon after they opened, because the overlap was added to later packets too.
```

- **One sentence by default.** For Fixed: the symptom the user saw, at most with a short
  cause. For Added, Changed, Removed: what the user gains or loses, with the UI path or the
  converted value they need. A second short sentence only when the user must know or do
  something more (Security bullets, a setting to change after updating). Never three.
- **Bold the b4 features and UI names** the bullet mentions, the way the docs do: **Discovery**,
  **Manual Bypass IPs**, **IP Block Detection**, **Escalation**, **MCP**, **Payloads**,
  **Settings, Core, Devices**. Use the UI's exact label. Bold only those names, never a whole
  phrase, a symptom or ordinary words.
- **Backticks only for literal values** a user would type or read: config keys such as
  `logging.instaflush`, header names such as `X-Forwarded-Proto`, file names, commands.
- Cut every clause that survives without changing what a user learns: mechanism
  walkthroughs, what else was tried, boundary caveats. Aim for under 200 characters, links
  excluded.

### A Fixed bullet describes only the problem

The bullet is under Fixed because it was fixed; the heading says so. The sentence is the
symptom the user saw, with at most its cause, and the bullet stops there. Never state
the remedy: not as a promise, not as a neutral present-tense sentence, not as a UI path
pointing at where a new warning appears. If a Fixed bullet feels unfinished, that is the
rule working: delete the last sentence.

Only Added, Changed, Removed and Deprecated bullets say what the thing does, because there
the change is the content. A Security bullet names the exposure; when users must act on
the change (set a login, pass a header through a proxy), a second sentence says what to do.

**Hard ban on promise closers.** Never write "now does X", "now works", "now correctly",
"is now", and never the Russian "Теперь". Dropping the "now" is not a way round this in a
Fixed bullet: "b4 waits for the lock itself" is the same remedy sentence with the temporal
marker filed off, and it goes too.

- Bad: `Test connections now use the version b4 is set to handle.`
- Bad: `Test connections use the version b4 is set to handle, and the log names it at the start of every run.`
- Good: `Test connections were made with whatever version the library defaulted to, so a run could pass on a protocol the router never speaks.`

### Voice

- Plain language, no marketing. No exclamation, no "seamlessly", no "powerful".
- No em-dash anywhere. Plain hyphen with spaces where a dash is needed.
- Describe behaviour, not code. "a set bound to a manually added source device stopped
  taking its route" beats "fixed nil deref in buildDeviceMatch".
- Write for non-technical users. Drop jargon such as RFC numbers, CGNAT, TSPU, SNI, DoH or
  TLS handshake stages, and describe the effect a user sees. Use the UI's own feature names
  (**DPI Detector**) and refer to UI locations the way the UI labels them ("under
  **Settings, Core, Devices**").
- b4 is a "service", not a "program".

Example of the density to aim for:

- Bad: `- **A set with Domain-only matching on applied only to the start of each connection** - the rest of the connection went out without the set's strategy and showed no set on the Traffic page. Where several sets share an address, those packets could get another set's strategy instead. b4 picked the set for them by address, and a domain-only set does not track addresses.`
- Good: `- A set with **Domain-only matching** on applied its strategy only to the start of each connection, and the rest showed no set on the **Traffic** page.`

## Link the fixed issue

When the work comes from a pull request or task with linked GitHub issues, and you know
which issue a bullet resolves, end that bullet with a link to the issue, after the last
sentence, separated by a space:

```
- With the UDP action **Coalesce QUIC Initial**, sites did not open over QUIC in Chrome, because the padding went in front of every handshake packet. [#427](https://github.com/DanielLavrushin/b4/issues/427)
```

- Full URL, `[#N](https://github.com/DanielLavrushin/b4/issues/N)`, never a bare `#N`:
  the changelog is read outside GitHub, in the update dialog and the docs.
- Several issues behind one bullet: list them all, comma-separated,
  `[#448](...), [#449](...)`.
- Two bullets that each fix part of the same issue both carry its link.
- The Russian bullet carries the same links in the same place.
- Link only issues the change actually resolves. Find them in the PR's closing keywords
  (`gh pr view <n> --json closingIssuesReferences`), the task brief or the issue you were
  given. Do not guess a number, and do not link an issue the change only touches on.
- An Added or Changed bullet that answers a feature request links it the same way.
- No issue, no issue link.

## Credit a contributor's pull request

When the change comes from someone else's pull request, credit its author at the end of the
bullet: the PR link, `thanks` and the author's GitHub profile, in parentheses. Issue links
go in the same parentheses after a semicolon:

```
- Connections that another service on the router had marked, such as those MagiTrickle routes, got no strategy from b4's sets, because b4 took those marks for its own. ([#422](https://github.com/DanielLavrushin/b4/pull/422), thanks [@asovetov](https://github.com/asovetov); [#276](https://github.com/DanielLavrushin/b4/discussions/276))
```

- Find the author with `gh pr view <n> --json author`. Use the GitHub login as shown,
  `[@login](https://github.com/login)`.
- Russian writes `спасибо` in place of `thanks`; links and logins are identical.
- A contributor PR that was finished or taken over on our own branch still credits the
  original author and links their PR.
- Several contributors on one bullet: `thanks [@a](...), [@b](...)`.
- The owner's own PRs (DanielLavrushin) get no credit and no PR link: only the issue link,
  as above.
- Regrouping or shortening never drops a credit or an issue link. A merged bullet carries
  the links of every bullet it absorbed.

## Russian

Mirror the English meaning, do not translate word for word. The Russian file has the same
versions, the same section headings (from the table above) and the same number of bullets in
each section, in the same order.

Follow the project's fixed choices: a routing set is "сет"; Discovery the feature is
"Дискавери", while the act of searching stays "поиск"; payloads, strategy family names and
technical terms stay in English (TLS, SNI, DPI, QUIC, ASN, MTProto, MSS, SOCKS5, NFQUEUE).
The short-sentence rule holds in Russian too: no chains of «который», «где», «при этом» or
stacked participle phrases. UI names are bold, as in English, with the label exactly as
`src/http/ui/src/i18n/ru.json` has it and no «» quotes around it: **Обнаружение блокировки
по IP**, **Ручные IP обхода**, **Дискавери**.

## Procedure: adding an entry

1. Read the `[Unreleased]` block of `changelog.md` to match the voice and see what is there.
   If there is no `[Unreleased]` heading, add one above the newest version in both files.
   During a release candidate cycle, a change that ships in that version goes into its
   `## [x.y.z]` block instead (see "Cutting a release").
2. Decide whether the change earns a bullet at all ("What earns a bullet"). If it belongs
   to something already listed, extend that bullet instead of adding one.
3. Pick the section. Changed is for a deliberate behaviour change that was not itself a
   defect. Security beats Fixed when users or their data were exposed.
4. Write the English bullet. Scan it for "now" and for a present-tense verb that states the
   fixed behaviour as a closer (works, loads, stays, shows, stops, applies, saves) and
   delete that clause.
5. Add issue links and contributor credit if they apply.
6. Write the Russian bullet at the mirrored position in `changelog_ru.md` and scan it the
   same way ("Теперь", работает, загружается, остаётся, показывается).
7. Re-read the whole `[Unreleased]` block in both files and re-group if it drifted.
8. Verify both files have the same sections and bullet counts under `[Unreleased]`.

## Procedure: cutting a release

A release and its release candidates share one block. In both `changelog.md` and
`changelog_ru.md`, in one commit before the first rc, or before the release when there is
no rc:

1. Re-read and re-group the `[Unreleased]` block against the grouping rules above.
2. Rename `## [Unreleased]` to `## [x.y.z] - YYYY-MM-DD` and add an empty `## [Unreleased]`
   above it (an empty Unreleased has no sections).
3. At the bottom, point `[unreleased]` at `compare/vx.y.z...HEAD` and add
   `[x.y.z]: https://github.com/DanielLavrushin/b4/compare/v<previous>...vx.y.z` under it.

Between the first rc and the release, a change that ships in x.y.z goes into the
`## [x.y.z]` block, not into `[Unreleased]`. At the release, set the block's date to the
release day.

The release workflow publishes the `## [x.y.z]` block as the GitHub release text, for an rc
(`1.91.0rc1` uses `## [1.91.0]`) as well as for the release, and fails when the block is
missing. The block has to exist before an rc because the update dialog of every b4 since
1.79.0 shows a Russian user the `changelog_ru.md` block of the base version: an rc whose
notes were still under `[Unreleased]` would show the English release text instead. A
prerelease never gets a heading of its own: `## [1.91.0rc1]` fails the pre-merge check.

## Delegating

If changelog writing is delegated to a subagent, its brief must carry verbatim: the
short-sentence rule, the Fixed-describes-only-the-problem rule, the promise-closer ban, the
"what earns a bullet" lists, and the rule that no issue link or credit is dropped. A brief
that carried only the closer ban produced four-to-six-sentence bullets.

## Hub changelog

`hub/CHANGELOG.md` is the b4hub service's changelog, in the same format with these
differences:

- English only. There is no Russian mirror.
- Its own version, independent of b4: semver from 1.0.0, `## [x.y.z] - YYYY-MM-DD`. New
  capabilities bump the minor version, fixes alone the patch.
- **No `[Unreleased]` section.** The release workflow in the b4hub repository takes the top
  `## [` block and refuses the release unless its heading carries the version being
  released. Write changes straight into the next version's block. If the top block is
  already released (its version exists at github.com/DanielLavrushin/b4hub/releases), start
  a new block above it.
- Every change under `hub/` (Go code, the console in `hub/ui`, the mirror, the `b4hub` CLI,
  the store schema) or to the deploy bundle in the b4hub repo gets a bullet in the same
  session.
- Link references at the bottom compare the b4hub repository's tags:
  `[x.y.z]: https://github.com/DanielLavrushin/b4hub/compare/v<previous>...vx.y.z`.
- A change that crosses both sides, such as the wire format in `src/hubwire` or the router's
  hub client together with the hub, gets a bullet in each changelog, each written for its
  reader: router users in `changelog.md`, hub operators in `hub/CHANGELOG.md`.
