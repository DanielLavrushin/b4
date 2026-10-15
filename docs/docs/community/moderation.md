---
sidebar_position: 6
title: Moderation console
description: Approving sets, handling reports, keys and mirrors, and publishing the catalogue of a hub.
---

Every version of every set waits for a moderator before it reaches the catalogue, and the console is where that decision is made. It is part of `b4hub`, served at `/admin` on the hub's address, in English and Russian. A mirror has no console, see [Hosting a hub or a mirror](./hosting.md).

The console names two things differently from the router: a **vote** here is a **Works** or **Broken** report on the router, and a **report** here is a complaint.

## Signing in

The password is `B4HUB_ADMIN_PASSWORD` from the service environment. There are no accounts or roles: whoever has the password is a moderator. A session lasts seven days, and changing the password ends every open session. An address gets ten sign-in attempts in 15 minutes. The session cookie is marked secure when the request carries `X-Forwarded-Proto: https`, which the reverse proxy is expected to set.

Without the password, every sign-in is refused, and `b4hub moderate` is the only way to moderate, see [Without the console](#without-the-console).

## Publishing

A decision changes the store at once. The catalogue routers download is rebuilt in the background within about 10 seconds, so a burst of decisions ends in one build. A failed build does not undo a decision: routers keep the previous catalogue until a build succeeds.

**Health** on the **Overview** page lists what needs attention, each item with a link: a failed build, a manifest about to expire, a version pending for over 72 hours, a failing mirror or open reports. The menu counts pending versions, listed sets that need attention, pending mirrors and open reports.

## Queue

One card per pending version, oldest first, laid out like a set card on the router. The strip across the top lights the parts the set configures: **Target**, **Split**, **Fake**, **Route** or **Block**, and **DNS**. A click on a part opens its settings. The tooltip of the line under the title gives the network the upload came from.

The author is a 16-character HMAC of the contributor's key under the hub's secret. The key itself is never stored.

The version's tooltip says what approving would do, such as **New version of a set listed as vN**. A version **Older than the listed vN** is outlined in orange: approving it changes nothing in the catalogue.

Marks under the targets point at what needs a closer look:

| Mark | Meaning |
| --- | --- |
| **blocks traffic**, **pins addresses**, **categories only**, **pattern match**, **needs payload** | What the set does, explained in each tooltip. **blocks traffic** is red; **categories only** and **pattern match** are orange |
| **similar sets: N** | The hub holds sets with the same targets, strategy, domains, title or author. Orange for a likely duplicate |
| **author banned**, **set withdrawn** | The version stays out of the catalogue even when approved |
| **edited**, **open reports: N** | A moderator changed the version, or reports wait on it |

A click on the card opens the set's panel at this version, with everything it holds. **Compare with listed vN** in the card's menu shows the difference from the listed version.

### The decisions

| Action | Reason | Effect |
| --- | --- | --- |
| **Approve** | none | The version becomes active and is listed at the next build |
| **Reject** | required | The version is refused, and the reason is recorded |
| **Hide** | optional | The version leaves the catalogue at the next build |
| **Edit** | none | Opens the editor, see [Editing a pending version](#editing-a-pending-version) |
| **Ban uploader** | optional | See [Banning a key](#banning-a-key) |

Every dialog states the effect on the catalogue before anything changes. Hiding a listed version lists the newest older active version instead, unless **Also withdraw the whole set** is ticked. Open reports on the version are closed with the decision, unless **Keep the open reports open** is ticked. **Reject as duplicate** fills the reason with the set that is duplicated.

A rejection is a verdict on a submission. Its reason stays in the console: the author's router only sees that the set is not in the catalogue. Hiding takes a version out of circulation, and a hidden version still counts as a duplicate of later shares with the same strategy.

A checkbox on each card, or **Select all**, applies one decision to up to 200 versions at once. Each reason dialog offers the reasons saved for that action, and **Save as a preset** keeps a typed one.

## Editing a pending version {#editing-a-pending-version}

A submission with a good strategy and flawed targets can be corrected instead of rejected. The editor holds the title, the description, a **Moderator note** visible in the console only, and one field per kind of target. **Show the full set JSON** edits everything else the set carries. The router's **Publish to hub** sends no description, and one can be written here.

**Check** runs the result through the checks of a fresh share. A change to the targets, title or description keeps the fingerprint, so the recorded votes still count. A change to the strategy changes the fingerprint and drops the votes for this version, and the published set no longer matches the author's copy. A result that duplicates a set the hub holds is refused. Warnings appear under the fields they concern, such as private domains, a `/0` range or a dropped pin.

b4 has no wildcards, and a plain entry covers every subdomain. Under **Domains**, the editor flags four mistakes, and **Apply suggestions** fixes them in one step:

| Check | Example | Suggestion |
| --- | --- | --- |
| Entry that never matches | `*.example.com` | `example.com` |
| `www.` entry with no apex | `www.example.com` alone | `example.com`, which covers both |
| Repeated entry | `Example.com.` after `example.com` | Drop it |
| Entry already covered | `cdn.example.com` beside `example.com` | Drop it |

**Save** keeps the version pending, and **Save and approve** approves it at once. Both need a check of the current content that found a change and no duplicate. The received set is kept, so the card and the panel show what changed.

### Editing the text of a listed version {#editing-the-text-of-a-listed-version}

**Edit title** on a version outside the queue changes its title and description and nothing else. Routers see no new version, the votes keep counting, and new users get the new title from the next build.

## Sets

Five tabs hold what was decided: **Listed**, the version of each set in the catalogue; **Superseded**, older approved versions; **Withheld**, sets withdrawn or by a banned author; **Hidden** and **Rejected**, with their reasons. Versions hidden by reports wait on **Hidden** for a review.

The cards add the score routers got in the last build: green from 70%, orange from 40%, red below. **next:** follows it when the next build changes the score. On **Listed**, **Needs attention** keeps the sets with a mark that routers never see:

| Mark | When |
| --- | --- |
| **low score** | At least 3 weighted votes and a score under 30% |
| **stale** | No vote for 60 days, and almost no weight left |
| **under 50%** | Enough independent votes to show a score, and it is under 50% |
| **open reports** | The listed version has open reports |

A card opens the set's panel with every version, its votes and its **History**. A hidden version can be restored there, and a rejected one approved after all.

**Withdraw set** in the panel takes the whole set out of the catalogue at the next build, and **Reinstate** brings back exactly what was listed. A new version of a withdrawn set still enters the queue. **Delete permanently** removes the set with its versions, votes, reports and unused payload files, once the set id is typed in. It is meant for an entry that should never have existed. Routers that applied the set keep their copy.

## Keys

One row per contributor, by HMAC, with its activity and last network. The search also takes the key id a router shows under **Author identity**. **Operator notes** attach a name shown in place of the HMAC, a note and a tag. The tag **staff** is a label only. The tag **test** stops the key's votes and reports from counting, and leaves its activity out of the statistics.

**Trust** stops counting the key against the per-key daily limits; its sets still wait for a moderator, and the per-network limits still apply. **Untrust** reverses it.

### Banning a key {#banning-a-key}

A ban is a reversible quarantine. From the moment it is set:

- every new record signed by the key is refused;
- at the next build, its sets leave the catalogue for the **Withheld** tab;
- its votes stop counting, and its reports stop counting toward the automatic hide;
- its pending versions can be approved only with a second confirmation, and stay out of the catalogue.

Nothing is deleted, and **Unban** restores it all at the next build. The ban dialog counts what is affected, and names the mirrors the key announced. A ban leaves them announced; rejecting them on the Mirrors page takes them off.

## Mirrors {#mirrors}

Every hub that announced itself as a mirror of this one, pending first, with the key that announced it.

| Status | Meaning |
| --- | --- |
| **pending** | Waiting for a decision |
| **rejected** | Never announced. The tooltip gives the reason |
| **announced** | Approved and in the manifest |
| **joining**, **leaving** | Joins or leaves the manifest at the next build |
| **kept** | Failing, and kept in the manifest while no mirror passes its check |
| **not announced** | Approved, and joins the manifest after its next passing check |

**Approve** runs a health check at once, and the mirror is listed once one passes. **Reject** needs a reason, and the mirror is never announced. **Remove** forgets the mirror, which can then announce itself again. **Check now** and **Check all** run the health check on demand, and the tooltip of a failed check names the step that failed. **Serves** compares the mirror's catalogue with this hub's: **current**, **behind by** some builds, **stale**, or **ahead of this hub**.

## Feedback

**Votes** lists every vote with the weight it carries, filtered by kind, **ASN** or **Country**. A vote that does not count says why, for example that its key is banned or tagged **test**.

### How votes become the score

Votes count per strategy, in three cells: the ASN and the country the hub saw the vote come from, and worldwide.

| Vote | Weight |
| --- | --- |
| Works | +1 |
| Broken | -1 |
| The publication itself, as a works vote by the author | +1 |
| A vote without a known network, such as one relayed by an approved mirror | a quarter of the above |
| A vote from a key first seen less than a week ago | a quarter of the above |

The two reductions multiply. Every vote loses half its weight every two weeks. The score is `(works + 1) / (all + 2)`. A router uses a cell once at least two devices voted in it with a total weight of at least one. The hub also defines capped weights for automated votes from the DPI detector, the watchdog and Discovery, which b4 does not send yet.

### Reports

**Reports** is the inbox of complaints. A report is **open** until a moderator marks it **dismissed** or **handled**, and **Reopen** returns it. Hiding, rejecting, restoring and withdrawing close the open reports of what they touch.

A listed version is hidden automatically once three open reports against it come from independent keys and networks. Only reports with a known network count, from keys neither banned nor tagged **test**. Restoring a version dismisses its open reports, so the count starts again.

## Catalogue

The page shows the published files, the mirrors and revoked keys the manifest lists, and **Build history**: every build, why it ran, its result and what it changed.

| Operation | What it does |
| --- | --- |
| **Build now** | Builds from the store even when nothing changed |
| **Start a new epoch** | Resets the sequence number. Every router treats the new epoch as newer and downloads the catalogue again |
| **Revoke a hub signing key** | Adds a key id to the list every manifest carries. Routers stop trusting anything signed with it |

A build that would not be newer than the published catalogue, or than one an approved mirror serves, is refused. That follows a restore from an older backup, and a new epoch publishes it, see [Keeping the data](./hosting.md#keeping-the-data). Otherwise, a new epoch only makes every router download again.

:::danger
Routers keep a revocation forever. The hub refuses to revoke its own signing key, and revoking the key built into b4 needs a separate confirmation, because it cuts off every router that trusts that key.
:::

Revoking names the signing key of a hub or a mirror and reaches every router. Banning names a contributor and affects only this hub.

## Statistics and audit log

**Statistics** shows activity per UTC day, the spread of published scores and where votes come from. **Audit log** records every action with its time, actor, target, reason and changed fields. The actor is **console**, **password login**, **command line** or **automatic**.

## Settings {#settings}

Six limits apply from the next request:

| Limit | Per | Default |
| --- | --- | --- |
| Shares per key | day | 5 |
| Votes per key | day | 20 |
| Reports per key | day | 20 |
| Mirror announcements per key | day | 48 |
| New keys per network | day | 5 |
| Requests per network | hour | 2000 |

A day is the UTC day and an hour the clock hour. The counters live in memory and start over when the service restarts. A network is the sender's `/24`, or `/48` for IPv6. A refused record or a duplicate share does not spend the quota. What a router shows at a limit is described under [Limits](./feedback.md#limits).

**Saved reasons** edits the preset reasons of each decision dialog.

### Notifications {#notifications}

The hub sends a digest of new events to a Telegram chat, a webhook, or both. Telegram needs a bot token and a chat id: a number, or `@name` of a public channel, with the bot a member of the chat. A webhook needs a URL, `https://` or plain `http://` to a loopback or private address, and an optional signing secret.

`B4HUB_TELEGRAM_TOKEN`, `B4HUB_TELEGRAM_CHAT`, `B4HUB_WEBHOOK_URL` and `B4HUB_WEBHOOK_SECRET` in the service environment override the console, which then shows the field read-only. Values entered in the console are stored encrypted with a key derived from `secret`.

The events are new sets in the queue, reports, versions hidden by reports, failed builds and new mirrors. New sets and reports are selected until the settings are first saved. The first event after a quiet period goes out at once, and later ones at most once per digest interval: 60 seconds by default, from 30 to 3600. A failed delivery is retried with a growing delay, up to 30 minutes. **Send a test** checks a saved channel.

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

Every request carries `X-B4hub-Timestamp`, the Unix time of the send. With a secret set, it also carries `X-B4hub-Signature: sha256=<hex>`: the HMAC-SHA256, under the secret, of the timestamp, a dot and the raw body. Redirects are not followed, and any status outside 2xx is a failure.

## Without the console {#without-the-console}

`b4hub moderate` does from the command line what the console does, with the same checks and audit entries. It opens the database directly while the service runs. Every change leaves a build request that `b4hub serve` picks up within about 10 seconds.

| Commands | What they do |
| --- | --- |
| `list`, `approve`, `reject`, `hide`, `restore` | The queue and the version decisions. A set is named as `<id>` or `<id>/<version>`. Several can be given at once, and `approve` is all or nothing |
| `withdraw`, `reinstate`, `delete` | Whole sets. `delete` needs `--confirm <id>` |
| `ban`, `unban`, `trust`, `untrust` | Keys, named by their HMAC |
| `reports`, `dismiss-report`, `resolve-report`, `reopen-report` | The report inbox |
| `mirrors`, `approve-mirror`, `reject-mirror`, `remove-mirror`, `check-mirror` | Mirrors |
| `status`, `builds`, `epoch`, `revoke` | Publishing, the build history, a new epoch, and revoking a hub signing key with `--confirm <key_id>` |
| `audit` | The audit log |

Editing a version, key notes and tags, saved reasons, notifications and the limits are in the console only.
