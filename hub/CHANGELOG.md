# b4hub changelog

All notable changes to the b4hub community hub service are recorded in this file. The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/). Releases are published from the `b4hub` repository; the router changelog is `changelog.md` at the repository root.

## [1.7.0] - 2026-11-09

### Added

- Shared sets retain **Real TCP window (wssize)** settings, including the original ClientHello cutoff, and require b4 1.91.0.

## [1.6.0] - 2026-11-06

### Changed

- The console's **Check** treats `targets.ip_priority`, the set switch b4 1.90.0 adds, as a private field that is never shared, and lists it under **Stripped fields** when the **Full set JSON** turns it on.

### Fixed

- Telegram notifications repeated `Bad Request: chat not found`, because the chat id field did not explain the ids of supergroups, forums and channels, and the new id Telegram names after a group becomes a supergroup was discarded.

## [1.5.0] - 2026-10-21

### Added

- Sets without targets that handle only calls, which b4 1.88.0 can share, are accepted and marked as needing b4 1.88.0, unless they also filter TCP ports. Set cards show their calls, such as `Calls: Discord app, Browsers`, and search finds them by `discord` or `stun`.

## [1.4.0] - 2026-10-12

### Added

- Sets with the switch that removes ECH keys from DNS answers, which b4 1.87.0 can share, keep `dns.strip_ech` through the hub and are marked as needing b4 1.87.0.

## [1.3.0] - 2026-09-27

### Added

- Whole-set, bulk and report moderation: a set can be withdrawn and reinstated, a listed version's title and description edited, up to 200 versions decided at once, and reports go through an inbox as open, dismissed or handled.
- A console built for reviewing, with sets shown as cards like the router's, sortable paged lists, a linkable panel for every set and key, and a test tag that keeps a key's votes out of published scores.
- **Statistics**, **Audit log** and a **Health** card on **Overview** show daily activity, the spread of scores, where votes come from, every moderation action and what needs attention.
- Notifications to Telegram or a webhook with a digest of new sets, reports, automatic hides, failed builds and new mirrors, set up in the console or with `B4HUB_TELEGRAM_TOKEN`, `B4HUB_TELEGRAM_CHAT`, `B4HUB_WEBHOOK_URL` and `B4HUB_WEBHOOK_SECRET`.
- A restart after an outage or a database restore refuses to publish a catalogue that drops the mirrors, goes backwards or replaces the published one from a new database. `--new-database` on `serve` and `build` confirms an intended new database.

### Changed

- Moderation actions return at once, and a background build starts 1.5 seconds after the last change and no later than 10 seconds after the first, with failed builds and a build history on **Overview** and **Catalogue**. `b4hub moderate` covers sets, keys, reports, mirrors, builds and the audit log.
- A ban is a reversible quarantine: a banned key's sets leave the catalogue and its votes and reports stop counting until the ban is lifted.
- A mirror answers routers within their wait while the hub is down, and checks, limits and signs what it passes on, with its queue capped by `--queue-limit` (5000 by default, 0 for none). A vote relayed by an approved mirror counts only in the overall score, and its complaint never counts toward the automatic hide.

### Fixed

- A signed vote or complaint could be counted twice under a re-encoded signature.

## [1.2.0] - 2026-09-26

### Added

- Sets with ASN targets, which b4 1.83.0 can share, count as targets in the duplicate and no-targets checks and show as `AS15169` in the console and its search. A set with more than 50 ASNs is flagged `too_many_asns`.

## [1.1.0] - 2026-09-19

### Added

- The **Mirrors** page and `b4hub moderate mirrors` show the b4hub version each mirror reported, refreshed when the mirror starts and once a day.

## [1.0.0] - 2026-09-18

### Added

- A pending set can be edited in the moderation console before approval, with the same checks as a fresh share and the received version kept for comparison. The domain check flags entries that can never match, entries covered by a shorter one, duplicates and a `www.` entry without its apex, with a one-click tidied list.

### Changed

- b4hub is versioned and released on its own from the `b4hub` repository, and the binary reports the b4 source tree it was built from.

[1.7.0]: https://github.com/DanielLavrushin/b4hub/compare/v1.6.0...v1.7.0
[1.6.0]: https://github.com/DanielLavrushin/b4hub/compare/v1.5.0...v1.6.0
[1.5.0]: https://github.com/DanielLavrushin/b4hub/compare/v1.4.0...v1.5.0
[1.4.0]: https://github.com/DanielLavrushin/b4hub/compare/v1.3.0...v1.4.0
[1.3.0]: https://github.com/DanielLavrushin/b4hub/compare/v1.2.0...v1.3.0
[1.2.0]: https://github.com/DanielLavrushin/b4hub/compare/v1.1.0...v1.2.0
[1.1.0]: https://github.com/DanielLavrushin/b4hub/compare/v1.0.0...v1.1.0
[1.0.0]: https://github.com/DanielLavrushin/b4hub/releases/tag/v1.0.0
