---
sidebar_position: 2
title: TCP
description: The TCP tab of the set editor and its three sub-tabs.
---

The **TCP** tab holds the set's DPI bypass strategy for TCP connections, such as HTTPS. It has three sub-tabs:

| Sub-tab | What it holds |
| --- | --- |
| [General](./general.md) | The packet limit, delay and port filter, packet duplication, IP block detection, [RST injection protection](./rst-protection.md) and MSS clamping. |
| [Splitting](./splitting.md) | How b4 splits the first data packet of a connection, the TLS ClientHello. |
| [Faking](./faking.md) | Fake packets that DPI reads and the server discards, and changes to the real ClientHello. |

[Discovery](../../discovery.md) tests strategies against a blocked site and writes the values that work into a set. The strategy it suggests is the first one to try, and manual changes on these sub-tabs are for tuning it.

A set can send its traffic through a proxy, or through a tunnel interface that is up, on the [DNS & Routing](../routing.md) tab. b4 then leaves its packets alone: splitting, faking, packet duplication and IP block detection do not apply.
