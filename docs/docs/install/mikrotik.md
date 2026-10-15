---
sidebar_position: 5
title: MikroTik
description: Running b4 as a container on MikroTik RouterOS 7.
---

On MikroTik RouterOS 7, b4 runs as a container. RouterOS sends the traffic of chosen clients through it.

## Requirements

- RouterOS 7.21.1 or newer, on ARM64 or AMD64.
- External storage (flash, SSD or HDD) formatted as Ext4. The router's internal memory is not enough for containers.

## Community image

Besides the official `lavrushin/b4` image, the community image [wiktorbgu/b4-mikrotik](https://hub.docker.com/r/wiktorbgu/b4-mikrotik) is built for RouterOS and follows b4 releases with the same version tags. Compared with the official image:

- b4 runs as an OpenRC service, so it restarts without stopping the container;
- the firewall backend, nftables or iptables-legacy with ipset, is picked from the kernel modules RouterOS exposes;
- the priorities of the `main` and `default` routing rules are fixed, which RouterOS 7.22 passes into containers in a form that breaks policy routing;
- a companion container, [wiktorbgu/dnsproxy-mikrotik](https://hub.docker.com/r/wiktorbgu/dnsproxy-mikrotik), forwards DNS over DoH.

Its setup is described on its page. wiktorbgu maintains it, not the b4 project, and its build sources are not published, so its contents cannot be checked against this repository. The steps below use the official image.

## Example values

The steps use these values. They are replaced with the ones of the local network:

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

A bridge for the container network:

```routeros
/interface/bridge add name=bridge-docker port-cost-mode=short
/ip/address add address=192.168.210.1/24 interface=bridge-docker network=192.168.210.0
```

## Step 2: Interface

A virtual Ethernet interface on the bridge:

```routeros
/interface/veth add address=192.168.210.10/24 gateway=192.168.210.1 name=B4
/interface/bridge/port add bridge=bridge-docker interface=B4
```

## Step 3: Routing

A routing table with a route through the container:

```routeros
/routing table add disabled=no fib name=to_b4
/ip route add check-gateway=ping gateway=192.168.210.10 routing-table=to_b4
```

## Step 4: Traffic marking

The traffic of the clients in the `b4users` list goes through the container:

```routeros
/ip firewall mangle add chain=prerouting action=mark-connection \
    new-connection-mark=b4_connections passthrough=yes connection-state=new \
    dst-address-type=!local src-address-list=b4users in-interface-list=LAN \
    place-before=0

/ip firewall mangle add chain=prerouting action=mark-routing \
    new-routing-mark=to_b4 passthrough=no connection-mark=b4_connections \
    in-interface-list=LAN log=no place-before=1
```

FastTrack skips mangle rules, so it is limited to unmarked connections:

```routeros
/ip firewall filter set [find action=fasttrack-connection] connection-mark=no-mark
```

## Step 5: Mount point

```routeros
/container/mounts add name=b4_etc src=/usb1/docker/b4-mounts/etc dst=/opt/etc/b4
```

The directory `/usb1/docker/b4-mounts/etc` has to exist on the disk.

## Step 6: Container

The registry:

```routeros
/container/config set registry-url=https://registry-1.docker.io tmpdir=/usb1/docker/pull
```

The container:

```routeros
/container add remote-image=lavrushin/b4:latest interface=B4 \
    root-dir=/usb1/docker/b4-mikrotik mounts=b4_etc \
    cmd="--config /opt/etc/b4/b4.json" start-on-boot=yes \
    logging=yes dns=192.168.100.1
```

Once the image is pulled:

```routeros
/container start [find tag~"b4"]
```

Where the provider intercepts DNS on port 53, public resolvers inside the container do not help. DoH on RouterOS works around it, with the container pointed at the bridge gateway, `dns=192.168.210.1`:

```routeros
/ip dns set use-doh-server=https://cloudflare-dns.com/dns-query verify-doh-cert=yes
```

## Step 7: Clients

The devices that go through b4 are added to the `b4users` list:

```routeros
/ip firewall address-list add list=b4users address=192.168.100.50
/ip firewall address-list add list=b4users address=192.168.100.51
```

## Step 8: NAT Masquerade

1. Open the web interface at `http://192.168.210.10:7000`.
2. Turn on **NAT Masquerade** under [Settings > Core > Firewall](../settings/core.md#nat-masquerade). The masquerade interface stays at its default, all interfaces.

Without masquerade, RouterOS sends the server's replies straight to the client, past the container. b4 then never gets the TLS ClientHello of a connection, and its strategies do not run. With masquerade, the connections leave the container from its address, 192.168.210.10, so rules after the container on RouterOS cannot tell the clients apart.

## Web interface

After the container starts, the web interface is at `http://192.168.210.10:7000`.

:::tip Less disk wear
USB flash drives and SD cards wear out with writes. **Log Directory** on the **Logging** card under **Settings > System > Service** set to `/tmp/log/b4` keeps the logs in RAM. They are then lost on reboot.
:::

## Access from the WAN {#access-from-the-wan}

RouterOS filters and translates everything that arrives from the WAN before it reaches the bridge. A b4 port, such as `3128` of the MTProto proxy, is reachable from outside only through a `dst-nat` rule towards the container:

```routeros
/ip firewall nat add chain=dstnat action=dst-nat in-interface-list=WAN \
    protocol=tcp dst-port=3128 to-addresses=192.168.210.10 to-ports=3128
```

**Expose to internet** in b4 changes only the firewall inside the container and does not replace this rule, see [Access from the internet](../settings/security.md#expose-to-internet).

## Update {#update}

```routeros
/container stop [find tag~"b4"]
/container remove [find tag~"b4"]
/container add remote-image=lavrushin/b4:latest interface=B4 \
    root-dir=/usb1/docker/b4-mikrotik mounts=b4_etc \
    cmd="--config /opt/etc/b4/b4.json" start-on-boot=yes \
    logging=yes dns=192.168.100.1
```

The configuration is on the mount point and stays when the container is created again.

## Sending a set to another container

A set can hand its traffic to a SOCKS5 proxy in another container on the same bridge, such as Xray, sing-box or mihomo with a SOCKS5 inbound. The set's **Routing mode** is **Upstream SOCKS5 proxy**, with the other container's address, for example 192.168.210.20, and its SOCKS5 port. RouterOS needs no extra rules: b4 reaches the proxy inside `bridge-docker`, and only the proxy's own connections go out through RouterOS. See [Sending sites through a SOCKS5 proxy](../guides/proxy.md).

## Routing chosen destinations through the container {#routing-by-destination}

Step 4 picks the traffic by client. To pick it by destination, such as a site's subnets, the destinations go into an address list, and the marking rules match it in place of `b4users`:

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

The route through the container stays in table `to_b4` from [Step 3](#step-3-routing). A route to the container in the main table does not work: the container sends its traffic back to RouterOS, and the main-table route returns it to the container instead of the WAN.

The marking rules apply only to packets from the LAN interface list, so the packets the container sends back leave through the main table. `bridge-docker` is not in that list after Step 1. If it has been added, this rule keeps the container's packets out of the marking rules. It is added after them, so that `place-before=0` puts it on top:

```routeros
/ip firewall mangle add chain=prerouting action=accept \
    in-interface=bridge-docker place-before=0
```

b4 on a separate Linux machine needs the same layout:

- a subnet of its own, on its own RouterOS port, VLAN or bridge, with RouterOS as its default gateway;
- the route in `to_b4` pointing at the machine;
- NAT Masquerade on in b4, as in Step 8;
- its interface out of the LAN interface list, or exempted by the same accept rule with its name in place of `bridge-docker`.

On the clients' own subnet, the machine hands the server's replies straight to the clients, and RouterOS stalls or breaks their TCP connections.

## Routing a set's traffic by its DSCP value {#routing-by-dscp}

Everything b4 sends back to RouterOS leaves through the main route, whatever its set. A set's own [DSCP value](../sets/routing.md#dscp) marks its packets, and by it RouterOS sends that set's traffic through another route, such as a VPN. Routing into the container, above, needs nothing of the kind.

The value is turned on with **Enable per-set DSCP** on the set's **DNS & Routing** tab, here `31`. `wg-vpn` stands for the VPN interface and `via_vpn` for its routing table:

```routeros
/routing table add disabled=no fib name=via_vpn
/ip route add gateway=wg-vpn routing-table=via_vpn

/ip firewall mangle add chain=prerouting action=mark-connection \
    new-connection-mark=b4_dscp31 passthrough=yes connection-state=new \
    connection-mark=no-mark in-interface=bridge-docker \
    src-address=192.168.210.10 dscp=31

/ip firewall mangle add chain=prerouting action=mark-routing \
    new-routing-mark=via_vpn passthrough=no connection-mark=b4_dscp31 \
    in-interface=bridge-docker

/ip firewall mangle add chain=prerouting action=mark-connection \
    new-connection-mark=b4_other passthrough=yes connection-state=new \
    connection-mark=no-mark in-interface=bridge-docker src-address=192.168.210.10

/ip firewall mangle add chain=postrouting action=change-dscp new-dscp=0 \
    out-interface-list=WAN

/ip firewall mangle add chain=postrouting action=change-dscp new-dscp=0 \
    out-interface=wg-vpn

/ip firewall nat add chain=srcnat action=masquerade out-interface=wg-vpn
```

| Rule | What it does |
| --- | --- |
| The first `mark-connection` | Marks a connection from b4 whose first packet carries the value. `connection-mark=no-mark` keeps a connection already marked `b4_other` from moving to the VPN halfway, which would make it start over |
| `mark-routing` | Sends the marked connections into `via_vpn`. `in-interface=bridge-docker` keeps the server's replies, which share the connection mark, out of the VPN |
| The second `mark-connection` | Marks b4's other connections as `b4_other`, which keeps them out of FastTrack. FastTrack skips the `change-dscp` rules. In exchange, none of b4's traffic is fast-tracked |
| `change-dscp` on `WAN` | Clears the value before the internet |
| `change-dscp` on `wg-vpn` | Clears the value before the tunnel. Without it, the value reaches the VPN server inside the tunnel |
| `masquerade` on `wg-vpn` | Gives the connections the address of the VPN interface |

The FastTrack limit from [Step 4](#step-4-traffic-marking) is required. Without it the marked connections are fast-tracked and leave through the main uplink with the VPN's source address.

When the accept rule for `bridge-docker` from [Routing chosen destinations](#routing-by-destination) is at the top of mangle prerouting, the three prerouting rules never see b4's packets. They go above it, with this added to each (the quotes are required):

```routeros
place-before=[find where action=accept in-interface="bridge-docker"]
```

Variations:

- **A second uplink instead of a VPN**: the route in `via_vpn` points at the uplink's gateway, and the last two rules name its interface in place of `wg-vpn`.
- **More values**: each value needs its own `mark-connection` rule. A value that goes another way also needs its own connection mark, `mark-routing` rule, table, and the last two rules for its interface.
- **b4 on a separate machine**: its interface and address replace `bridge-docker` and `192.168.210.10`.
- **Reverse path filter**: `/ip settings rp-filter=strict` drops the replies that come back through the VPN. `loose` and the default `no` let them in.

In this layout the devices resolve names through RouterOS, so b4 learns the addresses of a set's domains only from TLS and QUIC names and from its own lookups. A connection to an address b4 has not learned yet stays on the main route. IP, GeoIP and ASN targets give a connection the value from its first packet.

The rules were tested on RouterOS 7.24.5, for IPv4.

## Troubleshooting

A container that does not start, an unreachable web interface, traffic that does not reach the container and an engine that does not start are covered under [Router platforms](../troubleshooting/platforms.md#mikrotik).
