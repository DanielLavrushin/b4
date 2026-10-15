---
sidebar_position: 5
title: RST Injection Protection
description: Dropping the connection resets that DPI injects in the server's name.
---

Some DPI breaks connections by sending a forged TCP reset (RST) in the server's name. The device takes it for a real one and closes the connection. RST injection protection drops resets that do not look like the server's own. It is turned on per set, in the **RST Injection Protection** section of the [General](./general.md) sub-tab.

| Field | What it does | Default |
| --- | --- | --- |
| **Enable RST Protection** | Turns the protection on for the set. | Off |
| **TTL Tolerance** | The largest allowed difference between the TTL of a reset and the TTL of the server's first reply, 1 to 20. | `3` |

b4 drops a reset when, for example:

- it arrives before the server has answered at all;
- it is not the first reset on the same connection;
- its TTL differs from the TTL of the server's replies by more than **TTL Tolerance**, a sign that it came from another point in the network.

A tolerance of 3 fits most networks. When b4 drops resets that the server really sent, a larger value is the fix. Each dropped reset appears in the [logs](../../logs.md) with its reason.

The protection pairs with [Escalation](../escalation.md): it keeps the current page working while b4 detects a block, and the **Escalation** tab suggests turning it on.
