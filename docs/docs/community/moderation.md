---
sidebar_position: 6
title: The moderation console
---

# The moderation console

Every version of every set waits for a moderator before it reaches the catalogue. The console is where that decision is made. It is part of the `b4hub` binary, served at `/admin` on the same address as the hub itself, and it is available in English and Russian.

The console belongs to a hub, not to a mirror: a mirror has no moderation database and makes no decisions, so it has no console. [Running a hub](./hosting.md) describes the difference.

:::warning
The console names two things differently from the router. What a router calls a **works or broken report** is a **vote** here, and what a router calls a **complaint** is a **report** here. The rest of this page uses the console's own words, so that every label matches what is on screen.
:::

## Signing in

The password is `B4HUB_ADMIN_PASSWORD` from the service environment. There are no accounts and no roles; whoever has the password is the moderator.

| | |
| --- | --- |
| Session | A cookie scoped to `/admin`, valid for seven days |
| Changing the password | Ends every open session at once |
| Sign-in attempts | Ten per address in each fifteen minute window, wrong or right, then refusals with a retry time |
| Where actions are accepted from | The console itself; a moderation request that does not come from it is refused |

:::info
The cookie is marked secure when the request arrives over TLS or carries `X-Forwarded-Proto: https`, which a reverse proxy is expected to set.
:::

With the password unset the console loads but nothing works: the sign-in card reports that moderation is not configured, and every sign-in and moderation request is refused. `b4hub moderate` on the host is then the only way to moderate. It opens the same database, in WAL mode with a busy timeout, so a running service does not lock it out, but that service goes on serving the catalogue it last built until its next build.

## The pages

| Page | What it is for |
| --- | --- |
| **Overview** | What is waiting, what is published, and what this hub is |
| **Queue** | The versions waiting for a decision. The count is shown on the navigation entry |
| **Sets** | Everything decided: listed, superseded, hidden and rejected |
| **Keys** | The contributors the hub has seen, and the ban and trust marks on them |
| **Mirrors** | The hubs that announced themselves as mirrors of this one |
| **Feedback** | The most recent votes and complaints as they arrived |
| **Catalogue** | The published files, and the three operations that republish them |
| **Settings** | The daily and hourly limits |

Overview, Queue, Sets and Mirrors refresh themselves every thirty seconds. Every action reports what it did in a notification carrying the hub's own wording, such as `approved 01J9.../2`.

## Overview

Four counts across the top, each opening the page it counts: pending versions, listed sets, keys with the number banned, and mirrors with the number approved and pending.

Below them, four cards. **Catalogue** is what routers download right now: the file, the epoch and sequence number, when it was generated and when it expires, how many sets and payload files it holds, how many mirrors it lists, and whether anything is waiting for the next build. **This hub** carries the version, the b4 source tree it was built from, the key id, the public URL and the number of revoked keys. **Moderation** counts the decisions so far: listed, superseded, hidden and rejected versions, and the votes and reports received. **Geo databases** shows each downloaded file with its size, source and any error.

## Queue

One card per pending version, oldest first. Approving publishes: the build runs inside the request, so the set is in the catalogue as soon as the action returns.

Each card carries the title, the set id and version, the technique family, and when it was received. A banner above states what approving would mean:

| | |
| --- | --- |
| First version of a new set | Nothing of this set is listed |
| New version of a set currently listed | Approving replaces the listed one |
| New version of a set with nothing listed | Approving lists the set again |

When a listed version exists, **Compare with listed** opens a field-by-field difference between it and the pending one.

The **Origin** line names the author label, the network the hub observed the upload from, the network the uploader claimed, and the b4 version and capture engine it was shared from.

:::info
The hub never stores a contributor's key. What it keeps, and what the console shows as the author, is an HMAC of that key under the hub's own secret: 16 characters wherever the author is named, with the full value in the tooltip on the Sets table and on the votes list. Two sets by the same contributor carry the same label; the key itself cannot be recovered from it.
:::

Under that, everything the version holds: the targets as a summary, the strategy written out as sentences, the flags, the server names the fake packets carry, the pins, the DoH host, the payload files with their sizes and download links, the **Votes** and **Reports** recorded for it, the fingerprint and the b4 version the set needs. **Projection** expands the set as JSON.

### The decisions

| Action | Reason | Effect |
| --- | --- | --- |
| **Approve** | none | The version is listed at the build that starts immediately |
| **Reject** | required | The version is refused and the reason is recorded |
| **Hide** | optional | The version leaves the catalogue at the build that starts immediately |
| **Edit** | none | Opens the editor in a dialog |
| **Ban uploader** | optional | Every later record signed by that key is refused |

:::warning
Banning a key refuses what it sends from then on. It does not take the sets it already published out of the catalogue; those have to be hidden one by one. The wording on the ban dialog says otherwise.
:::

A rejection and a hiding differ in what they say. A rejection is a verdict on a submission. Hiding takes something out of circulation without asking for a new version.

:::warning
A decision does not travel back. The catalogue carries approved versions only, so a rejection reason is recorded on the hub and read in this console; nothing delivers it to the author, whose router only reports that the set is not in the catalogue. The wording on the rejection dialog says otherwise and is wrong.
:::

## Editing a pending version

A submission with a good strategy and a flawed domain list can be corrected instead of refused. The editor opens on a pending version only: once a version is listed, rejected or hidden, the author has to publish again.

Four fields are editable: the title, the description, the domain list one entry per line, and the full set JSON. The domain list and the JSON stay in step, so a change to either is reflected in the other.

:::info
The description is the one field the router's publish button leaves empty. A set normally arrives with its name as the title and nothing else, although the router's `/api/hub/share` endpoint accepts a description and the hub stores it, so a description on a card was either sent with the share or written here.
:::

**Check** runs the submission through the same checks as a fresh share and answers with one of three verdicts:

| Verdict | What it means |
| --- | --- |
| Nothing differs from the received set | The edit is empty, and saving is refused |
| Only targets, title or description differ | The fingerprint is unchanged, so the votes already recorded still count |
| The strategy differs from what the author sent | The fingerprint changes, the votes recorded for this version are dropped, and the published set no longer matches the author's own copy |

A result that duplicates a set the hub already holds is refused outright and names the set it duplicates.

The check also reports what the scrub did: fields b4 does not know, values it does not accept, a DoH host outside the public list, domains or addresses that look private, pins, a missing payload, a block set matching by pattern. Settings that cannot travel in a shared set at all are listed separately with the reason they were dropped.

### The domain-list checks

b4 has no wildcard syntax, and a plain entry already covers every subdomain. The editor flags the four ways a list gets that wrong, and **Apply suggestions** rewrites the list in one step.

| Check | Example | What it suggests |
| --- | --- | --- |
| Entry that can never match | `*.example.com` | Replace it with `example.com`, or drop it when that is already listed |
| `www.` entry with no apex | `www.example.com` alone | Replace it with `example.com`, which covers both. Other subdomains are not flagged |
| Repeated entry | `Example.com.` after `example.com` | Drop it |
| Entry already covered | `cdn.example.com` beside `example.com` | Drop it |

A `regexp:` entry is never treated as covering anything and is never flagged as covered; the only check that touches it is the repeated-entry one, which drops the same pattern listed twice.

**Save** keeps the version pending. **Save and approve** publishes it in the same step. Both record the moderator's note, which is visible in the console only, and keep the received set so the card can show what was changed. A version changed by someone else since the dialog was opened is refused rather than overwritten.

## Sets

Four tabs: **Listed** is the newest approved version of each set, **Superseded** the older approved versions still on record, **Hidden** and **Rejected** the decisions taken, most recent first and capped at the fifty most recent.

One search box matches the title, the set id, the author label, the technique family, the reason a decision carried, every target domain, every geosite category and every sentence of the strategy.

A row opens a panel holding every version of that set, each with its status, its facts and the actions its status allows. A hidden version can be restored and a rejected one approved after all, both of which put it back in the catalogue. The panel also lists every vote recorded for the set: when it arrived, its kind, the version it applies to, the network the hub observed and whether it could place that network, the contributor, the domain the list was filtered by, and the b4 version. The reports against a version sit on that version's own card.

:::danger
**Delete permanently**, at the bottom of the panel, removes the set with all its versions, its votes, its reports and any payload file no other set uses. It asks for the set id to be typed. Hiding is what takes a set out of the catalogue; deletion is for an entry that should never have existed. Routers that applied the set keep their local copy and lose the link to the hub.
:::

## Keys

One row per contributor the hub has seen, identified by the HMAC, with when it was first seen and how many sets, votes and reports it signed. A switch narrows the list to banned keys.

| Action | Effect |
| --- | --- |
| **Ban** | Every later record from the key is refused. Its listed sets stay listed |
| **Unban** | The key is accepted again |
| **Trust** | The key stops being counted against the per-key daily limits |
| **Untrust** | It is counted again |

:::info
Trust lifts the limits and nothing else. A trusted contributor's sets still wait for a moderator like everyone else's, and the per-network limits still apply.
:::

## Mirrors

Every hub that announced itself as a mirror of this one, pending first. The row carries the URL, the key that announced it, the status, when it was first and last seen, and the result of the last health check.

| Action | Effect |
| --- | --- |
| **Approve** | The mirror is health-checked at every build and listed in the manifest while it passes |
| **Reject** | It stays in the list with the reason and is never announced. A reason is required |
| **Remove** | It is forgotten, and can announce itself again |

The health check runs only on approved mirrors, and only when the catalogue is built, so a pending or rejected mirror stays unchecked forever.

:::info
The console reports a mirror as healthy only when its last check succeeded, while the manifest keeps listing it for 24 hours after its last success. A mirror can therefore read as failed here and still be in the catalogue routers are downloading.
:::

## Feedback

Two tabs showing the raw traffic from routers, newest first, 200 rows by default and up to 1000.

**Votes** carries, per row, when it arrived, the set and version, its kind, the weight it ended up with, the network the hub observed and the one the router claimed, the contributor, the domain the list was filtered by, and the b4 version and engine. A set reference opens the set's panel.

The kinds are the works and broken reports made by hand, the upload counted as a works report for the author, and the ones a router could send on its own from the DPI detector, the domain watchdog and Discovery. The hub defines and weighs those automatic kinds; b4 does not send them yet.

**Reports** is the complaints tab: when, which set, the network, the contributor and the reason typed in.

## Catalogue

The published files, the mirrors the manifest lists and the keys it revokes, plus three operations. Each of them republishes.

| Operation | What it does |
| --- | --- |
| **Build now** | Publishes from the store even when nothing changed |
| **Start a new epoch** | Resets the sequence number. Every router treats the new epoch as authoritative and downloads again |
| **Revoke a key** | Adds a key id to the list every manifest carries. Routers stop trusting anything signed by it |

:::warning
A new epoch is what makes routers accept a catalogue after the database was restored from a backup, because they refuse anything older than what they already hold. Outside that case it makes every router download the catalogue again for no gain.
:::

:::info
Revoking and banning are different tools. Revoking names the signing key of a hub or a mirror and travels to every router in the manifest; the hub refuses to revoke the key it signs with. Banning names a contributor and only affects what this hub accepts.
:::

## Settings

Six limits, each a whole number from 1 to 1000000, applied to the next request; what a key has already spent today is kept.

| Limit | Counted per | Default |
| --- | --- | --- |
| Shares per key | day | 5 |
| Votes per key | day | 20 |
| Reports per key | day | 20 |
| Mirror announcements per key | day | 48 |
| New keys per network | day | 5 |
| Requests per network | hour | 2000 |

A day is the UTC calendar day and an hour is the clock hour, so a limit reached at 22:00 UTC clears at midnight. The counters live in memory and start over when the service restarts. A network here is the sender's `/24`, or `/48` for IPv6.

A record the hub refuses, and a shared set that turns out to duplicate one it already holds, do not spend the contributor's quota. A key marked trusted skips the per-key limits but not the per-network ones.

:::info
What a router sees when it runs into one of these is described under [Reports and complaints](./feedback.md#limits).
:::

## Without the console

`b4hub moderate` does the same work from the command line: `list`, `approve`, `reject`, `hide`, `ban`, and `mirrors`, `approve-mirror` and `reject-mirror`. It opens the same database directly, in WAL mode with a busy timeout, so the service can stay up; it publishes nothing itself, and a running service picks the decision up at its next build.
