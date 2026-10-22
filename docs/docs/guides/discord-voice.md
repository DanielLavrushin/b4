---
title: Discord voice calls
description: A set that sends fakes before the packets that open a Discord voice call, for networks where calls fail while the rest of Discord works.
---

The setup adds a set that sends fakes before the UDP packets that open a Discord voice call. It is needed when a call sends its voice packets but gets no answer, or drops after a few seconds, while the rest of Discord works. A call that hangs at **RTC Connecting** fails on TCP instead, see [When it does not work](#when-it-does-not-work).

The address of a voice server is not known in advance. The set therefore has no targets and handles only calls on the voice ports, see [Sets for calls](../sets/udp.md#protocol-selection). Once it takes the packet that opens a call, the rest of that call belongs to the set and passes untouched, whatever other sets match its address.

## Requirements

- b4 is installed and running.
- A set that already opens Discord itself, the app and text chat. [Adding sites to a set](./add-sites.md) adds Discord's domains to a set, and [The Discord set's TCP ports](#tcp-ports) lists the ports that set needs.

## Setup

1. Open **Sets** and click **Create Set**.
2. Enter a name for the set, such as `Discord voice`.
3. Leave the **Targets** tab empty.
4. On the **UDP** tab, in **Calls**, select **Handle only calls**. **Which calls** stays at **Discord app and browsers**, and a warning asks for a port filter.
5. Click **Use Discord ports** in the warning, or enter `19294-19344,50000-50100` in **Port Filter**.
6. Leave **Action Mode** at **Fake & Fragment**. **Fake Packet Payload** now shows **(auto: STUN request)**.
7. On the **TCP** tab, leave **Port Filter** under **General** empty.
8. Click **Create Set**.

| Ports | Voice servers |
| --- | --- |
| `19294-19344` | Discord's voice servers on Cloudflare |
| `50000-50100` | The other voice servers, for example in Finland |

With **Discord app and browsers**, the set takes calls and voice channels in the Discord app, and calls in a browser, Discord in a browser included. **Fake Packet Count** and **Fake Packet Size** keep their defaults.

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

The voice server picks one of these ports for the signalling connection of a call, and the set's strategy has to work on each of them. A strategy that opens `discord.com` on port 443 can still fail on the port of a voice server, and [Checking the voice server](#voice-server-check) tests that port. The [watchdog](../watchdog.md) checks a new strategy only on the set's Discovery addresses, and an address given as a domain is checked on port 443. A strategy it writes can then open Discord and break calls, so keeping the watchdog off for this set is the usual choice.

## Checking the result

1. Join a voice channel in Discord.
2. Open the [Traffic](../connections.md) page.
3. Turn on **All packets**. Voice packets carry no domain and show only with it on.
4. Switch to **Raw feed** in the top right corner.

The packet that opens the voice connection and the STUN requests show the voice set's name in the **Set** column, with a destination port from the two ranges. The packets that follow on the same connection show the voice set as well, with a `voice` [label](../connections.md#labels): the set passed them through untouched.

### Checking the voice server {#voice-server-check}

Before the first voice packet, Discord opens a TCP connection to the voice server. Its domain ends in `discord.media`, and its port is one of the ports in [The Discord set's TCP ports](#tcp-ports).

1. Join a voice channel in Discord.
2. On the [Traffic](../connections.md) page, switch to **Raw feed** and enter `domain:discord.media` in the filter field.
3. Note the domain of the latest row, such as `c-xxx.discord.media`, and the port in its destination.
4. On a computer behind the router, run the command below with that domain and port.

```sh
curl -s -o /dev/null -w '%{http_code}/%{size_download}\n' https://c-xxx.discord.media:2087/
```

| Output | Meaning |
| --- | --- |
| `403` and a few thousand bytes, such as `403/4548` | The server's answer passes. |
| `000/0` or `403/0` | The connection to this port is cut. |

## When it does not work {#when-it-does-not-work}

While a call hangs, the [Traffic](../connections.md) page with **All packets** on shows which side fails.

| The Traffic page shows | Cause |
| --- | --- |
| No TCP row with a `discord.media` domain | b4 does not capture the voice server's port: it is missing from the **Port Filter** of the set with Discord's domains. |
| A TCP row with a `discord.media` domain, and no UDP packet to the voice ports | The TCP connection to the voice server fails, and Discord sends no voice packet. [Checking the voice server](#voice-server-check) confirms it, and the set with Discord's domains needs a strategy that works on that port, see [Unblocking a site](./unblock.md). |
| UDP packets to the voice ports with the voice set's name | The voice server does not answer them, see the causes below. |

On some networks no strategy opens the voice server's port when it is not 443. The check then reads `000/0` whatever the strategy, and calls through that server need a [proxy](./proxy.md) or a [VPN](./vpn.md) for `discord.media`.

**b4 refuses to save the voice set.** A warning on the **UDP** tab names the cause: the set has targets, its **Port Filter** is empty, or the **TCP** tab lists ports. The set saves with no targets, both port ranges in the UDP **Port Filter** and an empty TCP **Port Filter**.

**No packet to the voice ports shows the voice set.** The voice set took neither the packet that opens the call nor the STUN requests. Their port is missing from its **Port Filter**, **Which calls** leaves that kind of call out, or the set is turned off on the **Sets** page. The **IP Version Filter** on the set's **Targets** tab can also exclude one IP version.

**No packets to the voice ports appear.** **All packets** is off, or the port is missing from the voice set's **Port Filter**: b4 sees UDP only on port 443 and on the ports that sets list. With **Enable IPv6 Support** off under [Settings > Core](../settings/core.md#protocols), b4 sees no IPv6 packets at all.

**Calls through one Discord server fail, and calls through other servers work.** A voice channel uses the region Discord picks automatically. When a provider blocks the addresses of that region's voice servers, no strategy helps. Someone who can manage the voice channel sets another region for it in the channel's settings in Discord.

**The voice server check passes over IPv4 and fails over IPv6.** With the **TTL** strategy, or with **Apply Fake TTL** on, the set with Discord's domains sends its fakes with one **Fake TTL** for IPv4 and IPv6. A voice server can be fewer hops away over IPv6, and a fake that reaches the server breaks the connection. A lower **Fake TTL** on the [Faking](../sets/tcp/faking.md#fake-strategy) sub-tab, checked over both IP versions, is the fix.

**Discord does not open at all.** The set with Discord's domains does not cover the TCP side. Its **Port Filter** needs the ports from [The Discord set's TCP ports](#tcp-ports), and its strategy has to open Discord, see [Unblocking a site](./unblock.md).
