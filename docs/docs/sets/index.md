---
sidebar_position: 6
title: Sets
description: The list of sets, the set card and the tabs of the set editor.
---

A set ties a list of targets, such as domains, addresses or networks, to what b4 does with their traffic: a DPI bypass strategy, a route through another interface or a proxy, or a block. The **Sets** page lists the sets and opens the set editor.

Guides that start here: [Unblocking a site](../guides/unblock.md), [Adding sites to a set](../guides/add-sites.md), [Blocking ads and sites](./blocking.md), [Importing from zapret or byedpi](./import.md) and [Sharing a set](./sharing.md).

![The Sets page with one set card](/img/index/20260418234612.png)

## Toolbar

| Control | What it does |
| --- | --- |
| **Enable All** / **Disable All** | Turns every set on or off at once. |
| **Search sets...** | Shows only the sets whose name, domains, GeoSite categories or ASNs contain the text. |
| **Select** | Selects several sets. **Delete** removes the selected sets after a confirmation, and **Cancel** leaves the selection. |
| **Import** | Builds sets from a byedpi or zapret command line, see [Importing from zapret or byedpi](./import.md). |
| **Create Set** | Opens the editor for a new set. |

## Set order

The handle at the top left of a card drags the set to another position. When several sets could match the same traffic, b4 picks the most specific match, not the first set in the list, and prefers a set limited to source devices. The order decides only between matches that are equally specific. Routing is the exception: the length of the range plays no part there. Sets limited to selected devices or source interfaces come first, and among sets of the same kind the one higher in the list wins.

[How sets match traffic](../internals/matching.md) describes the full order of checks.

## Set card

A click on a card opens the set in the editor.

| Part | Shows |
| --- | --- |
| Switch and number | The switch turns the set on or off. The number is the set's position in the list. |
| Name and targets | The set's name and a summary of its targets. |
| Route line | What the set does with the traffic it matches: **Default route** for a set that only applies a bypass strategy, the interface or `socks5` proxy it routes through, **Telegram bridge**, or `block` with the block action. |
| Facet rail | The marks along the top of the card: **Target**, **Split**, **Fake**, **Route**, **DNS** and **Escalate**. A mark opens a summary of that part of the set, with a link to its tab in the editor. **Compare across sets** above the list opens the same part on every card at once. |
| Badges | **escalates to** names the [backup set](./escalation.md), and **fallback for** names the sets that use this one as their backup. **N ASN unresolved** means an [ASN](./targets.md#asn) has no prefixes yet. **Watchdog** appears while **Keep this set working with the watchdog** is on for the set, and opens the [watchdog](../watchdog.md) page. **Imported shared set** and **Community set** mark a set that came from someone else. |

The menu at the top right of a card holds:

| Item | What it does |
| --- | --- |
| **Edit** | Opens the set in the editor. |
| **Duplicate** | Creates a copy of the set. |
| **Compare** | Opens **Compare Sets** with this set and another side by side. **Differences only** hides the fields that match. |
| **Find a strategy** | Opens [Discovery](../discovery.md) for the set. Not shown for a set that routes its traffic. |
| **Delete** | Deletes the set after a confirmation. |

## Set editor

The top bar holds the set's name. **Save Changes**, or **Create Set** for a new set, saves it, and so does the round button at the bottom right. **Back** and **Cancel** return to the list without saving.

![The set editor on the Targets tab](/img/index/20260418234644.png)

| Tab | What it holds |
| --- | --- |
| [Targets](./targets.md) | Which traffic the set applies to: domains, GeoSite categories, addresses, GeoIP categories, ASNs and source devices. |
| [TCP](./tcp/index.md) | The DPI bypass strategy for TCP, on the **General**, **Splitting** and **Faking** sub-tabs. |
| [UDP](./udp.md) | Which QUIC and other UDP traffic the set matches, and what b4 does with it. |
| [DNS & Routing](./routing.md) | The DNS redirect, routing through an interface or a proxy, blocking, and the set's DSCP value. |
| [Escalation](./escalation.md) | A backup set for the sites this set keeps failing on. |
| [Discovery](./discovery.md) | The addresses Discovery tests for the set, the last search for it, and the watchdog switch. |
| [Import/Export](#import-and-export) | The set as JSON, and a shared set for other people. |

## Import/Export {#import-and-export}

The tab shows the set as JSON under **Set Configuration JSON**. **Copy JSON** copies it. A JSON pasted into the field and loaded with **Import** replaces the content of the editor, and it takes effect after **Save Changes**.

The JSON is for moving a set between one's own routers. It leaves out what only makes sense on this router:

- the source devices, and with them the set's own [DSCP value](./routing.md#dscp);
- the [Escalation](./escalation.md) settings, which point to other sets;
- the watchdog switch. The [Discovery addresses](./discovery.md) are carried, and the watchdog switch of the set the JSON is imported into stays as it was.

These are set again on the other router. A set can use a payload file that this router does not have. A warning after the import then names the file, and the fake packets use a built-in payload until it is uploaded under **Settings > Payloads**.

A set for someone else is a [shared set](./sharing.md), which leaves out everything tied to this router. With the Community Hub switched on under **Settings > Integrations**, the same tab prepares one. The **Import** field accepts both forms.
