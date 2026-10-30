---
name: b4-changelog
description: Write or edit an entry in b4's changelog.md and its Russian mirror changelog_ru.md, or in hub/CHANGELOG.md for the b4hub service. Use whenever a change needs to be recorded for a release, when asked to "add a changelog entry", when cutting a version, when editing existing changelog wording, or after changing anything under hub/.
---

# Changelog entry

b4 keeps two changelogs that must stay in lockstep: `changelog.md` (English) and
`changelog_ru.md` (Russian). An entry that lands in one and not the other is a bug.

## Shape

```
## [1.79.0] - 2026-08-18

- ADDED: **One-line summary in bold** - the rest of the paragraph, plain prose.
- FIXED: **One-line summary in bold** - the rest of the paragraph, plain prose.
- CHANGED: **One-line summary in bold** - the rest of the paragraph, plain prose.
```

Russian uses `ДОБАВЛЕНО:`, `ИСПРАВЛЕНО:`, `ИЗМЕНЕНО:` in the same order, same bullets,
same count. Newest version at the top, under the `# B4 - Bye Bye Big Bro` heading.

One bullet per feature or fix. Do not split one change across several bullets, and do not
bundle unrelated changes into one.

## Collapse a release's worth of work into few bullets

**A feature is one bullet, however many commits, tools or settings it took.** The unit is
what a user gained, not what was built. Work that took a week of changes across a dozen
files is still one bullet if a user would describe it in one sentence.

The failure mode to avoid: shipping a subsystem and writing a bullet per piece of it. Nine
bullets about one MCP release is nine chances for a reader to give up. Name the capability
once and list what it covers inside that bullet.

Before writing, group everything in the release by the sentence a user would say. Then:

- Merge every bullet that shares a sentence. A new permission, the tools it unlocks and the
  UI switch that turns it on are one bullet, not three.
- A defect found and fixed *while building the feature* belongs inside the feature's bullet,
  not beside it. It was never shipped broken.
- Split only when a reader is genuinely different: something that changes behaviour for
  users who never touch the new feature earns its own bullet, because they need to see it
  and the feature's audience does not.
- Prefer the fix over the mechanism. Three symptoms of one root cause are one bullet.

A release with more than about six bullets usually means the grouping was not done. Ten is
almost always wrong.

**Re-read the whole block, not the line you just added.** This is where it goes wrong every
time: entries get written one at a time, in whichever session that piece landed, and each
reads fine on its own. The count creeps up and nobody sees it until the release is
unreadable - 1.79.0 drifted on bullet length, one MCP release reached 12 bullets, 1.80.0
reached 31. After any session that touches the changelog, re-read the entire version block
and re-group it.

When regrouping a release:

- Check each FIXED bullet against the last stable tag, not the rc tags. A symptom that only
  existed in an rc (a counter or card added after the tag) was never shipped and goes.
- Group by reader: several symptoms for the same reader (TUN users, interface-set users)
  can share one bullet; a different reader gets their own short bullet.
- Merging is where errors creep in. Re-verify every merged bullet against its sources: the
  1.83.0 regroup dropped a condition, chained a cause onto the wrong symptom, gave a symptom
  to the wrong subject and widened a scope.

## The style rule that matters most

**A FIXED bullet describes only the problem.**

The entry is in the list because it was fixed; the tag already says so. The bold summary is
the symptom the user saw, the paragraph gives its cause, and the bullet stops there. Never
state the remedy - not as a promise, not as a neutral present-tense sentence, and not as a
UI path pointing at where some new warning appears. If a FIXED line feels unfinished, that
is the rule working: delete the last sentence.

Only ADDED and CHANGED bullets say what the thing does, because there the change is the
content: a new control has to be named, a deliberate behaviour change has to be stated.

**Hard ban on promise closers.** Never write "now does X", "now works", "now correctly",
"is now", and never the Russian "Теперь". Dropping the "now" is not a way round this on a
FIXED line - "b4 waits for the lock itself" is the same remedy sentence with the temporal
marker filed off, and it goes too.

- Bad: `Test connections now use the version b4 is set to handle.`
- Bad: `Test connections use the version b4 is set to handle, and the log names it at the start of every run.`
- Good: `Test connections were made with whatever version the library defaulted to, so a run could pass on a protocol the router never speaks.`

The existing file is not uniformly clean. A stray "now" or "теперь" in an older entry is a
past miss, not a licence to add more.

## Voice

- Plain language, no marketing. No exclamation, no "seamlessly", no "powerful".
- No em-dash anywhere. Plain hyphen, with spaces, as the separator after the bold summary.
- Describe behaviour, not code. "a set bound to a manually added source device stopped
  taking its route" beats "fixed nil deref in buildDeviceMatch".
- Write for non-technical users. Drop jargon such as RFC numbers, CGNAT, TSPU, SNI, DoH or
  TLS handshake stages, and describe the effect a user sees. Use the UI's own feature name
  ("DPI Detector").
- **Keep it short and simple to read.** One bullet is one user story. The bold summary is
  one short sentence: the symptom the user saw (FIXED) or what the user gains (ADDED,
  CHANGED). After the hyphen come one to three short sentences: the cause, or for ADDED and
  CHANGED the detail a user needs (a new control, a converted value, a UI path). One idea
  per sentence. A reader must get each sentence on the first read: when a sentence needs a
  colon, a chain of "which", "where", "while", "so", or a second "and", split it. Two or
  three short sentences beat one long one, but the content stays minimal: draft it, then
  delete every clause that survives without changing what a user learns (internal mechanism
  walkthroughs, what else was tried, boundary caveats). Splitting is no licence for a remedy
  sentence: a FIXED entry still describes only the problem. 1.78.0 and 1.76.x are the target
  density.
  - Bad: `- FIXED: **With Domain-only matching on, a set handled only the packet carrying a connection's ClientHello: the Traffic page showed the set on that row alone, and the connection's later packets got neither the set's strategy nor its RST Injection Protection** - b4 chose the set for later packets by their destination address, which a domain-only set never learns, and on an address shared with other sets they went to whichever set had last learned it.`
  - Good: `- FIXED: **A set with Domain-only matching on applied only to the start of each connection** - the rest of the connection went out without the set's strategy and showed no set on the Traffic page. Where several sets share an address, those packets could get another set's strategy instead. b4 picked the set for them by address, and a domain-only set does not track addresses.`
- b4 is a "service". Refer to UI locations the way the UI labels them
  ("under Settings, Core, Devices").

## Russian

Mirror the English meaning, do not translate word for word. Follow the project's fixed
choices: a routing set is "сет"; Discovery the feature is "Дискавери", while the act of
searching stays "поиск"; payloads, strategy family names and technical terms stay in
English (TLS, SNI, DPI, QUIC, ASN, MTProto, MSS, SOCKS5, NFQUEUE).

The short-sentence rule holds in Russian too: no chains of «который», «где», «при этом» or
stacked participle phrases. Split them into short sentences, keeping the English count.

## Procedure

1. Read the top ~40 lines of `changelog.md` to match the current voice and the version
   block you are adding to.
2. Decide `ADDED` / `FIXED` / `CHANGED`. `CHANGED` is for a deliberate behaviour change
   that was not itself a defect. An installer-only change (`installer/`, `install.sh`) gets
   no entry and no version bump: `install.sh` is served from `main`, so the fix reaches
   users when it lands. It may be mentioned inside a binary release's bullet it shipped with.
3. Write the English bullet. Before keeping it, scan it for "now" and for a present-tense
   verb that states the fixed behaviour as a closer (works, loads, stays, shows, stops,
   applies, saves). Delete that clause.
4. Write the Russian bullet at the mirrored position in `changelog_ru.md` and scan it the
   same way ("Теперь", работает, загружается, остаётся, показывается).
5. Verify both files have the same number of bullets under that version heading.

If changelog writing is delegated to a subagent, its brief must carry the short-sentence
rule (one short summary, then one to three short sentences), the FIXED-describes-only-the-problem
rule and the promise-closer ban verbatim. A brief that carried only the closer ban produced
four-to-six-sentence bullets.

## Hub changelog

The hub service has a changelog of its own, `hub/CHANGELOG.md`, and every change under
`hub/` (Go code, the console in `hub/ui`, the mirror, the `b4hub` CLI, the store schema)
or to the deploy bundle in the b4hub repo gets a bullet there in the same session, the way
router changes get one here. The b4hub release workflow refuses a version that has no
block in this file, so the bullet is what makes a release possible.

Same shape and same rules as above, with three differences:

- English only. There is no Russian mirror of the hub changelog.
- Its own version, independent of the b4 version: semver from 1.0.0, newest block on top,
  `## [x.y.z] - YYYY-MM-DD`. New capabilities bump the minor version, fixes alone the
  patch. If the top block is already released (its version exists at
  github.com/DanielLavrushin/b4hub/releases), start a new block above it.
- A change that crosses both sides, such as the wire format in `src/hubwire` or the
  router's hub client together with the hub, gets a bullet in each changelog, each written
  for its reader: router users in `changelog.md`, hub operators in `hub/CHANGELOG.md`.
