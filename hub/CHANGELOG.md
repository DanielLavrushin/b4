# b4hub changelog

Releases of the community hub service, published from the `b4hub` repository. The router changelog is `changelog.md` at the repository root.

## [1.1.0] - 2026-09-19

- ADDED: **The Mirrors page of the console and `b4hub moderate mirrors` show the b4hub version each mirror reported** - the version travels in the announcement, so it refreshes when the mirror starts and once a day after that.

## [1.0.0] - 2026-09-18

- ADDED: **A pending set can be edited in the moderation console before it is approved** - title, description, the domain list and the full set JSON go through the same checks as a fresh share, the received version is kept for comparison, and the check flags domain entries that can never match (b4 has no wildcard syntax), entries already covered by a shorter one, duplicates and a `www.` entry whose apex is missing, with a one-click tidied list. Until now a submission with a flawed domain list could only be approved as it was or rejected.
- CHANGED: **b4hub is versioned and released on its own** - tarballs, images and release notes come from the `b4hub` repository, and the binary reports the b4 source tree it was built from. Until now the hub shipped under the b4 version number, so a hub-only change required a router release.
