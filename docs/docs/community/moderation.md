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

With the password unset the console loads but nothing works: the sign-in card reports that moderation is not configured, and every sign-in and moderation request is refused. `b4hub moderate` on the host is then the only way to moderate, as described [at the end of this page](#without-the-console).

## The pages

| Page | What it is for |
| --- | --- |
| **Overview** | What needs attention, what is published, and what this hub is |
| **Queue** | The versions waiting for a decision |
| **Sets** | Everything decided: listed, superseded, withheld, hidden and rejected |
| **Keys** | The contributors the hub has seen, their activity, and the marks on them |
| **Mirrors** | The hubs that announced themselves as mirrors of this one, and their health |
| **Feedback** | Every vote with the weight it carries, and the inbox of reports |
| **Catalogue** | The published files, the build history, and the operations that republish |
| **Statistics** | Activity per day, the spread of published scores, and where votes come from |
| **Audit log** | Every action taken on the hub, by whom and when |
| **Settings** | The daily and hourly limits, saved reasons and notifications |

The navigation carries counts: pending versions on **Queue**, listed sets that need attention on **Sets**, pending mirrors on **Mirrors** and open reports on **Feedback**. The browser tab title starts with the number of pending versions, and a coloured dot on **Overview** shows the worst open health issue.

A set or a key named anywhere in the console is a link. It opens a side panel with its own address, `/admin/sets/<id>`, `/admin/sets/<id>/v/<version>` or `/admin/keys/<hmac>`, over the page it was opened from; the browser's back button closes it, and the address can be shared or bookmarked. Lists keep their search, filters, sort order and page in the address as well.

Every action reports what it did in a notification carrying the hub's own wording, such as `approved 01J9.../2`.

## Publishing

A decision changes the store at once. The catalogue routers download is rebuilt in the background: a build starts 1.5 seconds after the last change, and never later than 10 seconds after the first, so a burst of decisions ends in one build. Nothing waits for the build, and a failed build does not undo the decision.

Where publishing stands is shown on two pages:

| Where | What it shows |
| --- | --- |
| **Overview**, the **Catalogue** card | **Catalogue matches the store**, or **Changes waiting for the next build** while the store holds changes no build has published yet |
| **Overview**, **Health** | The last build failed, with its error |
| **Catalogue**, **Published catalogue** | The same as the **Catalogue** card, and the time and error of a failed build |
| **Catalogue**, **Build history** | Every build, why it ran, its result and what it changed |

A failed build stays on both pages until a build succeeds, and routers keep the previous catalogue meanwhile. **Build now** on the Catalogue page queues another build. The console polls the build state and, once a build finishes, reloads the catalogue, the health list, the build history, mirrors, sets and the navigation counts.

## Overview

**Health** comes first and lists what needs attention, each item with a link to the page that deals with it:

| Issue | Severity |
| --- | --- |
| No catalogue has been published | error |
| The last build failed, with its error | error |
| The manifest expires within 2 days | error; within 7 days a warning |
| No successful build for longer than a day and two check intervals | warning |
| A version has been pending for more than 72 hours | warning |
| Approved mirrors failing their health check, or serving an older catalogue | warning |
| Mirrors waiting for approval | note |
| Open reports | note |
| The hub runs a development build, or one built from a source tree with uncommitted changes | note |

Under the list, the facts behind it: when the manifest expires, the last successful build, the last build error, the version that has waited longest, and how many approved mirrors are announced, failing or behind.

Four counts follow, each opening the page it counts: pending versions, listed sets, keys with the number banned, and mirrors with the number announced and pending. **Catalogue** is what routers download right now: the file, the epoch and sequence number, when it was generated and when it expires, how many sets and payload files it holds, how many mirrors it lists, and whether anything is waiting for the next build. **This hub** carries the version, the b4 source tree it was built from, the key id, the public URL and the number of revoked keys. **Moderation** counts the decisions so far.

## Queue

One card per pending version, oldest first, laid out like a set card on the router's **Sets** page.

The strip along the top edge has one segment for each part of the set: **Target**, **Split**, **Fake**, **Route**, **DNS** and **Escalate**, with **Block** in place of **Route** on a block set. A segment is lit when the set configures that part, and a click on it opens the part's settings under the card; **Escalate** is never lit, because escalation does not travel in a shared set. With more than one card, **Compare across sets** above them opens the same part on every card, and **Collapse all** and **Expand all** close and reopen what is open.

The version sits in the top right corner, `v1` for the first version of a new set. Its tooltip states what approving would mean:

| Tooltip | Meaning |
| --- | --- |
| **First version of a new set** | Nothing of this set is listed |
| **New version of a set listed as vN** | Approving replaces the listed one |
| **New version of a set with nothing currently listed** | Approving lists the set again |
| **Older than the listed vN** | Approving changes nothing in the catalogue: the newest active version stays listed. The version is outlined in orange |

The line under the title names the author, the technique family, the b4 version the set needs and when the version was received. Its tooltip adds the exact time, the network the hub observed the upload from, the network the uploader claimed, and the b4 version and capture engine it was shared from. The targets follow as a summary, then the route: the default route, the default route with DNS pins, or a block with its action. A set that does not decode with the hub's b4 version has no strip and a note in place of the route.

:::info
The hub never stores a contributor's key. What it keeps, and what the console shows as the author, is an HMAC of that key under the hub's own secret: 16 characters wherever the author is named, with the full value in the key's panel. Two sets by the same contributor carry the same label; the key itself cannot be recovered from it.
:::

A row of marks follows:

| Mark | Meaning |
| --- | --- |
| **needs payload**, **blocks traffic**, **pins addresses**, **categories only**, **pattern match** | The flags, each explained in its tooltip. **blocks traffic** is red, **categories only** and **pattern match** are orange |
| **author banned** | Nothing of the set is listed until the ban is lifted |
| **set withdrawn** | A moderator withdrew the whole set |
| **edited** | A moderator changed the version. The tooltip gives the time and the note, and a click compares the version with the received set |
| **similar sets: N**, or **similar sets not checked** | The hub already holds sets that resemble this version, or they could not be checked; a click opens the list or the error |
| **open reports: N** | The version has open reports; the tooltip counts them, and a click opens the set's panel |

The list holds what the hub already has with the same targets, the same strategy, shared or overlapping domains, the same or a similar title, or the same author, each with its status, its votes and the decision it carries. The same targets or the same title as another set mark a likely duplicate and turn **similar sets: N** orange. **Reject as duplicate** fills the rejection reason with a reference to the set it duplicates.

A click on the card, or **Open details** in the menu in its corner, opens the set's panel at this version with everything the version holds: the targets as a summary, the strategy written out as sentences, the flags, the server names the fake packets carry, the pins, the DoH host, the payload files with their sizes and download links, the **Votes** and **Reports** recorded for it, the fingerprint and the b4 version the set needs. **Projection** expands the set as JSON. When a listed version exists, **Compare with listed vN** in the same menu opens a field-by-field difference between it and the pending one under the card. The menu also holds **Ban uploader**; **Approve**, **Edit**, **Reject** and **Hide** are buttons at the bottom of the card.

### The decisions

| Action | Reason | Effect |
| --- | --- | --- |
| **Approve** | none | The version becomes active and is listed at the next build |
| **Reject** | required | The version is refused and the reason is recorded |
| **Hide** | optional | The version leaves the catalogue at the next build |
| **Edit** | none | Opens the editor in a dialog |
| **Ban uploader** | optional | See [Banning a key](#banning-a-key) |

Each dialog states the catalogue effect before anything changes: which version joins the catalogue, which one it replaces, whether the set leaves it, or that nothing changes. Hiding the listed version of a set lists the newest older active version instead; **Also withdraw the whole set** prevents that. When the version has open reports, the dialog says how many will be dismissed or marked handled, and **Keep the open reports open** leaves them as they are.

Approving a version whose set is withdrawn, or whose author is banned, needs a second confirmation: the version becomes active but stays out of the catalogue until the set is reinstated or the ban lifted.

:::info
A rejection reason is recorded on the hub and read in this console. The catalogue carries approved versions only, so nothing delivers the reason to the author, whose router only reports that the set is not in the catalogue.
:::

A rejection and a hiding differ in what they say. A rejection is a verdict on a submission. Hiding takes something out of circulation without asking for a new version, and a hidden version keeps counting as a duplicate of later shares with the same strategy.

### Several at once

A checkbox on each card, or **Select all**, collects versions for one decision. The bulk dialog lists the catalogue effect of every selected version and names the ones the action cannot apply to, such as a version that is no longer pending; those are skipped and the rest are changed together, in one transaction, followed by one build. A bulk action takes at most 200 versions.

### Saved reasons

Every dialog that asks for a reason offers the reasons saved for that kind of action as one-click chips, most used first. A typed reason can be kept with **Save as a preset**. The list is edited under **Settings**, one list per action: rejecting and hiding a version, withdrawing a set, banning a key, rejecting a mirror and dismissing a report.

## Editing a pending version

A submission with a good strategy and flawed targets can be corrected instead of refused. The editor opens on a pending version only: once a version is listed, rejected or hidden, only its title and description can change, as described [below](#editing-the-text-of-a-listed-version).

The dialog holds the **Title**, the **Description**, the **Targets** and a **Moderator note**. Each kind of target has a field of its own:

| Field | What it takes |
| --- | --- |
| **Domains** | One entry per line |
| **Addresses** | IP addresses and CIDR ranges, one per line or separated by commas or spaces |
| **ASNs** | One per line, as `AS13335` or `13335`; a valid entry is stored as the bare number |
| **Geosite categories**, **GeoIP categories** | Names offered from the hub's own geosite and GeoIP files, or typed in; each router looks them up in its own file |
| **TLS version** | **Any**, **TLS 1.2** or **TLS 1.3** |
| **IP version** | **Any**, **IPv4** or **IPv6** |
| **Domain-only matching** | Matches the domains only by the name in the handshake, never by their IP address |

**Show the full set JSON** opens everything the set carries as one JSON document, which is where a setting outside these fields is changed. The fields and the JSON stay in step: a change to a field rewrites only that key of the JSON, and a change to the JSON fills the fields again. While the JSON is not valid, the target fields are locked.

:::info
The description is the one field the router's publish button leaves empty. A set normally arrives with its name as the title and nothing else, although the router's `/api/hub/share` endpoint accepts a description and the hub stores it, so a description on a card was either sent with the share or written here.
:::

**Check** runs the submission through the same checks as a fresh share and answers with one of three verdicts:

| Verdict | What it means |
| --- | --- |
| Nothing differs from the current version | The edit is empty, and saving is refused |
| Only targets, title or description differ | The fingerprint is unchanged, so the votes already recorded still count |
| The strategy differs from what the author sent | The fingerprint changes, the votes recorded for this version are dropped, and the published set no longer matches the author's own copy |

A result that duplicates a set the hub already holds is refused outright and names the set it duplicates. A change to the b4 version the set needs is stated, and so is every flag the edit adds or removes; a new **blocks traffic**, **pattern match** or **categories only** flag turns that notice into a warning.

The check also reports what the scrub did. A warning that concerns one field sits under that field:

| Field | Warnings |
| --- | --- |
| **Domains** | Domains that look private, too many domains, a block set matching by pattern, a pin dropped because its domain is not targeted or it points at a private address |
| **Addresses** | Addresses that look private, entries that are neither an IP address nor a CIDR range, which routers skip, a `/0` range covering every address, too many addresses |
| **ASNs** | Too many ASNs, values b4 does not accept |
| **Geosite categories**, **GeoIP categories** | Names missing from the hub's own file; the author may use a different source |

**Warnings from the check** lists the rest: fields b4 does not know, other values it does not accept, a DoH host outside the public list, pins, a missing payload. **Stripped fields** lists the settings that cannot travel in a shared set at all, with the reason they were dropped. Addresses, categories and ASNs are checked while they are typed; after a check, the hub's verdict takes the place of those notes on every field that has not changed since.

When the check refuses the set, **The check refused the set:** lists each reason with its field, and a target field also shows its reason in red under it, such as an ASN that is not a public AS number.

### The domain-list checks

b4 has no wildcard syntax, and a plain entry already covers every subdomain. The editor lists the four ways a list gets that wrong under **Domains**, and **Apply suggestions** rewrites the list in one step.

| Check | Example | What it suggests |
| --- | --- | --- |
| Entry that can never match | `*.example.com` | Replace it with `example.com`, or drop it when that is already listed |
| `www.` entry with no apex | `www.example.com` alone | Replace it with `example.com`, which covers both. Other subdomains are not flagged |
| Repeated entry | `Example.com.` after `example.com` | Drop it |
| Entry already covered | `cdn.example.com` beside `example.com` | Drop it |

A `regexp:` entry is never treated as covering anything and is never flagged as covered; the only check that touches it is the repeated-entry one, which drops the same pattern listed twice.

**What saving changes**, the last part of the check result, compares the edited set with the version as stored now, field by field, with a line for every list entry added or removed.

**Save** keeps the version pending. **Save and approve** approves it in the same transaction. Both stay disabled until a check of the current content finds a change and no duplicate, and neither accepts a set without a domain, an address, an ASN or a category, or one with a TLS or IP version b4 does not know. Both record the moderator's note, which is visible in the console only, and keep the received set so the queue card and the set's panel can show what was changed; the audit entry also lists the targets added and removed, and the b4 version the set needs when it changed. A version changed by someone else since the dialog was opened is refused rather than overwritten.

## Editing the text of a listed version

**Edit title** on any version outside the queue changes its title and description and nothing else. The strategy, the targets, the fingerprint and the version number stay the same, so routers see no new version and the votes keep counting. The catalogue carries the new text from the next build: routers that applied the set keep the name they stored, and new ones get the new title. The title is required and limited to 120 characters, the description to 2000.

## Sets

Five tabs:

| Tab | What it holds |
| --- | --- |
| **Listed** | The version of each set that is in the catalogue |
| **Superseded** | Older approved versions still on record, with the version that replaced them |
| **Withheld** | Sets kept out of the catalogue as a whole, because they were withdrawn or their author is banned, with the reason |
| **Hidden** | Hidden versions, with the reason, and a mark on those hidden by reports that still wait for a review |
| **Rejected** | Rejected versions, with the reason |

Every tab shows its sets as cards, laid out like the queue's, and is paged on the hub, with 25, 50, 100 or 200 sets per page. **Sort by** orders them by **Last update**, **Received**, **Title**, **Author**, **Score**, **Vote weight**, **Devices** or **Reports**, and the arrow beside it reverses the order; **Hidden** and **Rejected** carry no score, so they offer neither **Score**, **Vote weight** nor **Devices**. The search box matches the title, the description, the set id, the author label and HMAC, the technique family and codes, the reason a decision carried, every target domain, every geosite category and every ASN written as `AS15169`.

A card has the strip of the set's parts, the version, the title, the targets and the route, like a card in the queue, and adds:

| Part | What it shows |
| --- | --- |
| Version | The set id and version in the tooltip, and on **Listed** the other active versions |
| The line under the title | The author, the technique family and when the version last changed. The tooltip adds the author's name and tag, the label routers see, whether the key is trusted or banned, the network the upload came from, and when the version was received and updated |
| The line under the route | **Superseded by vN** with its time, or the reason the version is hidden, rejected or withheld |
| Marks | The flags, **author banned**, **set withdrawn**, **hidden before approval**, **edited**, the attention marks and the reports |
| Score | The score routers were given in the last build, green from 70%, orange from 40% and red below, or **unrated**; **next:** with the live value follows it when the next build changes it. Then the independent works and broken votes. The tooltips give the weighted votes and devices behind each, the author's own votes, and how many other sets share the strategy's votes. Not shown on **Hidden** and **Rejected** |

Three toggles narrow the list: **With open reports**, **Edited** and, on **Listed**, **Needs attention**. The attention marks on a listed set are signals for this console only, and routers still list the set:

| Mark | When |
| --- | --- |
| **low score** | At least 3 weighted votes and a score under 30% |
| **stale** | No vote for 60 days and almost no weight left |
| **under 50%** | Enough independent votes to show a score, and the score is under 50% |
| **open reports** | The listed version has open reports |

A card shows them as one mark that names the most severe and adds **+N** for the others; its tooltip lists them all. Open reports show in the reports mark instead: **open reports: N**, **awaiting review** on a version hidden by reports that still has open ones, or **reports: N** once every report is dealt with. A click on it opens the set's panel.

Cards can be selected for a bulk action on the tabs where one applies, one by one or with **Select all** for the page: hiding listed and superseded versions, restoring hidden ones and approving rejected ones.

A card opens a panel holding every version of that set, each with its status, its facts and the actions its status allows. A hidden version can be restored and a rejected one approved after all. The panel also lists every vote recorded for the set, and its **History**: every action taken on the set, by whom and with what reason. The menu in the card's corner offers **Open details**, which opens the same panel, and **Open the author's key**.

### Withdrawing a set

**Withdraw set** in the panel takes the whole set out of the catalogue at the next build, whatever its versions' states. The versions keep their states, so **Reinstate** brings back exactly what was listed before. Withdrawing marks the set's open reports as handled. A share of a new version of a withdrawn set is still accepted and waits in the queue like any other.

:::danger
**Delete permanently**, at the bottom of the panel, removes the set with all its versions, its votes, its reports and any payload file no other set uses. It asks for the set id to be typed. Withdrawing is what takes a set out of the catalogue; deletion is for an entry that should never have existed. Routers that applied the set keep their local copy and lose the link to the hub.
:::

## Keys

One row per contributor the hub has seen, identified by the HMAC, with when it was first and last seen, how many sets, votes and reports it signed, and the network of its last record. The search box takes an HMAC prefix, a name, a note, or a router's own key id, which the hub turns into its HMAC. Filters narrow the list by status (banned, trusted, neither), tag, and sets (any, listed, pending, none).

A row opens the key's panel:

| Section | What it holds |
| --- | --- |
| Activity | Shares, votes, reports and mirror announcements per day over the last 90 days |
| Sets | Every set the key shared, with its status and the reports against it |
| Votes | The key's votes, with the weight each one carries |
| Reports | The reports the key filed and their state |
| Networks | The networks its records came from |
| b4 versions | The b4 versions and capture engines it reported |
| Mirrors | The mirrors it announced |
| History | Every action taken on the key |

**Operator notes** attach a name, shown instead of the HMAC prefix everywhere in the console, a free-text note, and a tag:

| Tag | Effect |
| --- | --- |
| **staff** | A label only; the key's votes count like any other |
| **test** | The key's votes stop counting in published scores from the next build, its reports stop counting toward the automatic hide, and its activity is left out of the statistics |

| Action | Effect |
| --- | --- |
| **Ban** | See below |
| **Unban** | The key is accepted again; its sets return at the next build and its votes and reports count again |
| **Trust** | The key stops being counted against the per-key daily limits |
| **Untrust** | It is counted again |

:::info
Trust lifts the limits and nothing else. A trusted contributor's sets still wait for a moderator like everyone else's, and the per-network limits still apply.
:::

### Banning a key

A ban is a reversible quarantine. From the moment it is set:

- every new record signed by the key is refused;
- at the next build its sets leave the catalogue and move to the **Withheld** tab;
- its votes stop counting in published scores, and its reports stop counting toward the automatic hide;
- its pending versions can only be approved with an override, and stay out of the catalogue while the ban lasts.

Nothing is deleted, and unbanning restores all of it. The ban dialog counts what the ban affects before it is confirmed: listed sets, pending versions, votes and the strategies they count toward, and open reports. It also names the mirrors the key announced, which a ban leaves announced; rejecting them on the Mirrors page is what stops that.

## Mirrors

Every hub that announced itself as a mirror of this one, pending first, in the columns **Mirror**, **Status**, **Check**, **Serves** and, on a wide screen, **Last announced**. **Mirror** holds the URL, which opens the mirror in a new tab, and under it the b4hub version the mirror reported in its last announcement and the key that announced it. The details of every column are in its tooltips. **Status** combines the decision with what the manifest does with the mirror:

| Status | Meaning |
| --- | --- |
| **pending** | Waiting for a decision |
| **rejected** | Never announced; the tooltip carries the reason |
| **announced** | Approved and in the manifest |
| **joining** | Approved, and joins the manifest at the next build |
| **leaving** | Leaves the manifest at the next build |
| **kept** | Failing its check, and kept in the manifest while no mirror passes its check |
| **not announced** | Approved, and joins the manifest after its next passing check |

**Check** is the result of the last health check, **ok** or **failed** with how long ago it ran, or **not checked yet**.

| Action | Effect |
| --- | --- |
| **Approve** | A health check runs at once, and the mirror is listed in the manifest once one passes |
| **Reject** | It stays in the list with the reason and is never announced. A reason is required |
| **Remove** | It is forgotten, and can announce itself again |
| **Check now** | Checks one mirror; **Check all** checks every approved one |

The hub checks every approved mirror every 10 minutes, and once more before the first build after it starts. A check fetches the mirror's health endpoint and its manifest and verifies that the manifest is signed by this hub; the tooltip of a failed check names the step that failed. An approved mirror stays in the manifest for 24 hours after its last passing check, and the tooltip of its status gives the time it drops out without another one. When every approved mirror fails at once, the ones the previous manifest listed stay in it until one passes again or a moderator rejects or removes them; their status reads **kept**, with no drop-out time. When a round of checks changes the set of mirrors that should be announced, a build is requested.

The **Serves** column compares the catalogue the mirror had at its last check with this hub's: **current**, **behind by** some sequence numbers, **stale** when it is still behind 15 minutes after the newer catalogue was published or holds an older epoch, or **ahead of this hub**. Its tooltip names the manifest the mirror served.

## Feedback

Two tabs.

**Votes** lists every vote, newest first, and loads older ones on request. Per row: when it arrived, the set and version, its kind, the network the hub observed and the one the router claimed, the contributor, the domain the list was filtered by, the b4 version and engine, and the weight it carries. Filters narrow it by works or broken, by whether the voter is the set's author, by known or unknown network, and by **ASN** and **Country**, both picked from the networks and countries votes have arrived from, each with its total number of votes; a chosen country leaves in the **ASN** list only the networks whose votes come mostly from it. A set or key reference adds a filter for it.

The weight column shows what the vote contributes to the published score: its base weight, the factors for the network, a young key and age, and the result as of the last build. A vote that does not count says why: no listed set uses its strategy, it arrived after the last build, its key is banned or tagged test, or automated votes would outweigh manual ones.

The kinds are the works and broken votes made by hand, the upload counted as a works vote for the author, and the ones a router could send on its own from the DPI detector, the domain watchdog and Discovery. The hub defines and weighs those automatic kinds; b4 does not send them yet.

**Reports** is an inbox with three states:

| State | Meaning |
| --- | --- |
| **open** | Waiting for a moderator; counts toward the automatic hide |
| **dismissed** | Judged unfounded |
| **handled** | Acted on |

Each report carries when it arrived, the set and version, the network, the contributor, the reason typed in, and whether it counts. **Dismiss** and **Mark handled** take an optional note and apply to one report or to a selection; **Reopen** returns a report to the inbox. Hiding, rejecting, restoring and withdrawing close the open reports of what they touch and record why.

:::info
A listed version is hidden automatically once three open reports against it come from independent keys and networks. A report counts only while it is open, when its network is known, and when its key is neither banned nor tagged **test**. A report an approved mirror passed on with a valid relay signature has no known network, so it never counts. Restoring a version dismisses its open reports, so the count starts again from zero.
:::

## Catalogue

The published files, the mirrors the manifest lists, the key this hub signs with and the keys the manifest revokes.

**Build history** records every build: when it ran and why, whether it succeeded, the sequence number, the number of sets, how long it took, and what changed in the catalogue: sets added, removed, given a new version, given new text or rescored, and changes to the mirror list and the revoked keys. A build that changed nothing but the scores says so. The list can be narrowed to changes and failures, or to failures only.

Three operations, each of which republishes:

| Operation | What it does |
| --- | --- |
| **Build now** | Queues a build that publishes from the store even when nothing changed |
| **Start a new epoch** | Resets the sequence number. Every router treats the new epoch as authoritative and downloads again |
| **Revoke a hub signing key** | Adds a key id to the list every manifest carries. Routers stop trusting anything signed by it |

A build is refused, and the build history gives the cause, in two cases: when its epoch and sequence number would not be above the catalogue already published or the one an approved mirror serves, which is what a database restored from an older backup looks like, and when a database that has never published would replace the published catalogue or sign an empty one with a key built into b4. `--new-database` on `serve` or `build` confirms the second case.

:::warning
A new epoch is what publishes a catalogue after the database was restored from an older backup: routers refuse anything older than what they already hold, and the hub refuses to build it until the epoch changes. Outside that case it makes every router download the catalogue again for no gain.
:::

:::danger
Routers keep every revocation forever; removing a key from the hub's list later does not undo it. The hub refuses to revoke the key it signs with, and revoking the key built into b4 needs a separate confirmation, because it cuts off every router that trusts that key.
:::

:::info
Revoking and banning are different tools. Revoking names the signing key of a hub or a mirror and travels to every router in the manifest. Banning names a contributor and only affects what this hub accepts and counts.
:::

## Statistics

Counts over the last 7, 30, 90 or 365 days, by UTC day. Keys tagged **test** are left out everywhere on this page.

| Section | What it shows |
| --- | --- |
| Totals | Shares with the number of duplicates, votes split into works and broken, new keys, reports and the sets they hid |
| Activity | Shares, duplicates and new keys per day; works votes, broken votes and reports per day |
| Moderation and publishing | Approvals, rejections, hides, automatic hides and withdrawals per day; builds, failed builds and mirror announcements per day |
| Published scores | The global score of every set in the current catalogue in ten bands, separating sets with votes from at least two devices from those with fewer; the median, and how many sets carry a low-score or stale mark |
| Most voted sets | The sets the most keys voted on in the range |
| Where votes come from | Votes and keys by country and by network, from the address each vote arrived from, leaving out votes an approved mirror passed on with a valid relay signature, which carry no network; and keys by the b4 version and engine of their latest record |

## Audit log

Every action taken on the hub is recorded with its time, who took it, the target, the reason, and the fields it changed. Who is one of **console**, **password login** (basic authentication instead of the session), **command line** or **automatic**, such as the automatic hide; console and password entries also carry the address the request came from.

The log is filtered by target kind, by who, and by an exact target id, and loads older entries on request. Actions taken together in a bulk dialog carry a **bulk** mark that shows the whole batch. The same history appears in the panel of each set and each key.

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

**Saved reasons** holds the preset reasons offered in the decision dialogs, one list per kind of action, each with a short label, the text and how often it was used.

### Notifications

The hub can send a digest of what arrived to a Telegram chat, to a webhook, or to both.

| Channel | Settings |
| --- | --- |
| Telegram | A bot token and a chat id: a number, or `@name` of a public channel. The bot has to be a member of the chat |
| Webhook | A URL, `https://` or plain `http://` to a loopback or private address, and an optional signing secret |

Each setting can also come from the service environment, `B4HUB_TELEGRAM_TOKEN`, `B4HUB_TELEGRAM_CHAT`, `B4HUB_WEBHOOK_URL` and `B4HUB_WEBHOOK_SECRET`, and a value set there overrides the console, which then shows the field as read-only. Values entered in the console are stored encrypted with a key derived from the hub's `secret` file and are never sent back to the browser: the page shows only that a value is stored, with the bot id or the URL's scheme and host.

| Event | Sent when |
| --- | --- |
| New sets in the queue | A share is accepted and waits for a decision |
| Reports | A report is accepted |
| Hidden by reports | A listed version is hidden automatically |
| Failed builds | A catalogue build fails, with its error |
| New mirrors | A mirror announces itself and waits for approval |

New sets and reports are selected until the settings are first saved. Shares and reports from keys tagged **test** are not sent.

The first event after a quiet period goes out at once; later ones are grouped into at most one message per digest interval, 60 seconds by default and anything from 30 to 3600. A message counts each kind, lists up to ten events of each, ends with a link to the console page that deals with them, and is written in English or Russian. Enabling a channel or an event does not replay what arrived before.

A failed delivery is retried with a growing delay, from 30 seconds up to 30 minutes, and honours `Retry-After`; nothing is lost while it fails, because each channel keeps its own position in the event stream. Each channel shows when it last delivered, how many messages it sent, and the last error while it keeps failing. **Send a test** delivers a test message through a saved channel, enabled or not.

The webhook receives a `POST` with a JSON body:

```json
{
  "hub": "https://hub.example",
  "sent_at": "2026-09-27T12:41:15Z",
  "counts": { "share": 2, "report": 1 },
  "events": [
    { "kind": "share", "at": "2026-09-27T12:40:58Z", "set_id": "01J9...", "version": 1, "title": "Example", "author": "70837b639d58b777", "asn": "12389", "country": "RU", "url": "https://hub.example/admin/sets/01J9.../v/1" }
  ],
  "more": 0,
  "console": "https://hub.example/admin/queue"
}
```

Every request carries `X-B4hub-Timestamp`, the Unix time of the send. With a secret set it also carries `X-B4hub-Signature: sha256=<hex>`, the HMAC-SHA256 under the secret of the timestamp, a dot and the raw body. Redirects are not followed, and any status outside 2xx counts as a failure.

## Without the console

`b4hub moderate` does from the command line what the console does, through the same checks and with the same audit entries. It opens the same database directly, in WAL mode with a busy timeout, so the service can stay up. It publishes nothing itself: every change leaves a build request that a running `b4hub serve` picks up within about ten seconds.

| Commands | What they do |
| --- | --- |
| `list`, `approve`, `reject`, `hide`, `restore` | The queue and the version decisions. A set is named as `<id>` or `<id>/<version>`; several can be given at once, and `approve` is all or nothing |
| `withdraw`, `reinstate`, `delete` | Whole sets. `delete` needs `--confirm <id>` |
| `ban`, `unban`, `trust`, `untrust` | Keys, named by their HMAC |
| `reports`, `dismiss-report`, `resolve-report`, `reopen-report` | The report inbox |
| `mirrors`, `approve-mirror`, `reject-mirror`, `remove-mirror`, `check-mirror` | Mirrors |
| `status`, `builds`, `epoch`, `revoke` | Publishing, the build history, a new epoch, and revoking a hub signing key with `--confirm <key_id>` |
| `audit` | The audit log |

Editing a version, editing a key's name, note and tag, saved reasons, notifications and the limits exist in the console only.
