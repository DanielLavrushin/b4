# b4hub changelog

All notable changes to the b4hub community hub service are recorded in this file. The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/). Releases are published from the `b4hub` repository; the router changelog is `changelog.md` at the repository root.

## [1.11.0] - 2026-11-24

### Changed

- Votes are deleted a year after they arrive, so the device counts in the catalogue and the console's vote lists cover the last year.
- An author's votes on their own strategy count only in its worldwide score, not per provider or country. The console shows them as author votes in every set with that strategy.
- The ids of new sets no longer contain the time of the upload.
- **Last network** on the **Keys** page is the network of the newest vote, publication or complaint.

### Fixed

- A router on a network that two providers announce could count for either of them, because the lookup took whichever answer came first.
- A router behind a 6to4 or Teredo tunnel counted for the operator of the tunnel's relay.
- Hiding and restoring a set without counted votes removed its **stale** mark in the console for 60 days.
- Deleted rows stayed readable in the database file until their space was used again. Rows deleted before the update still do.

### Security

- The lookup of a router's provider sends Team Cymru only the /24 or /48 network instead of the full address.
- The public catalogue listed provider and country scores that came from a single device, though no router shows a score below two devices. They leave it with the next build, within a day or at once with **Build now** on the **Catalogue** page, and the older files kept next to it go with the two builds after that.

## [1.10.0] - 2026-11-20

### Changed

- The console's **Check** treats `targets.use_collected_ips` and `tcp.wssize_reuse`, b4's new **Use collected IPs** and **Reuse WSSIZE connection** set options, as private fields that are never shared, and lists them under **Stripped fields** when the **Full set JSON** changes one from its default.
- Set cards name the built-in fake payload 3 `vk`, after b4 replaced its DuckDuckGo ClientHello with one for `vk.com`. [b4#648](https://github.com/DanielLavrushin/b4/issues/648)
- Links in the console are yellow everywhere, including the set, key and mirror names that open their details.

## [1.9.0] - 2026-11-15

### Added

- Sets whose **Real TCP window (wssize)** removes the Window Scale option, the choice b4 1.91.1 adds, keep `tcp.wssize.strip_scale` through the hub and are marked as needing b4 1.91.1. The console lists it with the technique.

## [1.8.0] - 2026-11-14

### Added

- Sets with a Disorder **Split Position** or the **In Order** shuffle mode, the set options b4 1.91.0 adds, keep them through the hub and are marked as needing b4 1.91.0. Set cards show the position.

### Fixed

- Set cards named the Google and DuckDuckGo fake payloads `default` and `default2`, unlike b4 and its AI tools. [b4#538](https://github.com/DanielLavrushin/b4/issues/538)

## [1.7.0] - 2026-11-13

### Added

- Sets with **Real TCP window (wssize)**, the set option b4 1.91.0 adds, keep `tcp.wssize` through the hub and are marked as needing b4 1.91.0. The console lists the technique with its window, scale and cutoff, and a search for `wssize` finds these sets.

### Changed

- The console's **Check** treats `dns.upstreams`, the resolver list b4 1.91.0 adds, as a private field that is never shared, and lists it under **Stripped fields** when the **Full set JSON** carries one.

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

[1.11.0]: https://github.com/DanielLavrushin/b4hub/compare/v1.10.0...v1.11.0
[1.10.0]: https://github.com/DanielLavrushin/b4hub/compare/v1.9.0...v1.10.0
[1.9.0]: https://github.com/DanielLavrushin/b4hub/compare/v1.8.0...v1.9.0
[1.8.0]: https://github.com/DanielLavrushin/b4hub/compare/v1.7.0...v1.8.0
[1.7.0]: https://github.com/DanielLavrushin/b4hub/compare/v1.6.0...v1.7.0
[1.6.0]: https://github.com/DanielLavrushin/b4hub/compare/v1.5.0...v1.6.0
[1.5.0]: https://github.com/DanielLavrushin/b4hub/compare/v1.4.0...v1.5.0
[1.4.0]: https://github.com/DanielLavrushin/b4hub/compare/v1.3.0...v1.4.0
[1.3.0]: https://github.com/DanielLavrushin/b4hub/compare/v1.2.0...v1.3.0
[1.2.0]: https://github.com/DanielLavrushin/b4hub/compare/v1.1.0...v1.2.0
[1.1.0]: https://github.com/DanielLavrushin/b4hub/compare/v1.0.0...v1.1.0
[1.0.0]: https://github.com/DanielLavrushin/b4hub/releases/tag/v1.0.0
