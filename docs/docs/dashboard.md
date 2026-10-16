---
sidebar_position: 4
title: Dashboard
description: Whether b4 is running, which sets match traffic, and what needs attention.
---

The dashboard is the first page of the web interface. It shows whether b4 is running, whether the sets match traffic, and what needs attention.

![The dashboard](/img/dashboard/20261003220000.png)

The figures update every second. Some panels appear only when they have something to show, such as **Blocked** while a block set is enabled.

## Status strip

The strip at the top shows the state of the service.

![Status strip](/img/dashboard/20261003220010.png)

| Cell | Shows |
| --- | --- |
| **Status** | **Running** in normal operation. **Engine failed** means b4 is up but does not process traffic, see [below](#packet-engine-not-running). |
| **Engine** | How b4 captures traffic, `NFQUEUE` or `TUN`, and the number of worker threads. |
| **Firewall** | The firewall b4 works with, `iptables` or `nftables`. **restored N times** means something on the router removed b4's rules and b4 put them back. Some router firmware does this routinely, and it is not an error. **Set up externally** means **Skip IPTables/NFTables Setup** is on, so b4 installs no rules of its own. |
| **Uptime**, **b4 RAM**, **CPU** | How long b4 has been running and what it uses. Hovering a cell shows the peaks. |

When the live updates stop, a line under the strip says so and the panels dim. The page reconnects by itself, and **Retry** reconnects at once. After **Session expired - sign in again**, **Sign in** opens the login page.

### If the packet engine did not start {#packet-engine-not-running}

When the packet engine fails to start, the web interface keeps working, but traffic passes through the router untouched: no set applies. A card under the strip shows the error. b4 retries by itself up to three times, and the card shows when the next retry is.

![The packet engine card](/img/dashboard/20260928134207.png)

- **Switch to TUN** or **Switch to NFQUEUE** tries the other capture mode, for example TUN when the kernel lacks the modules NFQUEUE needs.
- **Restart b4** starts again once the cause is fixed, such as a missing kernel module.
- **Engine settings** opens **Settings > Core > Packet Engine**.

## Needs attention {#needs-attention}

The list appears only when something needs action, errors first. The link on each line opens the place where the problem is fixed.

![Needs attention](/img/dashboard/20261003220020.png)

| Message | Meaning and action |
| --- | --- |
| **Set X: proxy ... unreachable** | The proxy of a [proxy set](sets/routing.md#upstream-socks5-proxy) does not answer: the proxy server is down, or its address on the set's **DNS & Routing** tab is wrong. |
| **Telegram bridge: ...** | [Telegram over WebSocket](telegram/websocket-bridge.md) cannot work. The message names the cause, and [Troubleshooting](telegram/troubleshooting.md#the-telegram-over-websocket-card-shows-a-warning) explains each one. |
| **Watchdog checks fail for X** | Some pages of the set stopped opening. The [watchdog](watchdog.md) keeps checking and searches for a new strategy if the failures continue. |
| **The watchdog is looking for a working strategy for X** | A search for the set is waiting or running. No action is needed. |
| **The watchdog cannot check X** | None of the set's pages can be checked, for example because another set handles them. The second line gives the reason. |
| **The watchdog gave up on X; its sites may not open** | Three attempts in a row to find a new strategy failed, and the watchdog stopped searching for this set. Searches resume after **Check now** on the **Watchdog** page, or once a check finds every page loading, for example after a manual [Discovery](discovery.md) run for the set. |
| **The watchdog is off, but N sets ask for it** | Either the watchdog is turned on under **Settings > Discovery**, or **Keep this set working with the watchdog** is turned off in those sets. |
| **No packets reached b4 in the last N min** | While devices are in use, this means their traffic is not reaching b4, and no set applies. The link opens the packet engine settings. [No packets reached b4](troubleshooting/traffic.md#no-packets) lists the causes and fixes. |
| **SOCKS5 server did not start**, **MTProto server did not start**, **Web server error** | That part of b4 did not start. The second line shows the error, for example a port that another program already uses. |
| **Web server TLS is not usable, serving plain HTTP instead** | The certificate or key under **Settings > System > Web Server** cannot be used, so the web interface runs over plain HTTP. The second line shows the error. |
| **Some set targets could not be loaded** | Some set targets were not loaded. The second line shows the error, and the link opens **Sets**. |
| **b4 was updated on disk; restart to run it** | **Restart** runs the new version. |
| **b4 skipped the bypass for N packets**, **b4's packet queue overflowed** | b4 could not keep up with the traffic for a moment, and some connections got no bypass. |
| **The router's connection table is N% full** | The router is close to its limit of open connections, and new connections may fail. The limit belongs to the router, not to b4. |
| **b4 holds N OS threads** | b4 uses far more threads than normal and may stop. A restart clears the state. For a [bug report](https://github.com/DanielLavrushin/b4/issues), `goroutines.txt` in the log directory holds the details when file logging is on. |
| **IPv6 traffic skips all sets** | The router has IPv6, but b4 handles only IPv4, so connections over IPv6 skip the sets. **Enable IPv6 Support** under **Settings > Core > Packet Engine** brings IPv6 into the sets. In TUN mode the message reads **TUN mode does not handle IPv6; IPv6 traffic skips b4**, because that mode never handles it. |

The cross on a start failure or on the IPv6 message hides it.

## Activity {#activity}

The chart shows how many connections b4 checked each minute: **in your sets** when a set matched the connection, **not in a set** otherwise.

![Activity](/img/dashboard/20261003220030.png)

- **1 h** and **24 h** switch the time window, and **Table** shows the same numbers as rows.
- **Live traffic** opens the [Traffic](connections.md) page.
- **Domains not in any set** opens the Traffic page with only the connections no set matched: the sites that are not in a set yet.

The history is kept in memory and starts over when b4 restarts.

### What counts as a connection {#what-a-connection-count-covers}

Each TCP connection or UDP flow counts once, when it starts. b4 sees only the first packets of a connection, so the dashboard does not show how long a connection lasted or how much data it carried.

Only connections to the ports b4 watches count: 443 and the ports the sets add. Connections count whether they come from devices on the network or from the router itself, and connections through proxy sets count too. DNS queries, Discovery's test connections and devices that [device filtering](settings/core.md#device-filtering) leaves out do not.

## Sets {#sets}

One row for each enabled set.

![Sets](/img/dashboard/20261003220040.png)

A row shows the set's kind (**Bypass**, **Route**, **Proxy** or **Block**), how many connections it matched in the last hour, and when it last matched. Proxy sets also show their open connections, and **upstream down** while their proxy fails. Block sets show how many DNS lookups they blocked. A set's name opens it.

When a set shows no matches although its sites are in use, the [Traffic](connections.md) page shows which set, if any, their connections get.

A set that the [watchdog](watchdog.md) keeps working shows its status: **Healthy**, **Failing**, **Healing**, **Gave up**, **Can't check**, **Not checked yet** or **Watchdog off**. The status opens the set's **Discovery** tab.

## Recent changes {#recent-changes}

What happened since b4 started, newest first: starts, applied settings, firewall rules restored, strategies the watchdog switched, settings an AI assistant changed over [MCP](settings/mcp.md), warnings and errors. Most entries link to the related page. The list is kept in memory and starts over when b4 restarts.

![Recent changes](/img/dashboard/20261003220050.png)

The warning **TCP port filters cover the source ports of client connections** means that b4 passes some connections unprocessed. A set's TCP port filter covers the ports devices pick for their own connections, and on this router b4 tells a device's packets from the server's by their ports. The second line names the ports. A filter below 32768 avoids it, and [When b4 tells the direction by port](internals/capture.md#direction-by-port) lists the cases.

## Top domains and addresses {#top-domains}

**Top domains** lists the names devices connect to most. **Top addresses** lists connections without a name, such as Telegram apps, voice calls, games and VPN servers.

![Top domains](/img/dashboard/20261009120010.png)

![Top addresses](/img/dashboard/20261009120040.png)

Badges show the sets that matched. **+** marks a name or an address that no set matched, and adds the domain, or the address, its network or its ASN, to a set. Both lists count from the last [reset](#reset-counters).

## Active Escalations {#active-escalations}

Shown while a host is moved to a backup set because its own set kept failing, see [Escalation](sets/escalation.md). Each row shows the host, the set it moved to and when it goes back. **Clear** sends every host back at once.

![Active Escalations](/img/dashboard/20261003220100.png)

## Blocked {#blocked}

Shown while a [block set](sets/blocking.md) is enabled. **Domains** lists what was blocked, **Devices** the devices that tried to reach it. A device opens its traffic.

![Blocked](/img/dashboard/20261009120020.png)

## Telegram {#telegram}

Shown while the [MTProto proxy](telegram/mtproto-proxy.md) or [Telegram over WebSocket](telegram/websocket-bridge.md) is on. For the proxy, it shows the connected networks, open connections and traffic of each secret. For the bridge, it shows the relayed sessions and failures, and **not working** when the bridge cannot work. The gear button opens **Settings > Telegram**.

![Telegram](/img/dashboard/20261009120030.png)

## Customize the layout {#customize}

**Customize** in the menu at the right end of the status strip switches the panels into edit mode:

1. Drag a panel by its handle to move it, or drop it on another panel to place the two side by side.
2. Drag the border between two panels to change their widths.
3. Click the eye button to hide a panel. Hidden panels are listed in the edit bar, where a click brings one back.
4. Click **Done**.

**Reset layout** returns to the default. The layout is saved in b4, so every browser shows the same one.

![Customize](/img/dashboard/20261009120050.png)

## Reset counters {#reset-counters}

**Reset counters** in the menu at the right end of the status strip starts the counters from zero.

![Reset counters](/img/dashboard/20261003220130.png)

It clears **Top domains**, **Top addresses**, the **Blocked** counts, the count of resets dropped by RST protection and the escalation count. It keeps the **Activity** history, **Recent changes**, the uptime, the Telegram figures and the hosts escalated right now. **Clear** on [Active Escalations](#active-escalations) sends those back.
