---
sidebar_position: 4
title: Publishing a set
description: Offering a set to other b4 users through the Community Hub.
---

A set that works can be published to the Community Hub. Other b4 users find it on their **Community** page once a moderator approves it. b4 publishes the set as a [shared set](../sets/sharing.md): the strategy and the targets, without anything tied to this router.

## Requirements

- The Community Hub is on, see [Switching it on](./index.md#switching-it-on).
- The set is saved. Unsaved changes are not published.
- The set was tested on the targets it lists. A domain it was never tried on collects **Broken** reports. A set that selects UDP packets by protocol has no targets and is tested with a call through it.

## Setup

1. Open the set and go to the **Import/Export** tab.
2. In **Share with other b4 users**, click **Prepare shared set**.
3. Read **Check before sharing** and **Left out of the shared set**, and correct the set where needed.
4. Click **Publish to hub**.

![The Share with other b4 users section of the set editor](/img/community/20260914230200.png)

## Preparing {#preparing}

**Prepare shared set** shows what is sent. **Check before sharing** lists what deserves a second look, such as domains that look private to this network or pins everyone importing the set will use. **Left out of the shared set** lists every dropped setting with the reason. Routing to a proxy or an interface, device filters, escalation and the DNS server address are always dropped, see [What leaves the router](../sets/sharing.md#what-leaves-the-router). The JSON below, with **Copy** and **Download**, hands the set over [directly](../sets/sharing.md).

The hub refuses a set without targets, except one that [selects UDP packets by protocol](../sets/udp.md#protocol-selection). Such a set is published with its protocols and UDP ports.

The published title is the set's name. The router sends no description, and a moderator can add one on the hub.

## Checking the result

The section shows the id the hub gave the set and its status, **pending moderation**. The set appears in the catalogue, for other users and for this router, once a moderator approves it and the router syncs. There is no notification of the decision. Until then, **Open in Community** opens **Set details** with a note that the set is not in the catalogue.

The published set is now linked to the hub set, like an applied one: its card on the **Sets** page shows **Community set**. A version that a moderator hides later leaves the catalogue, and routers that applied it keep it.

## Versions {#versions}

After the strategy of a published set changes, the button reads **Publish as new version**. Publishing then adds the next version to the same hub set, and routers that applied the previous one see **Update to version N**. When the original set was published by someone else, the hub creates a new set instead.

A publication whose strategy and targets match a version the hub already holds is not stored. The hub names that set, and the upload counts as a **Works** report for it.

For a set that selects UDP packets by protocol, the selected protocols and the UDP ports count as its targets, both for new versions and for duplicates.

## Author identity {#author-identity}

Every record the router sends to the hub is signed with its author key. The hub knows the router by this key: new versions of a set can be published only under the key that published it, and the moderators can trust or ban the key. The catalogue shows an author label derived from the key, never the key itself.

The key is created on first use and kept in `.hub/` in the configuration directory. A backup made under **Settings > System > Backup** does not include it. A router restored from a backup starts with a new key, and the hub gives a key it first saw less than a week ago less weight in the scores.

**Settings > Integrations > Community Hub > Author identity** shows the key and two actions:

- **Show recovery code** shows the code the key is rebuilt from. Anyone with the code can publish and report as this author, so the code is kept private.
- **Restore from code** replaces this router's key with the one the code describes. The sets published under it can then get new versions from this router.

## When it does not work

| Message | Meaning |
| --- | --- |
| **Linked to community set ... nothing to publish.** | The set is linked to a hub set, and its strategy has not changed since. |
| **The hub refused the set: ...** | The message names the reason, such as a set without targets that does not select UDP packets by protocol, or the daily limit, see [Limits](./feedback.md#limits). |
