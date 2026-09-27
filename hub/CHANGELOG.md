# b4hub changelog

Releases of the community hub service, published from the `b4hub` repository. The router changelog is `changelog.md` at the repository root.

## [1.3.0] - 2026-09-27

- CHANGED: **Moderation actions return at once and the catalogue is published by a background build** - a build starts 1.5 seconds after the last change and no later than 10 seconds after the first, the console header shows whether one is queued, running or failed with its error, and the Catalogue page keeps a history of what each build changed. Approved mirrors are checked every 10 minutes instead of at every build, and `b4hub moderate`, extended to sets, keys, reports, mirrors, builds and the audit log, leaves a build request that a running hub picks up within about ten seconds.
- CHANGED: **A ban is a reversible quarantine** - a banned key's sets leave the catalogue and its votes and reports stop counting until the ban is lifted, while before a ban only refused new records and left the key's sets listed and its votes in the scores.
- ADDED: **Whole-set, bulk and report moderation** - a set can be withdrawn from the catalogue and reinstated as it was, the title and description of a listed version can be changed without a new version, up to 200 versions can be decided at once, every decision states its catalogue effect before it is confirmed, and reports go through an inbox as open, dismissed or handled, with only open reports on a known network from keys neither banned nor tagged test counting toward the automatic hide.
- ADDED: **A console built for reviewing** - sortable, paged tables with the search in the address, a panel with its own link for every set and key, a key's activity, networks, name, note and a test tag that keeps its votes out of published scores, each vote's applied weight, similar sets and keyboard shortcuts in the queue, and saved reasons in every decision dialog.
- ADDED: **Oversight pages** - Statistics counts activity per day, the spread of published scores and where votes come from, the Audit log records every action from the console, the command line and the hub itself, and a Health card on Overview lists what needs attention in place of the geo databases card.
- ADDED: **Notifications to Telegram or a webhook** - a digest of new sets, reports, automatic hides, failed builds and new mirrors, grouped by a configurable interval, in English or Russian, with a signed JSON body for the webhook and the credentials set in the console or in `B4HUB_TELEGRAM_TOKEN`, `B4HUB_TELEGRAM_CHAT`, `B4HUB_WEBHOOK_URL` and `B4HUB_WEBHOOK_SECRET`.

## [1.2.0] - 2026-09-26

- ADDED: **Sets with ASN targets, which b4 1.83.0 can share, are handled as targets throughout the hub** - an ASN counts toward the same-targets duplicate check and the no-targets check, the targets summary, the moderation console and its search show it as `AS15169`, and a set carrying more than 50 ASNs is flagged with `too_many_asns`.

## [1.1.0] - 2026-09-19

- ADDED: **The Mirrors page of the console and `b4hub moderate mirrors` show the b4hub version each mirror reported** - the version travels in the announcement, so it refreshes when the mirror starts and once a day after that.

## [1.0.0] - 2026-09-18

- ADDED: **A pending set can be edited in the moderation console before it is approved** - title, description, the domain list and the full set JSON go through the same checks as a fresh share, the received version is kept for comparison, and the check flags domain entries that can never match (b4 has no wildcard syntax), entries already covered by a shorter one, duplicates and a `www.` entry whose apex is missing, with a one-click tidied list. Until now a submission with a flawed domain list could only be approved as it was or rejected.
- CHANGED: **b4hub is versioned and released on its own** - tarballs, images and release notes come from the `b4hub` repository, and the binary reports the b4 source tree it was built from. Until now the hub shipped under the b4 version number, so a hub-only change required a router release.
