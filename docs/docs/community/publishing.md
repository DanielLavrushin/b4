---
sidebar_position: 4
title: Publishing a set
---

# Publishing a set

A set that works is published from the set editor. The **Share with other b4 users** section sits at the bottom of the **Import/Export** tab and is shown while the Community Hub is on. It does two things: it prepares the set as a [shared set](../sets/sharing.md), the JSON form that can be passed to someone by hand, and it publishes that same form to the hub.

![The Share section of the set editor](/img/community/20260914230200.png)

## Preparing

**Prepare shared set** builds the shared set from the current editor state and shows what happened to it:

- **Check before sharing** lists what the set carries that may not belong in a publication: domains or addresses that look private to this network, a pin everyone who imports the set will use, a DoH resolver outside the known public list, a payload file that is missing or unusable, a block set that matches by pattern, a set with too many domains or addresses to be carried.
- **Left out of the shared set** lists every setting that was dropped and why. Routing to a proxy or an interface, device filters, escalation and the DNS server address never cross; the [sharing page](../sets/sharing.md#what-leaves-the-router) has the full list.
- The b4 version the set needs, and how many payload files are included.
- The JSON itself, with **Copy** and **Download**.

The section is the same whether the set is published or handed over as a file, so the warnings apply to both.

:::tip
A set is best published with the targets it was tested on. A set that lists a domain it was never tried against collects broken reports from people who applied it for that domain.
:::

## Publishing

**Publish to hub** sends the saved version of the set. A new set has to be saved first, and unsaved changes in the editor are not what gets published, so the button says so until the set is saved. The published title is the set's name, and there is no separate description; the name is what everyone browsing the catalogue sees.

The hub runs the same checks as an import, refuses a set with no targets, stores the payload files by hash, and answers with the hub id and the version. The set enters the moderation queue as **pending**; other users, and this router, see it in the catalogue after a moderator approves it and the next catalogue is built and synced. The section shows the id and the status and offers **Open in Community**, which until approval opens the details dialog with a note that the set is not in the local catalogue yet.

The set is now linked to the hub set, the same way an applied set is: the card on the Sets page shows **Community set**, and the Share section says which hub set and version it is linked to with nothing to publish.

## Versions

Publishing a linked set again, after its strategy changed, creates the next version of the same hub set rather than a new one. The Share section then reads **Publish as new version**. The catalogue carries the newest approved version only, and everyone who applied the previous one sees **Update to version N** on their card.

The hub decides what counts as the same set:

- a set linked to a hub set published under this router's author key is its next version;
- a set that is not linked, or linked to someone else's set, becomes the next version of this author's existing set when it has the same targets, or the same title and at least one target in common;
- otherwise it is a new hub set, and when it started as someone else's community set, the hub records which one it was derived from.

A published set whose strategy and targets both match a set already in the catalogue, by anyone, is not stored twice. The hub answers that it is a duplicate, names the existing set, and counts the upload as a works report for it.

## Moderation

Every version waits for a moderator before it is listed. A moderator sees the set as it would be imported, approves it, rejects it, or hides it later, and reads the complaints that arrive about it. A rejected or hidden version disappears from the catalogue at the next build; routers that applied it keep it, since an applied set does not depend on the catalogue. There is no notification of the decision; **Open in Community** keeps reporting the set as not in the catalogue until it is approved and synced, and lists it afterwards.

:::info
The hub accepts a limited number of published sets from one router per day. The limit is set by the hub operator, and a publish over it is refused with a message naming the limit and when it resets.
:::

## Author identity

Everything a router sends to the hub is signed with its author key, generated on first use and kept as `.hub/identity.json` in the configuration directory. The hub knows the router by this key: the sets it published belong to it, its reports count as one router, and the moderators can trust or ban it. The key id is what the catalogue shows as the author of a set.

The key is what ties published sets to their author, which is what makes it the thing to carry over when a router is replaced. **Settings, Integrations, Community Hub, Author identity** shows the key id and two actions:

- **Show recovery code** displays the code the key can be rebuilt from. Anyone with the code can publish and report as this author.
- **Restore from code** replaces this router's key with the one the code describes. The sets published under that key are then this router's to version, and its reports carry the same key's history, including its age on the hub.

A backup made under **Settings, Backup** does not include the `.hub/` directory, so a router restored from one starts with a fresh key, and a fresh key carries the reduced weight the hub gives keys it has known for less than a week. The recovery code is the way to carry the identity across.
