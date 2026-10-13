---
sidebar_position: 4
title: Keenetic
---

## Requirements

- Keenetic router with OPKG support
- Entware installed (required)

## Install Entware

### Newer models (with built-in storage)

1. The router web interface is opened
2. Section **System settings**
3. The **OPKG package manager** component is enabled

### Older models (USB drive required)

1. A USB drive is attached to the router
2. Entware is installed through the package manager

More details: [help.keenetic.com](https://help.keenetic.com/hc/en-us/articles/360021214160)

## Netfilter components {#netfilter-components}

Keenetic NDMS does not ship all the netfilter kernel modules b4 needs out of the box. The required components have to be enabled before b4 is installed:

1. In the router web interface: **System settings** → **Component options**
2. **Netfilter** - the base netfilter component - is enabled
3. Once Netfilter is enabled, a new option appears: **Xtables-addons for Netfilter**
4. The changes are applied, followed by a router reboot or a component reload

Then, over SSH, the iptables userspace is installed:

```bash
opkg install iptables
```

## Install b4

Over an SSH connection to the router:

```bash
curl -fsSL https://raw.githubusercontent.com/DanielLavrushin/b4/main/install.sh | sh
```

## Service control

```bash
/opt/etc/init.d/S99b4 start
/opt/etc/init.d/S99b4 stop
/opt/etc/init.d/S99b4 restart
```

## Paths

| What | Where |
| --- | --- |
| Binary | `/opt/sbin/b4` |
| Configuration | `/opt/etc/b4/b4.json` |
| Service | `/opt/etc/init.d/S99b4` |

## Architecture

- Older models (MT7621) - `mipsle_softfloat`
- Newer models (aarch64) - `arm64`

The installer detects the architecture automatically.

:::warning Without Entware
Without Entware, b4 is placed in `/tmp`, which is cleared on every reboot. For persistent operation, Entware is required.
:::

## Firewall rewrites {#firewall-rewrites}

NDMS rebuilds its netfilter tables on its own schedule: a UPnP port mapping renewal, a WAN reconnect, a change to a policy or a component. Each rebuild drops the chains it does not own, including those of b4, and then runs every script under `/opt/etc/ndm/netfilter.d/` with the table name in `$table` and the family in `$type`.

The installer places `50-b4.sh` in that directory. The script sends `SIGUSR1` to the running b4 process, which re-checks its rules as soon as the rebuild settles and restores what is missing. Without the hook, the tables monitor still restores the rules on its next poll, which is up to `system.tables.monitor_interval` seconds later (10 by default).

The rules of the [Expose to internet](../settings/security.md#expose-to-internet) switches sit in the `filter` table, which NDMS rewrites as well, and are re-checked on the same signal and at the same interval. b4 logs putting them back at the WARN level as `Expose: the rules opening ... were missing from ..., usually after the firewall was reloaded; restored them`, whether the hook or the poll found them missing, so that line follows a rewrite even when the hook works.

A rewrite announced by the hook leaves `Tables rules missing after a firewall rewrite, restoring...` followed by `Tables rules restored successfully` in the log, at the INFO level. The same line at the WARN level, without the rewrite wording, means the poll found the rules gone with no signal from the hook: either the hook is missing or something other than NDMS removed them. The same re-check can be requested by hand:

```bash
kill -USR1 $(cat /var/run/b4.pid)
```

In TUN mode the signal also makes the TUN engine re-check its capture chain `B4_TUN` and the jumps into it at once. The engine logs a rebuilt chain at the WARN level as `TUN: capture chain B4_TUN lost N of M rules (removed outside b4) ...`, whether the hook or the poll found it, and the tables monitor's lines cover only the masquerade, MSS clamp, DSCP and routing-set rules. The poll interval in this mode is at least 10 seconds.

## DNS queries {#dns-queries}

KeeneticOS runs the iptables raw table after conntrack, and b4 does not use it there: queueing a DNS query in that table does not keep the second of two queries a device sends at once from one socket from being dropped, see the **Requests and conntrack** note in [What b4 intercepts](../dns.md#what-b4-intercepts). With a kernel before 4.18 (4.14 before 4.14.173), which drops that second query, b4 holds no DNS query on KeeneticOS. It takes a copy of each one with NFLOG and acts on the resolver's reply, as described in [Copied requests](../dns.md#copied-requests). At start the log says:

```text
IPTABLES[iptables]: KeeneticOS runs its raw table after conntrack (kernel 4.9-ndm-5), so b4 copies DNS queries with NFLOG (group 537) instead of holding them in its queue
```

System Info then shows **DNS queries copied, not held (NFLOG)** as available under **Kernel Capabilities**. The group is `queue.start_num`, 537 by default.

With a later kernel, b4 queues DNS queries in the mangle table, as it does the replies, logs no such line, and System Info shows neither DNS row under **Kernel Capabilities**.

While b4 works on copies, the router's resolver still receives every query and resolves the name, also one that a set sends to a DoH resolver, and caches the real answer. For a name a set blocks, pins or sends to its own resolver, a device gets b4's answer in place of the router resolver's reply, or from b4 directly when that reply has not come within 20 ms; a device that asks a resolver outside the router gets b4's answer only when that resolver replies. A `nat` rule that moves DNS to another port is not supported. A rebuild of the tables by NDMS removes the copy rules along with b4's other rules, and queries pass without a copy until b4 restores them, see [Firewall rewrites](#firewall-rewrites).

## Troubleshooting

After the service starts, the log is worth checking:

```bash
cat /var/log/b4/errors.log
```

The line `xt_connbytes kernel module is not available` means the Netfilter components were not enabled correctly - see [Netfilter components](#netfilter-components) above, where both **Netfilter** and **Xtables-addons for Netfilter** have to be active.

The line `b4 cannot copy DNS queries with NFLOG (...)` means b4 holds DNS queries in its queue after conntrack, and a device that asks for a name's IPv4 and IPv6 addresses at once often loses the second answer. The brackets give the reason, such as a module that did not load, `xt_NFLOG` or `nfnetlink_log`. System Info shows **DNS queries copied, not held (NFLOG)** as unavailable under **Kernel Capabilities**, with the missing modules and a line naming the component to enable; the [Netfilter components](#netfilter-components) above are the place to check.

If the log is empty (or has no errors), the b4 web interface should be reachable on the router's LAN IP.
