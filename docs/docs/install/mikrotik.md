---
sidebar_position: 5
title: MikroTik
---

On MikroTik RouterOS 7.x, b4 runs as a container.

## Requirements

- RouterOS version 7.21.1 or newer
- ARM64 or AMD64 architecture
- External storage attached (Flash/SSD/HDD), formatted as Ext4

:::warning
Containers on MikroTik require external storage - the router's internal memory is not enough.
:::

## Community image

Besides the official `lavrushin/b4` image, there is a community image built specifically for RouterOS: [wiktorbgu/b4-mikrotik](https://hub.docker.com/r/wiktorbgu/b4-mikrotik), maintained by wiktorbgu. It follows b4 releases with matching version tags and packages the service for RouterOS containers:

- b4 runs as an OpenRC service inside the container, so it can be restarted without stopping the container
- the entrypoint selects the firewall backend at startup - nftables, or iptables-legacy with ipset - depending on which kernel modules RouterOS exposes to the container
- it normalizes the priorities of the `main` and `default` routing rules, which RouterOS 7.22 passes into the container in a form that breaks policy routing
- a companion container, [wiktorbgu/dnsproxy-mikrotik](https://hub.docker.com/r/wiktorbgu/dnsproxy-mikrotik), forwards DNS queries over DoH so they are not intercepted

Setup instructions are on the image page. The image is maintained by the community, not built by the b4 project, and its build sources are not published, so its contents cannot be checked against this repository. The guide below uses the official image.

## Example parameters

The guide uses the following values, which have to be replaced with the ones matching the local network:

| Parameter | Value |
| --- | --- |
| Bridge network | 192.168.210.0/24 |
| Bridge gateway | 192.168.210.1 |
| Bridge name | bridge-docker |
| Container IP | 192.168.210.10 |
| Interface name | B4 |
| LAN network | 192.168.100.0/24 |
| DNS server | 192.168.100.1 |
| Routing table | to_b4 |
| Disk | /usb1 |
| Client list | b4users |

## Step 1: Bridge

A bridge for the Docker network:

```routeros
/interface/bridge add name=bridge-docker port-cost-mode=short
/ip/address add address=192.168.210.1/24 interface=bridge-docker network=192.168.210.0
```

## Step 2: Interface

A virtual Ethernet interface attached to the bridge:

```routeros
/interface/veth add address=192.168.210.10/24 gateway=192.168.210.1 name=B4
/interface/bridge/port add bridge=bridge-docker interface=B4
```

## Step 3: Routing

A routing table and a route through the container:

```routeros
/routing table add disabled=no fib name=to_b4
/ip route add check-gateway=ping gateway=192.168.210.10 routing-table=to_b4
```

## Step 4: Traffic marking

Traffic from clients in the `b4users` list is redirected through the container:

```routeros
/ip firewall mangle add chain=prerouting action=mark-connection \
    new-connection-mark=b4_connections passthrough=yes connection-state=new \
    dst-address-type=!local src-address-list=b4users in-interface-list=LAN \
    place-before=0

/ip firewall mangle add chain=prerouting action=mark-routing \
    new-routing-mark=to_b4 passthrough=no connection-mark=b4_connections \
    in-interface-list=LAN log=no place-before=1
```

:::caution FastTrack
FastTrack bypasses mangle rules. It has to be restricted to unmarked connections:

```routeros
/ip firewall filter set [find action=fasttrack-connection] connection-mark=no-mark
```

:::

## Step 5: Mount points

```routeros
/container/mounts add name=b4_etc src=/usb1/docker/b4-mounts/etc dst=/opt/etc/b4
```

The `/usb1/docker/b4-mounts/etc` directory has to exist on the disk.

## Step 6: Run the container

Registry configuration:

```routeros
/container/config set registry-url=https://registry-1.docker.io tmpdir=/usb1/docker/pull
```

Creating the container:

```routeros
/container add remote-image=lavrushin/b4:latest interface=B4 \
    root-dir=/usb1/docker/b4-mikrotik mounts=b4_etc \
    cmd="--config /opt/etc/b4/b4.json" start-on-boot=yes \
    logging=yes dns=192.168.100.1
```

After the image has been pulled:

```routeros
/container start [find tag~"b4"]
```

:::info DNS hijacking
Where the ISP intercepts DNS (port 53 redirection), public resolvers inside the container do not help. The way around it is DoH on MikroTik, with the container pointed at the bridge gateway instead of public DNS:

```routeros
/ip dns set use-doh-server=https://cloudflare-dns.com/dns-query verify-doh-cert=yes
```

The container DNS then becomes `dns=192.168.210.1` (the bridge gateway).
:::

## Step 7: Add clients

Devices are added to the `b4users` address list:

```routeros
/ip firewall address-list add list=b4users address=192.168.100.50
/ip firewall address-list add list=b4users address=192.168.100.51
```

## Step 8: NAT Masquerade

In the web interface (`http://192.168.210.10:7000`), **NAT Masquerade** is turned on under [Settings, Core, Firewall](../settings/core#nat-masquerade). The masquerade interface stays at its default, all interfaces.

The routing mark from Step 4 applies only to packets arriving from the LAN, so RouterOS sends the server's replies straight to the client and they never pass through the container. Conntrack inside the container sees the client's SYN, never sees the answer to it, and marks every later packet of the connection as invalid. b4 picks the first packets of a connection by the conntrack packet count, so without masquerade it never receives the TLS ClientHello, and the strategies that act on it do not run.

With masquerade on, the container rewrites the source of forwarded traffic to its own address, 192.168.210.10. The replies return to the container, which hands them back to the client through RouterOS. On the leg from the container to the internet, RouterOS sees 192.168.210.10 as the source of every connection, so rules on that leg cannot tell the clients apart.

## Web interface

After the container starts: `http://192.168.210.10:7000`

:::tip Reduce disk wear
USB flash and SD cards have a limited number of write cycles. b4 logs can be moved to RAM in the web interface:

**Log Directory** on the **Logging** card under **Settings, System, Service**: `/tmp/log/b4`

Logs are lost on reboot, but storage lasts longer.
:::

## Access from the WAN {#access-from-the-wan}

The container sits behind RouterOS, which filters and translates everything that arrives from the WAN before it reaches the bridge. A b4 port, such as the MTProto proxy's `3128`, is reachable from outside only through a `dst-nat` rule on RouterOS towards the container's address:

```routeros
/ip firewall nat add chain=dstnat action=dst-nat in-interface-list=WAN \
    protocol=tcp dst-port=3128 to-addresses=192.168.210.10 to-ports=3128
```

**Expose to internet** in b4 changes only the firewall inside the container and does not replace this rule. What the switch does elsewhere is described under [Access from the internet](../settings/security.md#expose-to-internet).

## Update

```routeros
/container stop [find tag~"b4"]
/container remove [find tag~"b4"]
/container add remote-image=lavrushin/b4:latest interface=B4 \
    root-dir=/usb1/docker/b4-mikrotik mounts=b4_etc \
    cmd="--config /opt/etc/b4/b4.json" start-on-boot=yes \
    logging=yes dns=192.168.100.1
```

The configuration is stored on the mount point and is preserved when the container is recreated.

## Sending a set to another container

A set can hand its traffic to a SOCKS5 proxy in another container on the same bridge, such as Xray, sing-box or mihomo with a SOCKS5 inbound, instead of letting it leave through the main route of RouterOS. The set's **Routing mode** is *Upstream SOCKS5 proxy*, with the other container's address (for example 192.168.210.20) and its SOCKS5 port as the upstream. RouterOS needs no extra rules for this: b4's connection to the proxy stays inside `bridge-docker`, and only the proxy's own connections go out through RouterOS.

In this mode b4 applies no DPI strategy to the set's traffic; the proxy reaches the destination. [Upstream SOCKS5 proxy](/docs/sets/routing#upstream-socks5-proxy) describes the diversion, UDP handling and the kernel modules it needs. Several sets can point at different proxies.

## Routing chosen destinations through the container {#routing-by-destination}

Step 4 picks the traffic by client. To pick it by destination, for example a site's subnets, the destinations go into an address list, and the marking rules match that list instead of `b4users`:

```routeros
/ip firewall address-list add list=via_b4 address=203.0.113.0/24

/ip firewall mangle add chain=prerouting action=mark-connection \
    new-connection-mark=b4_connections passthrough=yes connection-state=new \
    dst-address-type=!local dst-address-list=via_b4 in-interface-list=LAN \
    place-before=0

/ip firewall mangle add chain=prerouting action=mark-routing \
    new-routing-mark=to_b4 passthrough=no connection-mark=b4_connections \
    in-interface-list=LAN log=no place-before=1
```

The route through the container stays in table `to_b4` from [Step 3](#step-3-routing). A route to the container in the main table does not work here: the container sends everything it processed back to RouterOS, its default gateway, and a main-table route by destination sends the same packets straight back to the container instead of out through the WAN.

The marking rules, here and in Step 4, apply only to packets that arrive from the LAN interface list, so the packets the container sends back leave through the main table. `bridge-docker` is not in that list after Step 1. If it has been added, a rule above the marking rules keeps the container's packets out of them. It is added after them, so that `place-before=0` puts it on top:

```routeros
/ip firewall mangle add chain=prerouting action=accept \
    in-interface=bridge-docker place-before=0
```

b4 on a separate Linux machine instead of a container needs the same layout: a subnet of its own on its own RouterOS port, VLAN or bridge, RouterOS as its default gateway, the route in `to_b4` pointing at the machine's address, NAT Masquerade on in b4 as in Step 8, and that interface kept out of the LAN interface list or exempted by the same accept rule with its name in place of `bridge-docker`. On the clients' own subnet the machine hands the server's replies straight to the client, so RouterOS sees only the client's half of each connection and treats the client's following TCP packets as invalid, which stalls or breaks its TCP connections.

RouterOS tells b4's packets apart by the interface they arrive on, so these rules need nothing from b4 inside the packets. The marks b4 uses internally, the packet mark among them, never leave the container or the machine.

## Troubleshooting

**Container will not start:**

1. Status: `/container print`
2. Logs: `/log print where topics~"container"`
3. The disk has to be formatted as Ext4

**No access to the web interface:**

1. The container has to be running: `/container print`
2. Connectivity: `/ping 192.168.210.10`

**Traffic is not redirected:**

1. The list: `/ip firewall address-list print where list=b4users`
2. Mangle: `/ip firewall mangle print`
3. The route: `/ip route print where routing-table=to_b4`

**Destinations routed through the container do not open:**

1. The route to the container has to be in table `to_b4`, not in the main table, see [Routing chosen destinations through the container](#routing-by-destination)
2. `bridge-docker`, or the interface of a separate b4 machine, has to stay out of the LAN interface list, or its packets have to be exempted from the marking rules as shown there

**Traffic reaches the container but the bypass has no effect:**

1. NAT Masquerade has to be on in b4, see [Step 8](#step-8-nat-masquerade)

**The dashboard reports that the NFQUEUE engine did not start:**

1. The error on the dashboard card names the cause. `Extension NFQUEUE revision 0 not supported, missing kernel module?` means the kernel RouterOS runs the container on has no NFQUEUE support
2. **Switch to TUN** on the same card selects the TUN engine, which needs no queue modules, and restarts b4, see [Packet engine](../settings/core.md#packet-engine)
