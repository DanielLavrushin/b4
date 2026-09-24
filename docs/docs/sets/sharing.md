---
sidebar_position: 7
title: Sharing a set
---

A set can be handed to another b4 installation as a shared set: one JSON document that carries the strategy, the targets and the payload files the set uses, and nothing that is tied to the router it came from. The set editor prepares it under **Import/Export** once the Community Hub is switched on under **Settings, Integrations**. A shared set pasted into the **Import** field of any b4 is checked, its payload files are installed, and the set opens in the editor for saving.

:::info
The **Import/Export** JSON described under [Sets](./index.md#import-and-export) copies a set as it is, including routing, and is meant for moving a set between one person's own routers. A shared set is the form to pass to someone else. Both are accepted by the same **Import** field; b4 tells them apart by their shape.
:::

:::info
The same shared set is what **Publish to hub** sends to the community hub, where other b4 users find it on their **Community** page. Publishing, moderation and versions are described under [Community Hub](../community/publishing.md).
:::

## What leaves the router

A shared set is built from an allow-list of settings. Anything not on it is left out, and the preview names every setting that was dropped and why.

Carried:

- the TCP, UDP, fragmentation and fake packet settings, including port filters;
- the targets: domains, addresses, geosite and geoip category names, the TLS and IP version filters;
- the DNS redirect: whether it is on, the DoH URL, strict mode, and pins for domains the set targets; a DoH URL whose host is not a known public resolver is reported when the set is prepared, and the import drops the whole DNS redirect rather than send every query for the set's domains to an unknown resolver;
- the MSS clamp;
- routing in block mode, since a set that blackholes ads or trackers carries nothing private.

Left out:

- routing to an interface, a proxy or the Telegram bridge, together with the upstream host, port and credentials;
- device filters, which name MAC addresses of one network;
- escalation, which points at a set id that exists on one router only;
- the DNS server address the set redirects to;
- IP block detection;
- the [Discovery addresses](./discovery) and the watchdog switch, which describe what one network checks; the preview lists the addresses as private;
- the set's id and its enabled state.

A DNS pin survives only when the pinned domain is one the set targets and every address in it is public. A pin for another domain, or one pointing at a private address, is dropped and reported.

:::warning
Domains that look private to the sharing network are reported, not removed: a single label such as `nas`, an address literal, or a suffix such as `.lan`, `.local` or `.home`. They mean nothing on another network and are worth deleting before the set is passed on.
:::

## Payload files

A set whose fake packets use a captured ClientHello refers to a file under `captures/`. That file travels inside the shared set, so the person importing it runs the same fake packet as the author. A file is accepted only when it parses as a TLS ClientHello with a server name or as a QUIC Initial with a readable ClientHello, and only up to 16 KB. When the file named by the set does not exist on the sharing router, the reference is left out and the preview says so: on both sides the fake packets then fall back to a built-in payload, which is what the sharing router was already sending.

On import, each file is written under `captures/` and appears under **Settings, Payloads**. The name is the protocol and the server name read from the payload itself, dots replaced by underscores, such as `tls_www_google_com.bin`. An existing file with the same name and different contents is never overwritten; the imported one is saved under a name with a short hash suffix, and the set refers to that.

## What the import checks

The import runs on the router, not in the browser, and reports what it had to change:

- the shared set's format; a future format is refused rather than read partially;
- settings this b4 does not know, which are dropped with a note, since a newer b4 may have produced them;
- the minimum b4 version the set was prepared for, compared with the running one;
- values this b4 does not accept, such as a mode name from a newer release, which are replaced by defaults and named;
- each payload file's hash against its contents, and whether every payload the set refers to is present;
- geosite and geoip categories the set names that are missing from the databases on this router, or the absence of a database altogether;
- pins, to which the same rule applies as when the set was prepared: a pin for a domain the set does not target or one pointing at a private address is dropped and named, and the surviving pins are listed so they can be checked in the **DNS** tab before saving;
- a DoH URL whose host is not a known public resolver, which drops the DNS redirect and names the host.

The set itself is passed through the same validation as a set saved from the editor, so a shared set cannot carry a routing block, a device filter or a DNS server address even when the JSON was edited by hand to include one.

## Provenance

An imported set remembers the fingerprint of the strategy it arrived with. The fingerprint covers the packet-altering settings and the DNS redirect settings only, so changing the targets, the port filter or the pins does not count as an edit. The set card shows **Shared set** while the strategy is unchanged and **Shared set, edited** once it differs.

## Geosite categories

A category name means something only relative to the geosite file it is looked up in. The sources offered under **Settings, Geodat Settings** differ in what a category of the same name contains, and a category present in one may be absent from another. A shared set records the source URLs the author's router was using, and the import reports every category that the local database does not have. The categories are carried by name; the databases themselves are never part of a shared set.

:::info
Command lines from byedpi and zapret are a different import, described under [Import from another tool](./import.md).
:::
