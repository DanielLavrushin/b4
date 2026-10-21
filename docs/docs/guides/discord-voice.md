---
title: Discord voice calls
description: A set that sends fakes before the packets that open a Discord voice call, for networks where calls fail while the rest of Discord works.
---

The setup adds a set that sends fakes before the packets that open a Discord voice call. It is needed when calls do not connect, or drop after a few seconds, while the rest of Discord works.

The address of a voice server is not known in advance. The set therefore has no targets and selects packets by port and by protocol, see [Selecting packets by protocol](../sets/udp.md#protocol-selection).

## Requirements

- b4 is installed and running.
- A set that already opens Discord itself, the app and text chat. [Adding sites to a set](./add-sites.md) adds Discord's domains to a set, and [The Discord set's TCP ports](#tcp-ports) lists the ports that set needs.

## Setup

1. Open **Sets** and click **Create Set**.
2. Enter a name for the set, such as `Discord voice`.
3. Leave the **Targets** tab empty.
4. On the **UDP** tab, enter `19294-19344,50000-50100` in **Port Filter**.
5. In **Protocols**, select **Discord voice setup** and **STUN**.
6. Leave **Action Mode** at **Fake & Fragment**. **Fake Packet Payload** now shows **(auto: STUN request)**.
7. On the **TCP** tab, leave **Port Filter** under **General** empty.
8. Click **Create Set**.

| Ports | Voice servers |
| --- | --- |
| `19294-19344` | Discord's voice servers on Cloudflare |
| `50000-50100` | The other voice servers, for example in Finland |

**Discord voice setup** selects the packet that opens a voice connection in the Discord app. **STUN** selects the request that opens a call in a browser, Discord in a browser included. **Fake Packet Count** and **Fake Packet Size** keep their defaults.

### Importing the set {#import}

The JSON below creates the same set without the steps above. Settings it leaves out keep their defaults.

```json
{
  "name": "Discord voice",
  "udp": {
    "dport_filter": "19294-19344,50000-50100",
    "filter_l7": ["discord", "stun"],
    "fake_payload_file": "@stun"
  }
}
```

1. Open **Sets** and click **Create Set**.
2. Go to the **Import/Export** tab, paste the JSON into **Set Configuration JSON** and click **Import**.
3. Click **Create Set**.

The [Community Hub](../community/browsing.md) can carry a set of this kind as well. A zapret line for Discord voice with no host list or ipset of its own, such as `--filter-udp=19294-19344,50000-50100 --filter-l7=discord,stun --dpi-desync=fake`, converts into the same set on [import](../sets/import.md#domains).

### The Discord set's TCP ports {#tcp-ports}

The Discord app, text chat and the signalling connection of a call run over TCP. They belong to the set with Discord's domains: the GeoSite category `discord`, or domains such as `discord.com`, `discord.gg` and `discord.media`. b4 captures TCP port 443 and the ports that any set lists. Discord's other TCP ports therefore have to be in that set's list.

1. Open the set with Discord's domains.
2. On the **TCP** tab, enter `443,2053,2083,2087,2096,8443` in **Port Filter** under **General**.
3. Click **Save Changes**.

The list limits the set to these ports, and `443` keeps it on HTTPS.

## Checking the result

1. Join a voice channel in Discord.
2. Open the [Traffic](../connections.md) page.
3. Turn on **All packets**. Voice packets carry no domain and show only with it on.
4. Switch to **Raw feed** in the top right corner.

The packet that opens the voice connection and any STUN requests show the voice set's name in the **Set** column, with a destination port from the two ranges. The voice packets that follow show no set, or another set that matches their address. A `voice` [label](../connections.md#labels) next to them means that set has **Filter STUN Packets** on and passed them through untouched.

## When it does not work

**b4 refuses to save the voice set.** A warning on the **UDP** tab names the cause: the set has targets, its **Port Filter** is empty, or the **TCP** tab lists ports. The set saves with no targets, both port ranges in the UDP **Port Filter** and an empty TCP **Port Filter**.

**No packet to the voice ports shows the voice set.** The voice set selected neither the packet that opens the call nor the STUN requests. Their port is missing from its **Port Filter**, that kind of packet is not selected in **Protocols**, or the set is turned off on the **Sets** page. The **IP Version Filter** on the set's **Targets** tab can also exclude one IP version.

**No packets to the voice ports appear.** **All packets** is off, or the port is missing from the voice set's **Port Filter**: b4 sees UDP only on port 443 and on the ports that sets list. With **Enable IPv6 Support** off under [Settings > Core](../settings/core.md#protocols), b4 sees no IPv6 packets at all.

**The app or the call does not connect at all.** The set with Discord's domains does not cover the TCP side. Its **Port Filter** needs the ports from [The Discord set's TCP ports](#tcp-ports), and its strategy has to open Discord, see [Unblocking a site](./unblock.md).
