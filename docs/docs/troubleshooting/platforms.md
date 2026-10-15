---
sidebar_position: 50
title: Router platforms
description: Problems specific to OpenWrt, Keenetic and MikroTik, listed by what shows up.
---

Problems that belong to one router platform, by the symptom. The installation itself is described on the [platform pages](../install/index.md).

## OpenWrt {#openwrt}

### The service does not start {#openwrt-service-does-not-start}

1. Check that the kernel modules are installed and loaded, see [Kernel modules](../install/openwrt.md#kernel-modules).
2. Read the log:

   ```bash
   logread | grep b4
   ```

### `Error: Could not process rule` {#could-not-process-rule}

b4 fails while it adds its rules. The cause may be a broken table left from an earlier run that failed. To delete it and start b4 again:

```bash
nft delete table inet b4_mangle 2>/dev/null
/etc/init.d/b4 restart
```

### Slow speed or stuttering video {#openwrt-slow-speed}

On some devices **Software flow offloading**, under **Network > Firewall** in LuCI, affects how fast b4 works. Both positions of the switch are worth a try.

### b4 runs but sees no traffic, with flow offloading on {#flow-offloading}

Flow offloading moves an established connection to a fast path that skips the firewall hooks b4 works from. Turning it off fixes this, but on weaker hardware it costs a lot of throughput.

b4 needs only the first packets of a connection: 19 for TCP and 8 for UDP by default, set by **TCP Connection Packets Limit** and **UDP Connection Packets Limit** under **Settings > Core > Packet Engine**. Offloading can start after them:

1. In `/usr/share/firewall4/templates/ruleset.uc`, find the line:

   ```text
   meta l4proto { tcp, udp } flow offload @ft;
   ```

2. Replace it with:

   ```text
   meta l4proto { tcp, udp } ct original packets ge 40 flow offload @ft;
   ```

3. Reload the firewall:

   ```bash
   fw4 restart
   ```

The limits of this approach:

- The threshold, `40` here, has to stay above b4's packet limits. A higher limit in b4 puts connections on the fast path before b4 is done with them.
- The rule reads conntrack accounting. When `sysctl net.netfilter.nf_conntrack_acct` returns `0`, the counter stays at zero and nothing is offloaded at all.
- A set with **Enable Packet Duplication** on its **TCP** tab inspects the whole connection, so no threshold fits it.
- The file belongs to the `firewall4` package. A package upgrade or a sysupgrade overwrites it.

**System Info** under **Settings > System > Service**, and the installer's diagnostics, show the threshold they find and compare it with b4's limits.

## Keenetic {#keenetic}

The error log is `/var/log/b4/errors.log`.

### `xt_connbytes kernel module is not available` {#keenetic-xt-connbytes}

The Netfilter components are not fully on. Both **Kernel modules for Netfilter** and **Xtables-addons for Netfilter** have to be on, see [Netfilter components](../install/keenetic.md#netfilter-components).

### `b4 cannot copy DNS queries with NFLOG (...)` {#keenetic-nflog}

b4 holds DNS queries in its queue after conntrack. A device that asks for a name's IPv4 and IPv6 addresses at once then often loses the second answer. The brackets give the reason, such as a module that did not load, `xt_NFLOG` or `nfnetlink_log`.

**System Info** shows **DNS queries copied, not held (NFLOG)** as unavailable under **Kernel Capabilities**, with the missing modules and the component to turn on. The fix is in [Netfilter components](../install/keenetic.md#netfilter-components).

### The log reports b4's rules missing {#keenetic-firewall-rewrites}

KeeneticOS rebuilds its firewall on its own: on a UPnP renewal, a WAN reconnect, or a change to a policy or a component. Each rebuild removes b4's rules. The installer places `50-b4.sh` in `/opt/etc/ndm/netfilter.d/`, and this hook makes b4 restore its rules as soon as the rebuild ends. Without the hook, b4 finds them missing at its next check, every **Firewall Monitor Interval (seconds)**, 10 by default.

| Log line | Meaning |
| --- | --- |
| `Tables rules missing after a firewall rewrite, restoring...`, then `Tables rules restored successfully` | The hook reported a rebuild, and b4 put its rules back. Normal |
| `Tables rules missing, restoring...` at the WARN level | The check found the rules gone with no signal from the hook. Either `50-b4.sh` is missing, or something other than KeeneticOS removed the rules |
| `Expose: the rules opening ... were missing from ..., usually after the firewall was reloaded; restored them` | The rules of **Expose to internet** were put back. It follows every rebuild, also with a working hook |
| `TUN: capture chain B4_TUN lost N of M rules (removed outside b4) ...` | In TUN mode, the capture chain was rebuilt. It follows every rebuild as well |

The same check can be started by hand:

```bash
kill -USR1 $(cat /var/run/b4.pid)
```

### The log says b4 copies DNS queries with NFLOG {#keenetic-dns-queries}

On KeeneticOS with a kernel before 4.18 (4.14 before 4.14.173), b4 does not hold DNS queries. It takes a copy of each one and acts on the resolver's reply. At start the log says:

```text
IPTABLES[iptables]: KeeneticOS runs its raw table after conntrack (kernel 4.9-ndm-5), so b4 copies DNS queries with NFLOG (group 537) instead of holding them in its queue
```

This is normal, and **System Info** shows **DNS queries copied, not held (NFLOG)** as available. What it changes:

- The router's resolver still gets every query and caches the real answer, also for a name that a set blocks, pins or sends to its own resolver. Devices that ask the router get b4's answer instead.
- A device that asks a resolver outside the router gets b4's answer only when that resolver replies.
- A `nat` rule that moves DNS to another port is not supported.
- After a firewall rebuild, queries pass without a copy until b4 restores its rules, see [above](#keenetic-firewall-rewrites).

With a later kernel, b4 holds DNS queries in its queue and logs no such line. [How b4 handles DNS](../internals/dns.md) explains the difference.

## MikroTik {#mikrotik}

The commands refer to the names of the [MikroTik setup](../install/mikrotik.md).

### The container does not start {#mikrotik-container-does-not-start}

1. Check its status: `/container print`.
2. Read its log: `/log print where topics~"container"`.
3. Check that the disk is formatted as Ext4.

### The web interface does not open {#mikrotik-web-interface}

1. Check that the container runs: `/container print`.
2. Check that RouterOS reaches it: `/ping 192.168.210.10`.

### Traffic does not go through the container {#mikrotik-traffic-not-redirected}

1. Check the client list: `/ip firewall address-list print where list=b4users`.
2. Check the marking rules: `/ip firewall mangle print`.
3. Check the route: `/ip route print where routing-table=to_b4`.
4. Check that FastTrack is limited to unmarked connections, see [Step 4](../install/mikrotik.md#step-4-traffic-marking).

### Destinations routed through the container do not open {#mikrotik-destinations}

1. Check that the route to the container is in table `to_b4`, not in the main table, see [Routing chosen destinations](../install/mikrotik.md#routing-by-destination).
2. Check that `bridge-docker`, or the interface of a separate b4 machine, is out of the LAN interface list, or that its packets are exempted from the marking rules as shown there.

### Traffic reaches the container, but nothing changes {#mikrotik-no-effect}

NAT Masquerade is off in b4, so the server's replies bypass the container. [Step 8](../install/mikrotik.md#step-8-nat-masquerade) turns it on.

### The dashboard reports that the NFQUEUE engine did not start {#mikrotik-nfqueue}

The error on the dashboard card names the cause. `Extension NFQUEUE revision 0 not supported, missing kernel module?` means that the kernel RouterOS runs the container on has no NFQUEUE support. **Switch to TUN** on the same card selects the TUN engine, which needs no queue modules, and restarts b4. See [Packet engine](../settings/core.md#packet-engine).
