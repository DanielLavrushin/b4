---
sidebar_position: 7
title: Sharing a set
description: Handing a set to another b4 user as a shared set, and what the import on the other router checks.
---

A shared set is a set in a form that can be handed to another b4 user. It is one JSON document with the strategy, the targets and the payload files the set uses, and nothing tied to the router it came from. The same form is what **Publish to hub** sends to the [Community Hub](../community/publishing.md).

The plain JSON of the **Import/Export** tab, described under [Sets](./index.md#import-and-export), copies a set as it is, routing included. It suits moving a set between one's own routers. Both forms are pasted into the same field, and b4 tells them apart.

## Requirements

- On the router the set comes from, the Community Hub is on: the **Share with other b4 users** section is shown only then, see [Switching it on](../community/index.md#switching-it-on).
- The router that imports the set needs no hub.

## Setup

On the router the set comes from:

1. Open the set and go to the **Import/Export** tab.
2. In **Share with other b4 users**, click **Prepare shared set**.
3. Read **Check before sharing** and **Left out of the shared set**, and correct the set where needed.
4. Click **Copy** or **Download**, and pass the JSON on.

On the router that receives it:

1. Open **Sets** and click **Create Set**.
2. Go to the **Import/Export** tab, paste the JSON into **Set Configuration JSON** and click **Import**.
3. Read **Notes from the import**, and check the other tabs.
4. Click **Create Set**.

## What leaves the router {#what-leaves-the-router}

A shared set is built from a list of allowed settings. The preview names every setting that was dropped, and why.

Carried:

- the TCP, UDP, fragmentation and fake packet settings, with port filters;
- the targets: domains, addresses, geosite and geoip category names, ASNs, the TLS and IP version filters;
- the DNS redirect, with its DoH URL and pins for the set's own domains;
- the MSS clamp;
- block mode, since a block set carries nothing private.

Left out:

- routing to an interface, a proxy or the Telegram bridge, with the proxy's address and credentials;
- device filters and escalation;
- the DNS server address the set redirects to, and IP block detection;
- the [Discovery addresses](./discovery.md), the watchdog switch, the [IP priority](./targets.md#ip-addresses) switch and the set's own [DSCP value](./routing.md#dscp);
- the set's id and its enabled state.

ASNs travel as numbers. The router that imports the set fetches their address ranges itself.

Domains that look private to the sharing network are reported, not removed: a single label such as `nas`, an IP address in the domain list, or a name ending in `.lan`, `.local` or `.home`. They mean nothing on another network and are worth deleting before the set is passed on.

## Payload files {#payload-files}

A set whose fake packets use a captured ClientHello carries that file inside the shared set, so the importing router sends the same fake packets. A file is carried only when it parses as a TLS ClientHello with a server name, or as a QUIC Initial with a readable ClientHello, and only up to 16 KB. When the file is missing on the sharing router, the set is shared without it, and both sides use the built-in payload.

On import, the file appears under **Settings > Payloads**, named after the protocol and the server name, such as `tls_www_google_com.bin`. An existing file with the same name and other contents is never overwritten: the new file gets a short hash suffix.

## What the import checks {#what-the-import-checks}

The import runs on the router and lists every change under **Notes from the import**:

- A shared set in a newer format is refused. Settings this b4 does not know are dropped, and values it does not accept are replaced by defaults.
- The b4 version the set needs is compared with the running one.
- Each payload file must match its hash.
- Geosite and geoip categories missing from this router's databases are named. A category name means what the local database says, which can differ between the sources under **Settings > Geodat Settings**.
- ASNs whose address ranges this router has not fetched yet are named. They match nothing until b4 fetches them, which starts once the set is saved.
- A pin for a domain the set does not target, or for a private address, is dropped. The remaining pins are listed, to be checked on the **DNS & Routing** tab before saving.
- A DNS redirect to a DoH resolver outside the known public list is dropped.

Settings a shared set leaves out are removed even when the JSON was edited by hand to include them. A shared set imported into an existing set keeps that set's own DSCP value, and turns its **IP priority** switch off.

## Checking the result

The card of the imported set on the **Sets** page shows **Imported shared set**. Once the strategy is changed in the editor, the chip reads **Imported shared set, edited**. Targets, port filters and pins can be changed without that.

## When it does not work

**The shared set could not be imported: ...** names the reason, such as a format from a newer b4. **Invalid JSON format** means the pasted text is not valid JSON.

Command lines from byedpi and zapret are a different import, described under [Importing from zapret or byedpi](./import.md).
